// Package shell is the core of basalt-shell's daemon: desktop state from
// the compositor adapter, the theme store, the closed set of typed
// actions, proposals that wait for the person's confirmation, the audit
// log, and the event fan-out to the shell's UI.
package shell

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math"
	"reflect"
	"strings"
	"sync"
	"time"

	"github.com/basalt-os/basalt-shell/internal/agentio"
	"github.com/basalt-os/basalt-shell/internal/appearance"
	"github.com/basalt-os/basalt-shell/internal/apps"
	"github.com/basalt-os/basalt-shell/internal/assistant"
	"github.com/basalt-os/basalt-shell/internal/audit"
	"github.com/basalt-os/basalt-shell/internal/compositor"
	"github.com/basalt-os/basalt-shell/internal/decor"
	"github.com/basalt-os/basalt-shell/internal/hw"
	"github.com/basalt-os/basalt-shell/internal/intent"
	"github.com/basalt-os/basalt-shell/internal/ledger"
	"github.com/basalt-os/basalt-shell/internal/models"
	"github.com/basalt-os/basalt-shell/internal/skills"
	"github.com/basalt-os/basalt-shell/internal/theme"
	"github.com/basalt-os/basalt-shell/internal/voice"
	"github.com/basalt-os/basalt-shell/internal/wlime"
	"github.com/basalt-os/basalt-shell/internal/wlvirt"
)

// Call is a request for one typed action.
type Call struct {
	Action string         `json:"action"`
	Args   map[string]any `json:"args"`
}

// Proposal statuses.
const (
	StatusPending  = "pending"
	StatusApplied  = "applied"
	StatusDeclined = "declined"
	StatusExpired  = "expired"
	StatusFailed   = "failed"
	StatusStale    = "stale"
	StatusRefused  = "refused" // the approval gate refused it (a rule, a hard limit)
)

// Proposal is a set of actions waiting for the person's decision.
type Proposal struct {
	ID     string `json:"id"`
	Origin string `json:"origin"` // mcp, ipc, commandbar
	Actor  string `json:"actor"`
	// From is who asks, in plain words for the confirmation sheet (an
	// app's name, an agent's), and FromKind "app" or "agent"; the
	// technical facts (pid, SELinux domain) stay in the activity log.
	From      string         `json:"from,omitempty"`
	FromKind  string         `json:"from_kind,omitempty"`
	Request   string         `json:"request,omitempty"`
	Calls     []Call         `json:"calls"`
	Steps     []string       `json:"steps"`
	Diff      []theme.Change `json:"diff,omitempty"`
	Created   time.Time      `json:"created"`
	Expires   time.Time      `json:"expires"`
	Status    string         `json:"status"`
	Error     string         `json:"error,omitempty"`
	Result    []any          `json:"result,omitempty"`
	Decided   string         `json:"decided_by,omitempty"`
	Explain   string         `json:"explain,omitempty"`   // how the request was understood
	Backend   string         `json:"backend,omitempty"`   // rules or model
	Assistant map[string]any `json:"assistant,omitempty"` // a system assistant proposal, when this wraps one
	// Previews are the exact content of acting steps (see step.Preview).
	Previews []map[string]any `json:"previews,omitempty"`
	// Editable are the parameters the person may change when confirming.
	Editable []string `json:"editable,omitempty"`
	Edited   bool     `json:"edited,omitempty"`
	// Gate is the proposal's request in the approval gate, when the gate
	// decides this path (the shell UI decides there, not here).
	Gate *GateInfo `json:"gate,omitempty"`
	// GateMode: "enforce" (the gate decides), "observe" (the shell decides
	// and tells the gate) or "" (no gate).
	GateMode string `json:"gate_mode,omitempty"`

	base theme.Settings
	next *theme.Settings
	runs []func(context.Context) (any, error)
	ends []func(string)
	meta Meta
	done chan struct{}
}

