package shell

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/basalt-os/basalt-shell/internal/apps"
	"github.com/basalt-os/basalt-shell/internal/assistant"
	"github.com/basalt-os/basalt-shell/internal/audit"
	"github.com/basalt-os/basalt-shell/internal/i18n"
	"github.com/basalt-os/basalt-shell/internal/keyboard"
	"github.com/basalt-os/basalt-shell/internal/models"
	"github.com/basalt-os/basalt-shell/internal/voiceprefs"
)

// Request is one IPC message from a client (newline-delimited JSON).
type Request struct {
	ID   int64           `json:"id"`
	Op   string          `json:"op"`
	Args json.RawMessage `json:"args,omitempty"`
}

// Reply answers a request.
type Reply struct {
	ID     int64  `json:"id"`
	OK     bool   `json:"ok"`
	Result any    `json:"result,omitempty"`
	Error  string `json:"error,omitempty"`
}

// Roles.
const (
	RoleUI    = "ui"    // the shell's own UI (Quickshell): may confirm, may act directly
	RoleAgent = "agent" // anything else: MCP servers, scripts; writes become proposals
)

// Server serves the local IPC socket.
type Server struct {
	Core *Core
	Path string
	// UI decides which peers may take the ui role (the only role that
	// may confirm): their SELinux domain in the product (DetectUICheck).
	UI UICheck

	uiMu   sync.Mutex
	uiSess *session
}

// DefaultSocket is $XDG_RUNTIME_DIR/basalt-shell/shell.sock.
func DefaultSocket() string {
	if p := os.Getenv("BASALT_SHELL_SOCKET"); p != "" {
		return p
	}
	dir := os.Getenv("XDG_RUNTIME_DIR")
	if dir == "" {
		dir = filepath.Join(os.TempDir(), fmt.Sprintf("basalt-shell-%d", os.Getuid()))
	}
	return filepath.Join(dir, "basalt-shell", "shell.sock")
}

// Listen serves until ctx ends.
func (s *Server) Listen(ctx context.Context) error {
	if c, err := net.Dial("unix", s.Path); err == nil {
		c.Close()
		return fmt.Errorf("another basalt-shell daemon is listening on %s", s.Path)
	}
	// Start from a fresh directory when it holds only a stale socket: the
	// daemon creating it gives it the SELinux type of the shell's runtime
	// files (the only socket agents may reach), whoever created it before.
	_ = os.Remove(s.Path)
	_ = os.Remove(filepath.Dir(s.Path)) // fails, harmlessly, when not empty
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	_ = os.Chmod(filepath.Dir(s.Path), 0o700)
	l, err := net.Listen("unix", s.Path)
	if err != nil {
		return err
	}
	_ = os.Chmod(s.Path, 0o600)
	go func() { <-ctx.Done(); l.Close() }()
	for {
		conn, err := l.Accept()
		if err != nil {
			if ctx.Err() != nil {
				return nil
			}
			return err
		}
		go s.serve(ctx, conn.(*net.UnixConn))
	}
}

// claimUI makes ss the one ui connection. A second process asking for
// the ui role while the shell UI is connected is refused (and audited):
// taking over needs the running UI gone first, which the person sees.
// The same process reconnecting replaces its old connection.
func (s *Server) claimUI(ss *session) bool {
	s.uiMu.Lock()
	defer s.uiMu.Unlock()
	if s.uiSess != nil && s.uiSess != ss {
		if s.uiSess.pid != ss.pid {
			return false
		}
		// The same UI process reconnecting: its old connection is dead or
		// about to be; the new one takes over.
		s.uiSess.conn.Close()
	}
	s.uiSess = ss
	return true
}

func (s *Server) releaseUI(ss *session) {
	s.uiMu.Lock()
	if s.uiSess == ss {
		s.uiSess = nil
	}
	s.uiMu.Unlock()
}

type session struct {
	s      *Server
	conn   *net.UnixConn
	wmu    sync.Mutex
	w      *bufio.Writer
	role   string
	client string
	pid    int
	peer   Peer
}

