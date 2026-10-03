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
	"syscall"
	"time"

	"github.com/openbasalt/basalt-shell/internal/apps"
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
	// UIExecutables are the program names allowed to take the ui role
	// (checked through the peer's /proc/PID/exe).
	UIExecutables []string
	// InsecureUI accepts any same-user peer as ui (development only).
	InsecureUI bool
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
	if err := os.MkdirAll(filepath.Dir(s.Path), 0o700); err != nil {
		return err
	}
	_ = os.Chmod(filepath.Dir(s.Path), 0o700)
	if c, err := net.Dial("unix", s.Path); err == nil {
		c.Close()
		return fmt.Errorf("another basalt-shell daemon is listening on %s", s.Path)
	}
	_ = os.Remove(s.Path)
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

// peer returns the uid and pid of the other end.
func peer(c *net.UnixConn) (uid, pid int, err error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return 0, 0, err
	}
	var cred *syscall.Ucred
	var cerr error
	if err := raw.Control(func(fd uintptr) {
		cred, cerr = syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
	}); err != nil {
		return 0, 0, err
	}
	if cerr != nil {
		return 0, 0, cerr
	}
	return int(cred.Uid), int(cred.Pid), nil
}

func (s *Server) isUI(pid int) bool {
	if s.InsecureUI {
		return true
	}
	exe, err := os.Readlink(fmt.Sprintf("/proc/%d/exe", pid))
	if err != nil {
		return false
	}
	base := filepath.Base(exe)
	for _, n := range s.UIExecutables {
		if base == n {
			return true
		}
	}
	return false
}

type session struct {
	s      *Server
	conn   *net.UnixConn
	wmu    sync.Mutex
	w      *bufio.Writer
	role   string
	client string
	pid    int
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
	uid, pid, err := peer(conn)
	if err != nil || uid != os.Getuid() {
		return
	}
	ss := &session{s: s, conn: conn, w: bufio.NewWriter(conn), role: RoleAgent, pid: pid}
	r := bufio.NewReaderSize(conn, 1<<16)
	var unsub func()
	defer func() {
		if unsub != nil {
			unsub()
		}
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
			ss.client = a.Client
			if a.Role == RoleUI {
				if !s.isUI(pid) {
					ss.send(Reply{ID: req.ID, Error: "the ui role is reserved for the shell UI"})
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
			ss.send(Reply{ID: req.ID, OK: true, Result: map[string]any{"role": ss.role, "version": Version}})
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
	return "agent:" + c
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
			"translator": c.Translator != nil, "version": Version,
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
		pr, err := c.Propose(ctx, Meta{Origin: origin, Actor: ss.actor(), Request: a.Request}, a.Calls)
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
			return nil, err
		}
		var a struct {
			ID      string `json:"id"`
			Approve bool   `json:"approve"`
		}
		if err := decode(req.Args, &a); err != nil {
			return nil, err
		}
		return c.Decide(ctx, a.ID, a.Approve, "ui")
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
	}
	log.Printf("unknown op %q from %s", req.Op, ss.actor())
	return nil, fmt.Errorf("unknown op %q", req.Op)
}