// Core is the daemon's state.
type Core struct {
	Comp      compositor.Adapter
	Themes    *theme.Store
	Audit     *audit.Log
	HW        hw.Report
	ConfigDir string
	// ApplyApps pushes tokens to GTK/Qt/portals (off in tests).
	ApplyApps bool
	// ProposalTTL is how long a proposal waits for a decision.
	ProposalTTL time.Duration
	// Translator is the optional language model of the command bar.
	Translator *intent.Model
	// Assistant is the system assistant bridge (nil when not installed).
	Assistant *assistant.Bridge
	// Tools are the screen capture and virtual keyboard programs.
	Tools agentio.Tools
	// Hooks replacing Tools (tests).
	Screenshot func(context.Context, agentio.CaptureSpec) (*agentio.Capture, error)
	TypeText   func(context.Context, string) error
	KeyCombo   func(context.Context, string) error
	// VirtualInput makes the daemon hold its own virtual keyboard and
	// pointer on the compositor's seat (wlvirt) for agent input; Always
	// keeps them for the whole session (a headless session has no input
	// devices otherwise).
	VirtualInput       bool
	VirtualInputAlways bool
	// Skills are the read-only skills (find files, e-mail, web pages);
	// nil when not set up.
	Skills *skills.Engine
	// Voice is the push-to-talk voice service (basalt-voiced); nil when
	// not running.
	Voice *voice.Client
	// ScreenLocked reports a locker program running (swaylock, the
	// fallback; push to talk is refused); replaced in tests. The shell's
	// own lock screen is reported by the UI (SetUILocked); Locked
	// combines both.
	ScreenLocked func() bool
	// Ledger receives the security-relevant records (nil: not running).
	Ledger *ledger.Sink
	// Models offers and follows the consented model downloads (nil: not
	// available); StartVoice starts the voice service (tests replace it).
	Models     *models.Manager
	StartVoice func(context.Context) error
	// PTTTick is how often a "press to start and stop" utterance checks
	// the hold limit and the silence (0: every 200 ms; tests shorten it).
	PTTTick time.Duration
	// PowerRun runs a session or power command (loginctl, systemctl,
	// basalt-lock); tests replace it.
	PowerRun func(ctx context.Context, argv []string) error
	// GateSocket is the approval gate's socket ("" : $BASALT_GATE_SOCKET or
	// the default); GateOff ignores the gate (tests).
	GateSocket string
	GateOff    bool
	gate       gateState
	// grantRules: grant id -> the gate rule its approval became.
	grantRules map[string]string

	mu        sync.Mutex
	proposals map[string]*Proposal
	order     []string
	subs      map[chan Event]struct{}
	appsCache []apps.App
	appsTime  time.Time
	desktop   Desktop
	lastApps  appearance.Result
	choices   map[string]chan string
	control   *Control
	virt      *wlvirt.Device
	uiModal   bool
	uiLocked  bool
	lastInput time.Time
	// voice is the push-to-talk state shown by the UI.
	voice            VoiceState
	voicePress       time.Time
	voiceRoute       voiceRoute
	voiceModel       string          // the speech model chosen when the key went down
	voiceSession     pttSession      // how the open microphone was opened (ptt.go)
	voiceGen         uint64          // the open utterance; bumped when it ends
	speakClient      *voice.Client   // the voice service connection for "speak" (skills.go)
	llmDeclined      bool            // Not now on the local model offer, this session
	voiceLangNoticed map[string]bool // answer languages told "shown, not spoken" this session
	// prefs are the person's voice and assistant settings (voiceprefs.go).
	prefs prefsState
	// kbd is the person's keyboard (keyboard.go).
	kbd kbdState
	// im is the seat's input method (dictation); nil when not held.
	im *wlime.IM
	// appAsks are apps' questions waiting for the command bar (appask.go).
	appAsks map[string]*appAsk
	// dismissed are the assistant reports the person dismissed (dismissed.go).
	dismissed dismissedStore
	// placed remembers windows the shell maximized or snapped: their
	// geometry before (to restore) and the placement given.
	placed map[string]placement
}

// placement is a window the shell maximized or snapped.
type placement struct {
	State  string          // maximized, left, right
	Before compositor.Rect // geometry to restore
	At     compositor.Rect // geometry given
	Since  time.Time       // when; a window is given a moment to get there
	// Unframed: the shell took the compositor's title bar and frame away
	// while the window is maximized; they come back on restore, or when
	// the window leaves the placement (dragged or resized by the person).
	Unframed bool
}

// Event goes to UI subscribers.
type Event struct {
	Event string `json:"event"`
	Data  any    `json:"data"`
}

// Desktop is the compositor state the UI shows.
type Desktop struct {
	Compositor string                 `json:"compositor"`
	Version    string                 `json:"version"`
	Caps       compositor.Caps        `json:"caps"`
	Windows    []compositor.Window    `json:"windows"`
	Workspaces []compositor.Workspace `json:"workspaces"`
	Outputs    []compositor.Output    `json:"outputs"`
}

// WireSkills connects the skills' grants to the audit log and the UI.
func (c *Core) WireSkills() { c.wireGrants() }

// New builds a core.
func New(comp compositor.Adapter, store *theme.Store, log *audit.Log, rep hw.Report, configDir string) *Core {
	c := &Core{Comp: comp, Themes: store, Audit: log, HW: rep, ConfigDir: configDir,
		ApplyApps: true, ProposalTTL: 5 * time.Minute,
		proposals: map[string]*Proposal{}, subs: map[chan Event]struct{}{}, choices: map[string]chan string{},
		placed: map[string]placement{}, ScreenLocked: ScreenLocked}
	log.OnAppend = func(r audit.Record) {
		c.Broadcast("activity", r)
		c.toLedger(r)
	}
	return c
}

// Subscribe returns a channel of events; call the cancel function to stop.
func (c *Core) Subscribe() (chan Event, func()) {
	ch := make(chan Event, 64)
	c.mu.Lock()
	c.subs[ch] = struct{}{}
	c.mu.Unlock()
	return ch, func() {
		c.mu.Lock()
		if _, ok := c.subs[ch]; ok {
			delete(c.subs, ch)
			close(ch)
		}
		c.mu.Unlock()
	}
}