func (ss *session) send(v any) {
	b, err := json.Marshal(v)
	if err != nil {
		return
	}
	ss.wmu.Lock()
	defer ss.wmu.Unlock()
	_ = ss.conn.SetWriteDeadline(time.Now().Add(5 * time.Second))
	ss.w.Write(b)
	ss.w.WriteByte('\n')
	ss.w.Flush()
}

func (s *Server) serve(ctx context.Context, conn *net.UnixConn) {
	defer conn.Close()
	p, err := peerInfo(conn)
	if err != nil || p.UID != os.Getuid() {
		return
	}
	ss := &session{s: s, conn: conn, w: bufio.NewWriter(conn), role: RoleAgent, pid: p.PID, peer: p}
	r := bufio.NewReaderSize(conn, 1<<16)
	var unsub func()
	defer func() {
		if unsub != nil {
			unsub()
		}
		s.releaseUI(ss)
	}()
	for {
		line, err := r.ReadBytes('\n')
		if err != nil {
			return
		}
		if len(line) > 1<<20 {
			return
		}
		var req Request
		if err := json.Unmarshal(line, &req); err != nil {
			ss.send(Reply{OK: false, Error: "bad request: " + err.Error()})
			continue
		}
		if req.Op == "hello" {
			var a struct {
				Role   string `json:"role"`
				Client string `json:"client"`
			}
			_ = json.Unmarshal(req.Args, &a)
			ss.client = sanitizeClient(a.Client)
			if a.Role == RoleUI {
				err := s.UI.Allows(p)
				if err == nil && ss.role != RoleUI && !s.claimUI(ss) {
					err = errors.New("another shell UI is already connected")
				}
				if err != nil {
					_, _ = s.Core.Audit.Append("refuse", ss.actor(), "ui role refused: "+err.Error(),
						map[string]any{"pid": p.PID, "exe": p.Exe, "context": p.Context, "ui_check": s.UI.Mode})
					ss.send(Reply{ID: req.ID, Error: "the ui role is reserved for the shell UI (" + err.Error() + ")"})
					continue
				}
				ss.role = RoleUI
				if unsub == nil {
					ch, cancel := s.Core.Subscribe()
					unsub = cancel
					go func() {
						for ev := range ch {
							ss.send(ev)
						}
					}()
				}
			}
			ss.send(Reply{ID: req.ID, OK: true, Result: map[string]any{"role": ss.role, "version": Version, "domain": p.Type, "ui_check": s.UI.Mode}})
			continue
		}
		// Long operations (waiting for a confirmation, the assistant) run
		// concurrently so that the UI connection keeps flowing.
		go func(req Request) {
			res, err := ss.handle(ctx, req)
			if err != nil {
				ss.send(Reply{ID: req.ID, Error: err.Error()})
				return
			}
			ss.send(Reply{ID: req.ID, OK: true, Result: res})
		}(req)
	}
}

// Version of the daemon (set by main).
var Version = "dev"

func (ss *session) actor() string {
	if ss.role == RoleUI {
		return "ui"
	}
	c := ss.client
	if c == "" {
		c = "pid " + strconv.Itoa(ss.pid)
	}
	if ss.peer.Type != "" {
		// The domain comes from the kernel; the client name is only a label.
		return "agent:" + c + " (" + ss.peer.Type + ")"
	}
	return "agent:" + c
}

// meta describes this connection for proposals.
func (ss *session) meta(origin, request string) Meta {
	return Meta{Origin: origin, Actor: ss.actor(), Request: request, PID: ss.pid, Domain: ss.peer.Type}
}

// sanitizeClient keeps a client's self-chosen name short and printable.
func sanitizeClient(s string) string {
	var b strings.Builder
	for _, r := range s {
		if b.Len() >= 40 {
			break
		}
		if r == '-' || r == '_' || r == '.' || r == ':' || r == '/' || (r >= '0' && r <= '9') || (r >= 'a' && r <= 'z') || (r >= 'A' && r <= 'Z') {
			b.WriteRune(r)
		}
	}
	return b.String()
}