// Broadcast sends an event to every subscriber (dropped for slow ones).
func (c *Core) Broadcast(name string, data any) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for ch := range c.subs {
		select {
		case ch <- Event{Event: name, Data: data}:
		default:
		}
	}
}

// Apps returns the desktop entries (cached for 10 s).
func (c *Core) Apps() []apps.App {
	c.mu.Lock()
	defer c.mu.Unlock()
	if time.Since(c.appsTime) > 10*time.Second || c.appsCache == nil {
		c.appsCache = apps.List()
		c.appsTime = time.Now()
	}
	return c.appsCache
}

// Refresh reloads the desktop state from the compositor and broadcasts it
// when it changed.
func (c *Core) Refresh(ctx context.Context) Desktop {
	d := Desktop{Compositor: c.Comp.Name(), Caps: c.Comp.Caps()}
	d.Version = c.Comp.Version(ctx)
	d.Windows, _ = c.Comp.Windows(ctx)
	d.Workspaces, _ = c.Comp.Workspaces(ctx)
	d.Outputs, _ = c.Comp.Outputs(ctx)
	c.mu.Lock()
	reframe := c.markPlaced(d.Windows)
	changed := !reflect.DeepEqual(c.desktop, d)
	c.desktop = d
	c.mu.Unlock()
	// A maximized window the person dragged or resized out of its place
	// gets its title bar and frame back.
	if f, ok := c.Comp.(compositor.Framer); ok {
		for _, id := range reframe {
			if err := f.SetFrame(ctx, id, true); err != nil {
				log.Printf("window frame: %v", err)
			}
		}
	}
	if changed {
		c.Broadcast("desktop", d)
	}
	return d
}

// markPlaced sets the state of windows the shell maximized or snapped,
// and forgets those that were closed or moved since (c.mu held). It
// returns the windows that left a placement without their frame, which
// get it back.
func (c *Core) markPlaced(wins []compositor.Window) (reframe []string) {
	seen := map[string]bool{}
	for i := range wins {
		w := &wins[i]
		seen[w.ID] = true
		p, ok := c.placed[w.ID]
		if !ok || w.State == "minimized" {
			continue
		}
		switch {
		case near(w.Rect, p.At) && w.Floating:
			w.State = p.State
		case time.Since(p.Since) > 2*time.Second:
			// Moved or resized since (by the person or the app).
			delete(c.placed, w.ID)
			if p.Unframed {
				reframe = append(reframe, w.ID)
			}
		}
	}
	for id := range c.placed {
		if !seen[id] {
			delete(c.placed, id)
		}
	}
	return reframe
}

// near: two rectangles equal within a few pixels (compositors round
// sizes to the app's size increments, terminals to cells).
func near(a, b compositor.Rect) bool {
	d := func(x, y, tol int) bool { return x-y <= tol && y-x <= tol }
	return d(a.X, b.X, 4) && d(a.Y, b.Y, 4) && d(a.W, b.W, 40) && d(a.H, b.H, 40)
}

// Desktop returns the last known desktop state.
func (c *Core) DesktopState() Desktop {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.desktop
}

// Watch follows compositor events until ctx ends, reconnecting.
func (c *Core) Watch(ctx context.Context) {
	for ctx.Err() == nil {
		ch, err := c.Comp.Subscribe(ctx)
		if err != nil {
			select {
			case <-ctx.Done():
				return
			case <-time.After(2 * time.Second):
			}
			continue
		}
		c.Refresh(ctx)
		timer := time.NewTimer(time.Hour)
		timer.Stop()
		pending := false
	loop:
		for {
			select {
			case <-ctx.Done():
				return
			case ev, ok := <-ch:
				if !ok {
					break loop
				}
				if ev.Kind == "keyboard" {
					// The panel's layout indicator (not the window list).
					go c.keyboardChanged(ctx)
					continue
				}
				if !pending {
					pending = true
					timer.Reset(40 * time.Millisecond)
				}
			case <-timer.C:
				pending = false
				c.Refresh(ctx)
			}
		}
		select {
		case <-ctx.Done():
			return
		case <-time.After(time.Second):
		}
	}
}

// ThemeState is what the UI and theme_get return.
type ThemeState struct {
	Settings theme.Settings    `json:"settings"`
	Tokens   theme.Tokens      `json:"tokens"`
	Themes   []theme.Meta      `json:"themes"`
	Specs    []theme.Spec      `json:"specs"`
	HW       hw.Report         `json:"hardware"`
	Apps     appearance.Result `json:"apps_appearance"`
	Errors   []string          `json:"errors,omitempty"`
}

// Theme returns the current theme state.
func (c *Core) Theme() ThemeState {
	s := c.Themes.Settings()
	t, err := c.Themes.Resolve(s, c.HW.Weak)
	st := ThemeState{Settings: s, Tokens: t, Themes: c.Themes.Themes(), Specs: theme.Specs, HW: c.HW, Errors: c.Themes.LoadErrors()}
	c.mu.Lock()
	st.Apps = c.lastApps
	c.mu.Unlock()
	if err != nil {
		st.Errors = append(st.Errors, err.Error())
	}
	return st
}

// ApplyTheme broadcasts the tokens and pushes them to the compositor and
// to applications.
func (c *Core) ApplyTheme(ctx context.Context) {
	st := c.Theme()
	c.Broadcast("theme", st)
	t := st.Tokens
	// elevation.shadow is the dark-mode strength; light surfaces need about
	// half of it (0.35 dark, 0.18 light with the Basalt theme). Inactive
	// windows cast a lighter shadow than the focused one.
	shadowAlpha := t.Num("elevation.shadow")
	if t.Str("mode") == "light" {
		shadowAlpha *= 18.0 / 35.0
	}
	style := compositor.Style{
		CornerRadius:   int(t.Num("radius.window")),
		BorderWidth:    int(t.Num("window.border")),
		Gaps:           int(t.Num("window.gaps")),
		FocusColor:     t.Str("color.accent"),
		InactiveColor:  t.Str("color.border"),
		UrgentColor:    t.Str("color.danger"),
		Shadows:        t.Bool("window.shadows"),
		ShadowColor:    theme.WithAlpha("#000000", shadowAlpha),
		ShadowBlur:     int(t.Num("elevation.blur")),
		ShadowOffsetY:  int(t.Num("spacing.unit")),
		ShadowInactive: theme.WithAlpha("#000000", shadowAlpha*4/7),
		Blur:           t.Bool("window.blur"),
		LayerBlur:      true,
		DimInactive:    t.Num("window.dimInactive"),
		Animations:     t.Str("motion") == "full",
		CursorTheme:    t.Str("apps.cursorTheme"),
		CursorSize:     int(t.Num("apps.cursorSize")),
		Title:          TitleStyle(t),
	}
	// Compositor effects (SwayFX shadows, blur, dimming) cost GPU time
	// every frame; with software rendering, few CPUs, little memory or
	// headless (hw.Probe's "weak") they are off whatever the theme says.
	if c.HW.Weak {
		style.Shadows, style.Blur, style.LayerBlur, style.DimInactive = false, false, false, 0
	}
	// The frame: the theme's border color on inactive windows, a strong
	// border on the focused one (at least 3:1 against the background and
	// the surfaces); the accent stays for focus inside apps.
	style.FocusColor = FocusFrame(t)
	style.InactiveColor = t.Str("color.border")
	style.Accent = t.Str("color.accent")
	if err := c.Comp.ApplyStyle(ctx, style); err != nil && !errors.Is(err, compositor.ErrNoCompositor) {
		log.Printf("compositor style: %v", err)
	}
	if c.ApplyApps {
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
			defer cancel()
			ls, ds := st.Settings.Clone(), st.Settings.Clone()
			ls.Mode, ds.Mode = "light", "dark"
			lt, err1 := c.Themes.Resolve(ls, c.HW.Weak)
			dt, err2 := c.Themes.Resolve(ds, c.HW.Weak)
			if err1 != nil || err2 != nil {
				lt, dt = t, t
			}
			r := appearance.Apply(ctx, t, lt, dt, c.ConfigDir, appearance.Options{ButtonLayout: decor.ButtonLayout(c.Comp.Caps().ClientMaximize, c.Comp.Caps().ClientMinimize)})
			c.mu.Lock()
			c.lastApps = r
			c.mu.Unlock()
			for _, e := range r.Errors {
				log.Printf("appearance: %s", e)
			}
		}()
	}
}

// FocusFrame is the focused window's frame color: the design system's
// borderStrong, derived until it is a token from the muted text over the
// surface (70 %), moved toward the muted text, then the text, until it
// reaches 3:1 against color.bg and color.surface (WCAG 1.4.11).
func FocusFrame(t theme.Tokens) string {
	surface, muted, text := t.Str("color.surface"), t.Str("color.textMuted"), t.Str("color.text")
	ok := func(c string) bool {
		return theme.Contrast(c, t.Str("color.bg")) >= 3 && theme.Contrast(c, surface) >= 3
	}
	for k := 0.7; k <= 1.0001; k += 0.05 {
		if c := theme.Mix(surface, muted, k); ok(c) {
			return c
		}
	}
	for k := 0.1; k <= 1.0001; k += 0.1 {
		if c := theme.Mix(muted, text, k); ok(c) {
			return c
		}
	}
	return text
}

// TitleStyle is the compositor title bar of a token set: the focused
// title on the raised surface, inactive ones on the plain surface with
// muted text, the interface font a little smaller than body text.
func TitleStyle(t theme.Tokens) compositor.TitleStyle {
	unit := int(t.Num("spacing.unit"))
	return compositor.TitleStyle{
		Font:  t.Str("font.family") + " SemiBold",
		Size:  math.Max(8, t.Num("font.size")-1),
		Align: "center",
		PadX:  unit * 3,
		// 12 x 5 with the 4 px unit: a bar of about 27 px at 10 pt.
		PadY:        unit + unit/4,
		FocusedBg:   t.Str("color.surfaceAlt"),
		FocusedText: t.Str("color.text"),
		InactiveBg:  t.Str("color.surface"),
		InactiveTxt: t.Str("color.textMuted"),
		UrgentBg:    t.Str("color.danger"),
		UrgentText:  "#ffffff",
		Radius:      int(t.Num("radius.window")),
		ColorScheme: t.Str("mode"),
	}
}