func decode(raw json.RawMessage, v any) error {
	if len(raw) == 0 {
		return nil
	}
	d := json.NewDecoder(strings.NewReader(string(raw)))
	return d.Decode(v)
}

func (ss *session) requireUI() error {
	if ss.role != RoleUI {
		return errors.New("only the shell UI may do this (agents propose, the person confirms)")
	}
	return nil
}

// Catalog describes the actions for clients.
func Catalog() []map[string]any {
	out := make([]map[string]any, 0, len(Actions))
	for _, a := range Actions {
		out = append(out, map[string]any{"name": a.Name, "title": a.Title, "description": a.Description, "params": a.Params})
	}
	return out
}

func (ss *session) handle(ctx context.Context, req Request) (any, error) {
	c := ss.s.Core
	switch req.Op {
	case "ping":
		return "pong", nil
	case "state":
		d := c.Refresh(ctx)
		return map[string]any{
			"desktop": d, "theme": c.Theme(), "pending": c.Pending(), "activity": c.Audit.Tail(60),
			"actions": Catalog(), "assistant": c.Assistant != nil && c.Assistant.Available(),
			"translator": c.translator() != nil, "version": Version, "control": c.ControlState(),
			"ui_check": ss.s.UI, "agent_io": c.AgentIO(), "keyboard": c.KeyboardIndicator(ctx),
			"voice": c.VoiceStatus(), "grants": grantsOf(c), "skills": c.Skills != nil,
			// The shell UI's own language is the session's (the desktop's
			// texts); the voice and the answers have the person's own
			// language settings.
			"ui_lang": i18n.SessionTag(), "ui_catalog": uiCatalog(),
			"models": modelsOf(c),
		}, nil
	case "desktop":
		return c.Refresh(ctx), nil
	case "theme":
		return c.Theme(), nil
	case "actions":
		return Catalog(), nil
	case "apps":
		var a struct {
			Query string `json:"query"`
		}
		_ = decode(req.Args, &a)
		list := c.Apps()
		if a.Query == "" {
			return list, nil
		}
		var out []apps.App
		q := strings.ToLower(a.Query)
		for _, x := range list {
			if strings.Contains(strings.ToLower(x.Name), q) || strings.Contains(strings.ToLower(x.ID), q) ||
				strings.Contains(strings.ToLower(x.Generic), q) || strings.Contains(strings.ToLower(strings.Join(x.Keywords, " ")), q) {
				out = append(out, x)
			}
		}
		return out, nil
	case "activity":
		var a struct {
			N int `json:"n"`
		}
		_ = decode(req.Args, &a)
		return c.Audit.Tail(a.N), nil
	case "pending":
		return c.Pending(), nil
	case "proposal":
		var a struct {
			ID string `json:"id"`
		}
		_ = decode(req.Args, &a)
		p, ok := c.Get(a.ID)
		if !ok {
			return nil, fmt.Errorf("no proposal %s", a.ID)
		}
		return p, nil

	case "propose":
		// Any client: the actions wait for the person's confirmation in
		// the shell UI. With wait, the reply comes after the decision.
		var a struct {
			Calls   []Call `json:"calls"`
			Request string `json:"request"`
			Wait    int    `json:"wait"` // seconds, 0 = return at once
		}
		if err := decode(req.Args, &a); err != nil {
			return nil, err
		}
		origin := "ipc"
		if strings.HasPrefix(ss.client, "mcp") {
			origin = "mcp"
		}
		pr, err := c.Propose(ctx, ss.meta(origin, a.Request), a.Calls)
		if err != nil {
			return nil, err
		}
		if a.Wait <= 0 {
			return pr.public(), nil
		}
		if a.Wait > 600 {
			a.Wait = 600
		}
		wctx, cancel := context.WithTimeout(ctx, time.Duration(a.Wait)*time.Second)
		defer cancel()
		p, err := c.Wait(wctx, pr.ID)
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return p, nil
	case "wait":
		var a struct {
			ID   string `json:"id"`
			Wait int    `json:"wait"`
		}
		_ = decode(req.Args, &a)
		if a.Wait <= 0 || a.Wait > 600 {
			a.Wait = 120
		}
		wctx, cancel := context.WithTimeout(ctx, time.Duration(a.Wait)*time.Second)
		defer cancel()
		p, err := c.Wait(wctx, a.ID)
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return p, nil

	case "execute":
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		var a struct {
			Calls []Call `json:"calls"`
		}
		if err := decode(req.Args, &a); err != nil {
			return nil, err
		}
		pr, err := c.Execute(ctx, "ui", a.Calls)
		if err != nil {
			return nil, err
		}
		return pr.public(), nil
	case "decide":
		if err := ss.requireUI(); err != nil {
			_, _ = c.Audit.Append("refuse", ss.actor(), "confirmation refused: not the shell UI", map[string]any{"pid": ss.pid, "context": ss.peer.Context})
			return nil, err
		}
		var a struct {
			ID      string         `json:"id"`
			Approve bool           `json:"approve"`
			Edits   map[string]any `json:"edits"`
		}
		if err := decode(req.Args, &a); err != nil {
			return nil, err
		}
		return c.DecideEdited(ctx, a.ID, a.Approve, "ui", a.Edits)
	case "edit":
		// The person's edits of an editable proposal (an e-mail draft)
		// before deciding; where the gate decides, the edited plan becomes
		// the request the person then approves.
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		var a struct {
			ID    string         `json:"id"`
			Edits map[string]any `json:"edits"`
		}
		if err := decode(req.Args, &a); err != nil {
			return nil, err
		}
		return c.EditProposal(ctx, a.ID, a.Edits, "ui")
	case "gate":
		return c.Gate(), nil
	case "assistant.submit":
		// Queue a system assistant proposal in the approval gate; the shell
		// UI then decides there (administrator authentication through
		// polkit) and the gate's executor applies it.
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		var a struct {
			ID string `json:"id"`
		}
		_ = decode(req.Args, &a)
		return c.AssistantSubmit(ctx, a.ID)
	case "ask":
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		var a struct {
			Text string `json:"text"`
		}
		_ = decode(req.Args, &a)
		return c.Ask(ctx, a.Text), nil
	case "theme.save_as":
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		var a struct {
			ID   string `json:"id"`
			Name string `json:"name"`
		}
		_ = decode(req.Args, &a)
		f, err := c.Themes.SaveAs(a.ID, a.Name)
		if err != nil {
			return nil, err
		}
		_, _ = c.Audit.Append("apply", "ui", "saved theme "+f.ID, nil)
		c.Broadcast("theme", c.Theme())
		return map[string]any{"id": f.ID}, nil
	case "reload":
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		if err := c.Themes.Load(); err != nil {
			return nil, err
		}
		c.ApplyTheme(ctx)
		return c.Theme(), nil

	case "choose":
		// Any client may ask the person a question with fixed options
		// (the screen-share output chooser uses this); only the UI answers.
		var a struct {
			Title   string   `json:"title"`
			Body    string   `json:"body"`
			Options []Option `json:"options"`
			Wait    int      `json:"wait"`
		}
		if err := decode(req.Args, &a); err != nil {
			return nil, err
		}
		if a.Wait <= 0 || a.Wait > 300 {
			a.Wait = 120
		}
		wctx, cancel := context.WithTimeout(ctx, time.Duration(a.Wait)*time.Second)
		defer cancel()
		v, err := c.Choose(wctx, ss.actor(), a.Title, a.Body, a.Options)
		if err != nil && !errors.Is(err, context.DeadlineExceeded) {
			return nil, err
		}
		return map[string]any{"choice": v}, nil
	case "chosen":
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		if c.recentInput() {
			return nil, errors.New("an answer right after agent input is not accepted; answer again")
		}
		var a struct {
			ID     string `json:"id"`
			Choice string `json:"choice"`
		}
		_ = decode(req.Args, &a)
		return nil, c.Chosen(a.ID, a.Choice)
	case "assistant.pending":
		if c.Assistant == nil || !c.Assistant.Available() {
			return []any{}, nil
		}
		return c.Assistant.Pending(ctx)
	case "assistant.show":
		var a struct {
			ID string `json:"id"`
		}
		_ = decode(req.Args, &a)
		if c.Assistant == nil {
			return nil, errors.New("the system assistant is not installed")
		}
		return c.Assistant.Show(ctx, a.ID)
	case "assistant.apply":
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		if c.recentInput() {
			return nil, errors.New("an Apply right after agent input is not accepted; apply again")
		}
		var a struct {
			ID   string `json:"id"`
			Code string `json:"code"`
		}
		_ = decode(req.Args, &a)
		out, err := c.AssistantApply(ctx, a.ID, a.Code)
		return map[string]any{"output": out, "ok": err == nil}, nil
	case "assistant.ignore":
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		var a struct {
			ID string `json:"id"`
		}
		_ = decode(req.Args, &a)
		out, err := c.AssistantIgnore(ctx, a.ID)
		return map[string]any{"output": out, "ok": err == nil}, nil
	case "drivers.state":
		// Additional drivers: the assistant's report (no change).
		if c.Assistant == nil || !c.Assistant.Available() {
			return nil, errors.New(i18n.G("The system assistant (basalt) is not installed: Additional drivers needs it."))
		}
		raw, err := c.Assistant.Drivers(ctx)
		if errors.Is(err, assistant.ErrDriversUnsupported) {
			// An assistant older than `basalt drivers`: the page says
			// driver installation is coming soon, never an error.
			return map[string]any{"coming_soon": true, "reason": "assistant"}, nil
		}
		return raw, err
	case "drivers.propose", "drivers.rollback":
		// Store the assistant's proposal; the shell UI then shows it on the
		// confirmation sheet and applies it with assistant.apply.
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		if c.recentInput() {
			return nil, errors.New("a request right after agent input is not accepted; ask again")
		}
		if c.Assistant == nil || !c.Assistant.Available() {
			return nil, errors.New(i18n.G("The system assistant (basalt) is not installed: Additional drivers needs it."))
		}
		var a struct {
			Variant string `json:"variant"`
		}
		_ = decode(req.Args, &a)
		var p assistant.Proposal
		var err error
		if req.Op == "drivers.rollback" {
			p, err = c.Assistant.DriversRollback(ctx)
		} else {
			p, err = c.Assistant.DriversPropose(ctx, a.Variant)
		}
		data := map[string]any{"assistant_proposal": p.ID, "variant": a.Variant}
		if err != nil {
			data["error"] = err.Error()
		}
		_, _ = c.Audit.Append("propose", "ui", req.Op, data)
		if err != nil {
			return nil, err
		}
		return p, nil
	case "updates.restart":
		// The restart into the staged offline update, when the power
		// menu's countdown ends (or Restart now): the assistant's unit
		// basalt-offline-reboot.service, which refuses without one.
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		if c.recentInput() {
			return nil, errors.New("a restart right after agent input is not accepted; choose again")
		}
		if c.Assistant == nil || !c.Assistant.Available() {
			return nil, errors.New(i18n.G("The system assistant (basalt) is not installed: Updates and channels needs it."))
		}
		err := c.Assistant.RestartIntoUpdate(ctx)
		data := map[string]any{"unit": "basalt-offline-reboot.service"}
		if err != nil {
			data["error"] = err.Error()
		}
		_, _ = c.Audit.Append("apply", "ui", "restart into the staged offline update", data)
		return map[string]any{"ok": err == nil}, err
	case "updates.progress":
		// An update being applied: its step (no dnf query).
		if c.Assistant == nil || !c.Assistant.Available() {
			return nil, errors.New(i18n.G("The system assistant (basalt) is not installed: Updates and channels needs it."))
		}
		return c.Assistant.UpdatesProgress(ctx)
	case "updates.state", "channels.state":
		// Settings, Updates and channels: the assistant's reports (no change).
		if c.Assistant == nil || !c.Assistant.Available() {
			return nil, errors.New(i18n.G("The system assistant (basalt) is not installed: Updates and channels needs it."))
		}
		var raw json.RawMessage
		var err error
		if req.Op == "updates.state" {
			raw, err = c.Assistant.Updates(ctx)
		} else {
			raw, err = c.Assistant.Channels(ctx)
		}
		if errors.Is(err, assistant.ErrUpdatesUnsupported) {
			return map[string]any{"coming_soon": true}, nil
		}
		return raw, err
	case "updates.check":
		// update.check: refresh the package lists (nothing is installed).
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		if c.Assistant == nil || !c.Assistant.Available() {
			return nil, errors.New(i18n.G("The system assistant (basalt) is not installed: Updates and channels needs it."))
		}
		raw, err := c.Assistant.UpdatesCheck(ctx)
		data := map[string]any{"action": "update.check"}
		if err != nil {
			data["error"] = err.Error()
		}
		_, _ = c.Audit.Append("ask", "ui", "check for updates", data)
		return raw, err
	case "updates.propose", "updates.rollback", "channels.propose":
		// Store the assistant's proposal (update.install, update.rollback,
		// repo.enable, repo.disable, source.add, source.remove); the page
		// shows it and applies it with assistant.apply, or the approval
		// gate decides it, like any other proposal.
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		if c.recentInput() {
			return nil, errors.New("a request right after agent input is not accepted; ask again")
		}
		if c.Assistant == nil || !c.Assistant.Available() {
			return nil, errors.New(i18n.G("The system assistant (basalt) is not installed: Updates and channels needs it."))
		}
		var a struct {
			Security bool `json:"security"`
			assistant.ChannelRequest
		}
		_ = decode(req.Args, &a)
		var p assistant.Proposal
		var err error
		data := map[string]any{}
		switch req.Op {
		case "updates.propose":
			p, err = c.Assistant.UpdatesPropose(ctx, a.Security)
			data["security"] = a.Security
		case "updates.rollback":
			p, err = c.Assistant.UpdatesRollback(ctx)
		default:
			p, err = c.Assistant.ChannelsPropose(ctx, a.ChannelRequest)
			data["request"] = a.ChannelRequest
		}
		data["assistant_proposal"] = p.ID
		if err != nil {
			data["error"] = err.Error()
		}
		_, _ = c.Audit.Append("propose", "ui", req.Op, data)
		if err != nil {
			return nil, err
		}
		return p, nil
	case "keyboard.state":
		// Settings, Keyboard: the person's settings, the system's, the
		// layouts in use (read only).
		return c.KeyboardInfo(ctx), nil
	case "keyboard.layouts":
		// The picker: every layout and variant of the system's registry.
		return c.KeyboardLayouts()
	case "keyboard.indicator":
		return c.KeyboardIndicator(ctx), nil
	case "keyboard.set":
		// Only the person, in Settings, changes their keyboard.
		if err := ss.requireUI(); err != nil {
			_, _ = c.Audit.Append("refuse", ss.actor(), "keyboard settings refused: not the shell UI", map[string]any{"pid": ss.pid})
			return nil, err
		}
		if c.recentInput() {
			return nil, errors.New("a change right after agent input is not accepted; choose again")
		}
		var s keyboard.Settings
		if err := decode(req.Args, &s); err != nil {
			return nil, err
		}
		if err := c.SetKeyboard(ctx, s); err != nil {
			return nil, err
		}
		return c.KeyboardInfo(ctx), nil
	case "keyboard.switch":
		// The panel's indicator: the next layout, or one.
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		a := struct {
			Index *int `json:"index"`
		}{}
		_ = decode(req.Args, &a)
		i := -1
		if a.Index != nil {
			i = *a.Index
		}
		if err := c.SwitchKeyboardLayout(ctx, i); err != nil {
			return nil, err
		}
		return c.KeyboardIndicator(ctx), nil
	case "keyboard.system":
		// "Use for the login screen and new accounts too": store the
		// assistant's keyboard.system proposal; the page shows it and
		// applies it with assistant.apply (or the approval gate), like
		// any other system change. The shell never runs localectl.
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		if c.recentInput() {
			return nil, errors.New("a request right after agent input is not accepted; ask again")
		}
		return c.KeyboardSystemProposal(ctx)
	case "toplevels":
		d := c.Refresh(ctx)
		return d.Windows, nil
	case "capture":
		// A screenshot for this agent: inside its control session, or
		// after the person confirmed it.
		var a struct {
			Args map[string]any `json:"args"`
			Wait int            `json:"wait"`
		}
		if err := decode(req.Args, &a); err != nil {
			return nil, err
		}
		if ss.role == RoleUI {
			return nil, errors.New("the shell UI does not take screenshots for itself")
		}
		if a.Wait <= 0 || a.Wait > 600 {
			a.Wait = 180
		}
		origin := "ipc"
		if strings.HasPrefix(ss.client, "mcp") {
			origin = "mcp"
		}
		return c.Capture(ctx, ss.meta(origin, ""), a.Args, time.Duration(a.Wait)*time.Second)
	case "input":
		var in InputRequest
		if err := decode(req.Args, &in); err != nil {
			return nil, err
		}
		if ss.role == RoleUI {
			return nil, errors.New("the shell UI does not inject input")
		}
		return c.Input(ctx, ss.meta("ipc", ""), in)
	case "control":
		return c.ControlState(), nil
	case "control.stop":
		// Anyone may stop a control session: it only takes power away.
		by := ss.actor()
		return map[string]any{"stopped": c.StopControl(by, "control session stopped by "+by)}, nil
	case "ui.state":
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		var a struct {
			Modal  bool `json:"modal"`
			Locked bool `json:"locked"`
		}
		_ = decode(req.Args, &a)
		c.SetUIModal(a.Modal)
		c.SetUILocked(a.Locked)
		return nil, nil
	case "voice.press":
		// Push to talk: only the shell UI (the key binding reaches it
		// through Quickshell IPC; the panel button is in it). Agents can
		// never open the microphone.
		if err := ss.requireUI(); err != nil {
			_, _ = c.Audit.Append("refuse", ss.actor(), "microphone refused: not the shell UI", map[string]any{"pid": ss.pid, "context": ss.peer.Context})
			return nil, err
		}
		// latch: no release will follow (niri's key bindings), so this
		// utterance is "press to start and stop".
		var a struct {
			CommandBar bool `json:"commandbar"`
			Latch      bool `json:"latch"`
		}
		_ = decode(req.Args, &a)
		return nil, c.VoiceKeyDown(ctx, a.CommandBar, a.Latch)
	case "voice.release":
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		return nil, c.VoiceRelease(ctx)
	case "voice.cancel":
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		c.VoiceCancel(ctx)
		return nil, nil
	case "voice.settings":
		// The person's voice and assistant settings, for the Settings
		// window only.
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		return c.VoiceSettings(ctx), nil
	case "voice.settings.set":
		// Only the person, in the Settings window, changes them; values
		// outside the administrator's policy are refused.
		if err := ss.requireUI(); err != nil {
			_, _ = c.Audit.Append("refuse", ss.actor(), "voice settings refused: not the shell UI", map[string]any{"pid": ss.pid})
			return nil, err
		}
		var p voiceprefs.Prefs
		if err := decode(req.Args, &p); err != nil {
			return nil, err
		}
		if err := c.SetVoiceSettings(ctx, p); err != nil {
			return nil, err
		}
		return c.VoiceSettings(ctx), nil
	case "voice.status":
		st := c.VoiceStatus()
		return map[string]any{"voice": st, "dictation": c.DictationState()}, nil
	case "models", "models.list", "models.download", "models.dismiss", "models.retry", "models.cancel", "models.remove":
		// The consented model downloads (zero setup): only the person, in
		// the shell UI, agrees to a download or removes a model; an agent
		// can neither start one nor see the offers.
		if err := ss.requireUI(); err != nil {
			_, _ = c.Audit.Append("refuse", ss.actor(), req.Op+" refused: not the shell UI", map[string]any{"pid": ss.pid})
			return nil, err
		}
		return c.modelsOp(ctx, req.Op, req.Args)
	case "grants":
		if c.Skills == nil {
			return []any{}, nil
		}
		return c.Skills.Store.Active(""), nil
	case "grant.revoke":
		// Taking power away needs no confirmation, but only the person.
		if err := ss.requireUI(); err != nil {
			return nil, err
		}
		if c.Skills == nil {
			return 0, nil
		}
		var a struct {
			ID string `json:"id"`
		}
		_ = decode(req.Args, &a)
		n := c.Skills.Store.Revoke(a.ID)
		_, _ = c.Audit.Append("apply", "ui", fmt.Sprintf("revoked %d grants", n), map[string]any{"id": a.ID})
		return n, nil
	case "audit.verify":
		n, err := audit.Verify(c.Audit.Path())
		if err != nil {
			return map[string]any{"ok": false, "records": n, "error": err.Error()}, nil
		}
		return map[string]any{"ok": true, "records": n}, nil
	}
	log.Printf("unknown op %q from %s", req.Op, ss.actor())
	return nil, fmt.Errorf("unknown op %q", req.Op)
}