// toLedger forwards the records that matter for security to basalt-ledger:
// proposals and their outcome (with the exact previews of acting steps),
// edits, refusals, grants and skill sessions. Not the theme slider moves
// or the spoken words.
func (c *Core) toLedger(r audit.Record) {
	if c.Ledger == nil {
		return
	}
	_, isProposal := r.Data["proposal"]
	switch r.Type {
	case "request", "decline", "expire", "fail", "refuse", "edit", "done", "skill":
	case "apply":
		if !isProposal && !strings.HasPrefix(r.Text, "grant") && !strings.HasPrefix(r.Text, "revoked") {
			return
		}
	default:
		return
	}
	outcome := map[string]string{"decline": "denied", "refuse": "denied", "expire": "denied", "fail": "error"}[r.Type]
	if outcome == "" {
		outcome = "ok"
	}
	data := map[string]any{"text": r.Text, "actor": r.Actor, "shell_seq": r.Seq, "shell_hash": r.Hash}
	for _, k := range []string{"proposal", "previews", "calls", "origin", "changed", "error", "result", "domain"} {
		if v, ok := r.Data[k]; ok {
			data[k] = v
		}
	}
	session := ""
	if sess, ok := r.Data["session"].(map[string]any); ok {
		session, _ = sess["id"].(string)
	}
	c.Ledger.Append("shell."+r.Type, outcome, session, data)
}

func newID() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "d-" + hex.EncodeToString(b)
}

func (c *Core) planner(ctx context.Context) *planner {
	d := c.Refresh(ctx)
	return &planner{c: c, windows: d.Windows, spaces: d.Workspaces, outputs: d.Outputs, settings: c.Themes.Settings()}
}

// plan validates calls and returns the proposal (not stored).
func (c *Core) plan(ctx context.Context, calls []Call, m Meta) (*Proposal, error) {
	if len(calls) == 0 {
		return nil, errors.New("no actions")
	}
	if len(calls) > 16 {
		return nil, errors.New("too many actions in one request (at most 16)")
	}
	p := c.planner(ctx)
	p.meta = m
	pr := &Proposal{Calls: calls, base: p.settings.Clone()}
	for _, call := range calls {
		def, ok := ActionByName(call.Action)
		if !ok {
			return nil, fmt.Errorf("unknown action %q", call.Action)
		}
		if call.Args == nil {
			call.Args = map[string]any{}
		}
		for _, prm := range def.Params {
			if _, ok := call.Args[prm.Name]; prm.Required && !ok {
				return nil, fmt.Errorf("%s: missing %s", def.Name, prm.Name)
			}
		}
		for k := range call.Args {
			known := false
			for _, prm := range def.Params {
				if prm.Name == k {
					known = true
				}
			}
			if !known {
				return nil, fmt.Errorf("%s: unknown parameter %q", def.Name, k)
			}
		}
		if def.Person && m.Origin != "commandbar" && m.Origin != "voice" && !(def.UI && m.Origin == "ui") {
			return nil, fmt.Errorf("%s is planned only from the person's own request", def.Name)
		}
		if len(def.Editable) > 0 {
			if len(calls) != 1 {
				return nil, fmt.Errorf("%s must be alone in its proposal", def.Name)
			}
			pr.Editable = def.Editable
		}
		st, err := def.plan(ctx, p, call.Args)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", def.Name, err)
		}
		pr.Steps = append(pr.Steps, st.Summary)
		if st.Preview != nil {
			pr.Previews = append(pr.Previews, st.Preview)
		}
		if st.run != nil {
			pr.runs = append(pr.runs, st.run)
		}
		if st.end != nil {
			pr.ends = append(pr.ends, st.end)
		}
	}
	if p.touched {
		next := p.settings.Clone()
		if err := c.Themes.Check(&next); err != nil {
			return nil, err
		}
		from, err := c.Themes.Resolve(pr.base, c.HW.Weak)
		if err != nil {
			return nil, err
		}
		to, err := c.Themes.Resolve(next, c.HW.Weak)
		if err != nil {
			return nil, err
		}
		pr.Diff = theme.Diff(from, to)
		pr.next = &next
	}
	return pr, nil
}

// Meta describes where a proposal comes from.
type Meta struct {
	Origin, Actor, Request, Explain, Backend string
	// PID and Domain identify the requesting process (SO_PEERCRED and its
	// SELinux domain); 0 and "" for the command bar and the UI.
	PID    int
	Domain string
	// Client is the name the client gave itself in hello (a label only).
	Client string
}