var (
	uiCatOnce sync.Once
	uiCat     map[string][]string
)

// uiCatalog is the translation catalog of the session's language for the
// shell UI (nil for English).
func uiCatalog() map[string][]string {
	uiCatOnce.Do(func() { uiCat = i18n.Catalog(i18n.LocaleName(i18n.SessionTag())) })
	return uiCat
}

func grantsOf(c *Core) any {
	if c.Skills == nil {
		return []any{}
	}
	return c.Skills.Store.Active("")
}

func modelsOf(c *Core) any {
	if c.Models == nil {
		return nil
	}
	return c.Models.Snapshot()
}

// modelsOp serves the UI's model download requests (shell UI only).
func (c *Core) modelsOp(ctx context.Context, op string, raw json.RawMessage) (any, error) {
	if c.Models == nil {
		return nil, errors.New("model downloads are not available")
	}
	var a struct {
		ID     string `json:"id"`
		Kind   string `json:"kind"`
		Target string `json:"target"`
	}
	if err := decode(raw, &a); err != nil {
		return nil, err
	}
	switch op {
	case "models":
		return c.Models.Snapshot(), nil
	case "models.list":
		return c.ModelsList(ctx), nil
	case "models.download":
		// An offer the person accepted (the voice card, the model card),
		// or a model chosen in Settings. Where the approval gate decides
		// model downloads, the Download is the person's decision there.
		if c.gateMode("models") == gateEnforce {
			return c.modelsDownloadViaGate(ctx, a.ID, a.Kind, a.Target)
		}
		var j models.Job
		var err error
		if a.ID != "" {
			j, err = c.AcceptOffer(a.ID)
		} else {
			j, err = c.StartDownload(ctx, a.Kind, a.Target)
		}
		if err == nil {
			c.modelsObserve(j.Kind, j.Target, j.Purpose)
		}
		return j, err
	case "models.dismiss":
		c.DismissOffer(a.ID)
		return nil, nil
	case "models.retry":
		return c.Models.Retry(a.ID)
	case "models.cancel":
		c.CloseDownload(a.ID)
		return nil, nil
	case "models.remove":
		err := c.Models.Remove(ctx, a.Kind, a.Target)
		data := map[string]any{"kind": a.Kind, "what": a.Target}
		if err != nil {
			data["error"] = err.Error()
			_, _ = c.Audit.Append("refuse", "ui", "model not removed: "+a.Kind+" "+a.Target, data)
			return nil, err
		}
		_, _ = c.Audit.Append("apply", "ui", "model removed: "+a.Kind+" "+a.Target, data)
		c.Ledger.Append("model.remove.consent", "allowed", "", data)
		if a.Kind == "llm" && !models.HasLLMModel() {
			c.reloadLocalModel()
		}
		c.Broadcast("voice-settings", c.VoiceSettings(ctx))
		return c.ModelsList(ctx), nil
	}
	return nil, fmt.Errorf("unknown op %q", op)
}