// Propose stores a proposal for the person to confirm in the shell UI.
func (c *Core) Propose(ctx context.Context, m Meta, calls []Call) (*Proposal, error) {
	origin, actor, request := m.Origin, m.Actor, m.Request
	pr, err := c.plan(ctx, calls, m)
	if err != nil {
		_, _ = c.Audit.Append("refuse", actor, "invalid request: "+err.Error(), map[string]any{"calls": calls, "request": request, "domain": m.Domain})
		return nil, err
	}
	pr.ID, pr.Origin, pr.Actor, pr.Request = newID(), origin, actor, request
	pr.From, pr.FromKind = c.requester(m)
	pr.Explain, pr.Backend, pr.meta = m.Explain, m.Backend, m
	pr.Created = time.Now().UTC()
	pr.Expires = pr.Created.Add(c.ProposalTTL)
	pr.Status = StatusPending
	pr.done = make(chan struct{})
	c.mu.Lock()
	c.proposals[pr.ID] = pr
	c.order = append(c.order, pr.ID)
	if len(c.order) > 100 {
		old := c.order[0]
		c.order = c.order[1:]
		delete(c.proposals, old)
	}
	pr.GateMode = c.gateMode(gatePath(calls))
	c.mu.Unlock()
	_, _ = c.Audit.Append("request", actor, pr.summary(), map[string]any{"proposal": pr.ID, "origin": origin, "calls": calls, "diff": pr.Diff, "request": request, "pid": m.PID, "domain": m.Domain, "previews": pr.Previews, "gate_mode": pr.GateMode})
	var gateErr error
	if pr.GateMode == gateEnforce {
		// The gate decides: a rule may allow or refuse it at once; else the
		// person decides on the sheet, which answers the gate directly.
		gateErr = c.gateSubmit(pr)
	}
	c.Broadcast("proposal", pr.public())
	switch {
	case gateErr != nil:
		c.finish(pr, StatusRefused, "gate", gateErr.Error())
		return pr, nil
	case pr.GateMode == gateEnforce:
		c.gateAct(pr)
	}
	go func() {
		t := time.NewTimer(time.Until(pr.Expires))
		defer t.Stop()
		select {
		case <-pr.done:
		case <-t.C:
			c.finish(pr, StatusExpired, "", "nobody confirmed it in time")
		}
	}()
	return pr, nil
}

func (pr *Proposal) summary() string {
	if len(pr.Steps) == 1 {
		return pr.Steps[0]
	}
	return fmt.Sprintf("%s (+%d more)", pr.Steps[0], len(pr.Steps)-1)
}

// public returns a copy safe to send.
func (pr *Proposal) public() Proposal {
	cp := *pr
	cp.runs, cp.next, cp.done, cp.ends = nil, nil, nil, nil
	return cp
}

func (c *Core) finish(pr *Proposal, status, by, msg string) bool {
	c.mu.Lock()
	if pr.Status != StatusPending {
		c.mu.Unlock()
		return false
	}
	pr.Status, pr.Decided = status, by
	if msg != "" {
		pr.Error = msg
	}
	close(pr.done)
	ends := pr.ends
	cancelID := ""
	if pr.GateMode == gateEnforce && pr.Gate != nil && pr.Gate.Decision == "asked" {
		// Ended here (expired, replaced): withdraw it from the gate's queue.
		cancelID = pr.Gate.ID
	}
	c.mu.Unlock()
	c.gateCancel(cancelID)
	if pr.GateMode == gateObserve {
		c.gateObserve(pr, status, by)
	}
	if status != StatusApplied {
		for _, e := range ends {
			e(status)
		}
	}
	typ := map[string]string{StatusDeclined: "decline", StatusExpired: "expire", StatusStale: "refuse", StatusFailed: "fail", StatusApplied: "apply"}[status]
	_, _ = c.Audit.Append(typ, by, pr.summary(), map[string]any{"proposal": pr.ID, "origin": pr.Origin, "actor": pr.Actor, "error": msg})
	c.Broadcast("proposal", pr.public())
	return true
}

// Get returns a proposal.
func (c *Core) Get(id string) (Proposal, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	pr, ok := c.proposals[id]
	if !ok {
		return Proposal{}, false
	}
	return pr.public(), true
}

// Pending lists proposals waiting for a decision.
func (c *Core) Pending() []Proposal {
	c.mu.Lock()
	defer c.mu.Unlock()
	var out []Proposal
	for _, id := range c.order {
		if pr := c.proposals[id]; pr.Status == StatusPending {
			out = append(out, pr.public())
		}
	}
	return out
}

// Decide confirms or declines a proposal; only the shell UI may call it.
func (c *Core) Decide(ctx context.Context, id string, approve bool, by string) (Proposal, error) {
	return c.DecideEdited(ctx, id, approve, by, nil)
}

// DecideEdited is Decide with the person's edits of the editable
// parameters (the text of an e-mail draft): the action is planned again
// with them, the new preview is recorded, and that is what runs.
func (c *Core) DecideEdited(ctx context.Context, id string, approve bool, by string, edits map[string]any) (Proposal, error) {
	c.mu.Lock()
	pr, ok := c.proposals[id]
	c.mu.Unlock()
	if !ok {
		return Proposal{}, fmt.Errorf("no proposal %s", id)
	}
	if pr.GateMode == gateEnforce {
		return pr.public(), errGateDecides
	}
	if approve && len(edits) > 0 {
		if err := c.applyEdits(ctx, pr, edits, by); err != nil {
			return pr.public(), err
		}
	}
	if pr.GateMode == gateEnforce {
		// The shell UI decides at the gate (the only desktop decider);
		// this daemon only runs what the gate allowed.
		return pr.public(), errGateDecides
	}
	if !approve {
		if !c.finish(pr, StatusDeclined, by, "") {
			return pr.public(), fmt.Errorf("proposal %s is %s", id, pr.Status)
		}
		return pr.public(), nil
	}
	c.mu.Lock()
	if pr.Status != StatusPending {
		st := pr.Status
		c.mu.Unlock()
		return pr.public(), fmt.Errorf("proposal %s is %s", id, st)
	}
	c.mu.Unlock()
	// Synthetic input just ran: this confirmation may have been typed or
	// clicked by an agent, not by the person. Refuse it (the proposal
	// stays pending, the person can confirm again).
	if c.recentInput() {
		_, _ = c.Audit.Append("refuse", by, "confirmation right after synthetic input ignored", map[string]any{"proposal": pr.ID})
		return pr.public(), errors.New("a confirmation right after agent input is not accepted; confirm again")
	}
	return c.execute(ctx, pr, by)
}

// execute runs a confirmed proposal (confirmed here, or allowed by the
// approval gate and claimed).
func (c *Core) execute(ctx context.Context, pr *Proposal, by string) (Proposal, error) {
	// A theme change was computed against the settings of that moment:
	// if they changed since, the diff the person saw is not what would
	// happen, so refuse instead of applying something else.
	if pr.next != nil && !reflect.DeepEqual(normalized(c.Themes.Settings()), normalized(pr.base)) {
		c.finish(pr, StatusStale, by, "the theme changed since this was proposed; ask again")
		return pr.public(), errors.New(pr.Error)
	}
	res, err := c.run(ctx, pr.runs, pr.next)
	pr.Result = res
	if err != nil {
		c.finish(pr, StatusFailed, by, err.Error())
		return pr.public(), err
	}
	c.finish(pr, StatusApplied, by, "")
	if len(pr.Previews) > 0 {
		// What was done, as it was shown: the activity timeline and the
		// ledger keep the exact preview of every acting step.
		_, _ = c.Audit.Append("done", by, pr.summary(), map[string]any{"proposal": pr.ID, "previews": pr.Previews, "result": pr.Result})
	}
	return pr.public(), nil
}

// EditProposal applies the person's edits of the editable parameters
// (the shell UI only) before the decision. Where the gate decides, the
// edited plan is a new request (the old one is withdrawn): the person
// approves exactly what will be sent.
func (c *Core) EditProposal(ctx context.Context, id string, edits map[string]any, by string) (Proposal, error) {
	c.mu.Lock()
	pr, ok := c.proposals[id]
	c.mu.Unlock()
	if !ok {
		return Proposal{}, fmt.Errorf("no proposal %s", id)
	}
	c.mu.Lock()
	old := ""
	if pr.Gate != nil {
		old = pr.Gate.ID
	}
	c.mu.Unlock()
	before, _ := json.Marshal(pr.Calls)
	if err := c.applyEdits(ctx, pr, edits, by); err != nil {
		return pr.public(), err
	}
	after, _ := json.Marshal(pr.Calls)
	if pr.GateMode == gateEnforce && (string(before) != string(after) || old == "") {
		c.gateCancel(old)
		if err := c.gateSubmit(pr); err != nil {
			c.finish(pr, StatusRefused, "gate", err.Error())
			return pr.public(), err
		}
		c.Broadcast("proposal", pr.public())
		c.gateAct(pr)
	}
	return pr.public(), nil
}

// applyEdits plans the proposal's one call again with the edited values
// of its editable parameters and replaces the planned step.
func (c *Core) applyEdits(ctx context.Context, pr *Proposal, edits map[string]any, by string) error {
	c.mu.Lock()
	if pr.Status != StatusPending {
		c.mu.Unlock()
		return fmt.Errorf("proposal %s is %s", pr.ID, pr.Status)
	}
	if len(pr.Calls) != 1 || len(pr.Editable) == 0 {
		c.mu.Unlock()
		return errors.New("this proposal cannot be edited")
	}
	call := Call{Action: pr.Calls[0].Action, Args: map[string]any{}}
	for k, v := range pr.Calls[0].Args {
		call.Args[k] = v
	}
	var changed []string
	for k, v := range edits {
		ok := false
		for _, e := range pr.Editable {
			if e == k {
				ok = true
			}
		}
		if !ok {
			c.mu.Unlock()
			return fmt.Errorf("%s cannot be edited", k)
		}
		if fmt.Sprint(call.Args[k]) != fmt.Sprint(v) {
			changed = append(changed, k)
		}
		call.Args[k] = v
	}
	m := pr.meta
	c.mu.Unlock()
	if len(changed) == 0 {
		return nil
	}
	np, err := c.plan(ctx, []Call{call}, m)
	if err != nil {
		_, _ = c.Audit.Append("refuse", by, "edit refused: "+err.Error(), map[string]any{"proposal": pr.ID})
		return err
	}
	c.mu.Lock()
	// The preview of the original plan is replaced: its end hooks are
	// the new plan's too (they undo what is on screen).
	pr.Calls, pr.Steps, pr.Previews, pr.runs, pr.ends, pr.Edited = []Call{call}, np.Steps, np.Previews, np.runs, np.ends, true
	c.mu.Unlock()
	_, _ = c.Audit.Append("edit", by, pr.summary(), map[string]any{"proposal": pr.ID, "changed": changed, "previews": pr.Previews})
	return nil
}

func normalized(s theme.Settings) theme.Settings {
	for _, m := range []*map[string]any{&s.Overrides, &s.Light, &s.Dark} {
		if len(*m) == 0 {
			*m = nil
		}
	}
	return s
}

// run performs planned steps, then commits theme settings.
func (c *Core) run(ctx context.Context, runs []func(context.Context) (any, error), next *theme.Settings) ([]any, error) {
	var out []any
	for _, r := range runs {
		v, err := r(ctx)
		if err != nil {
			return out, err
		}
		out = append(out, v)
	}
	if len(runs) > 0 {
		// The compositor's events can arrive before the window committed
		// its new size, so the last refresh may still show the old
		// geometry (and no maximized or snapped state). Look again a
		// little later, so the UI's toggles (Super+Up then Super+Down)
		// see the state the action set.
		go func() {
			for _, d := range []time.Duration{300 * time.Millisecond, time.Second} {
				time.Sleep(d)
				rctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
				c.Refresh(rctx)
				cancel()
			}
		}()
	}
	if next != nil {
		if err := c.Themes.Commit(*next); err != nil {
			return out, err
		}
		c.ApplyTheme(ctx)
	}
	return out, nil
}

// Execute runs actions the person started from the shell UI itself (a
// click in the launcher, a slider in settings): no proposal, audited.
func (c *Core) Execute(ctx context.Context, actor string, calls []Call) (*Proposal, error) {
	for _, call := range calls {
		if def, ok := ActionByName(call.Action); ok && def.Agent {
			return nil, fmt.Errorf("%s is requested by agents, not run from the UI", call.Action)
		}
	}
	pr, err := c.plan(ctx, calls, Meta{Origin: "ui", Actor: actor})
	if err != nil {
		return nil, err
	}
	res, err := c.run(ctx, pr.runs, pr.next)
	pr.Result = res
	data := map[string]any{"calls": calls, "diff": pr.Diff}
	if err != nil {
		data["error"] = err.Error()
		_, _ = c.Audit.Append("fail", actor, pr.summary(), data)
		return pr, err
	}
	// Theme slider moves would flood the log; keep them but compact.
	_, _ = c.Audit.Append("apply", actor, pr.summary(), data)
	return pr, nil
}

// Wait blocks until a proposal is decided or ctx ends.
func (c *Core) Wait(ctx context.Context, id string) (Proposal, error) {
	c.mu.Lock()
	pr, ok := c.proposals[id]
	c.mu.Unlock()
	if !ok {
		return Proposal{}, fmt.Errorf("no proposal %s", id)
	}
	select {
	case <-pr.done:
		return pr.public(), nil
	case <-ctx.Done():
		return pr.public(), ctx.Err()
	}
}

// Option is one entry of a choice the person makes in the shell.
type Option struct {
	ID    string `json:"id"`
	Label string `json:"label"`
	Hint  string `json:"hint,omitempty"`
}

// Choose asks the person to pick one option in the shell UI (for example
// which screen to share) and returns its id, or "" when cancelled or timed
// out. The question and the answer are audited.
func (c *Core) Choose(ctx context.Context, actor, title, body string, opts []Option) (string, error) {
	if len(opts) == 0 {
		return "", errors.New("no options")
	}
	id := newID()
	ch := make(chan string, 1)
	c.mu.Lock()
	c.choices[id] = ch
	c.mu.Unlock()
	defer func() {
		c.mu.Lock()
		delete(c.choices, id)
		c.mu.Unlock()
		c.Broadcast("choice-done", map[string]any{"id": id})
	}()
	c.Broadcast("choose", map[string]any{"id": id, "title": title, "body": body, "options": opts, "actor": actor})
	select {
	case v := <-ch:
		typ := "confirm"
		if v == "" {
			typ = "decline"
		}
		_, _ = c.Audit.Append(typ, "ui", title+": "+orNone(v), map[string]any{"choice": id, "asked_by": actor, "options": opts})
		return v, nil
	case <-ctx.Done():
		_, _ = c.Audit.Append("expire", actor, title+": no answer", map[string]any{"choice": id})
		return "", ctx.Err()
	}
}

func orNone(s string) string {
	if s == "" {
		return "cancelled"
	}
	return s
}

// Chosen delivers the person's answer (UI only).
func (c *Core) Chosen(id, value string) error {
	c.mu.Lock()
	ch, ok := c.choices[id]
	c.mu.Unlock()
	if !ok {
		return fmt.Errorf("no open choice %s", id)
	}
	select {
	case ch <- value:
	default:
	}
	return nil
}
