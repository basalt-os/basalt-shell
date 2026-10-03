// Package shell is the core of basalt-shell's daemon: desktop state from
// the compositor adapter, the theme store, the closed set of typed
// actions, proposals that wait for the person's confirmation, the audit
// log, and the event fan-out to the shell's UI.
package shell

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"reflect"
	"sync"
	"time"

	"github.com/openbasalt/basalt-shell/internal/appearance"
	"github.com/openbasalt/basalt-shell/internal/apps"
	"github.com/openbasalt/basalt-shell/internal/assistant"
	"github.com/openbasalt/basalt-shell/internal/audit"
	"github.com/openbasalt/basalt-shell/internal/compositor"
	"github.com/openbasalt/basalt-shell/internal/hw"
	"github.com/openbasalt/basalt-shell/internal/intent"
	"github.com/openbasalt/basalt-shell/internal/theme"
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
)

// Proposal is a set of actions waiting for the person's decision.
type Proposal struct {
	ID        string         `json:"id"`
	Origin    string         `json:"origin"` // mcp, ipc, commandbar
	Actor     string         `json:"actor"`
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

	base theme.Settings
	next *theme.Settings
	runs []func(context.Context) (any, error)
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

	mu        sync.Mutex
	proposals map[string]*Proposal
	order     []string
	subs      map[chan Event]struct{}
	appsCache []apps.App
	appsTime  time.Time
	desktop   Desktop
	lastApps  appearance.Result
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

// New builds a core.
func New(comp compositor.Adapter, store *theme.Store, log *audit.Log, rep hw.Report, configDir string) *Core {
	c := &Core{Comp: comp, Themes: store, Audit: log, HW: rep, ConfigDir: configDir,
		ApplyApps: true, ProposalTTL: 5 * time.Minute,
		proposals: map[string]*Proposal{}, subs: map[chan Event]struct{}{}}
	log.OnAppend = func(r audit.Record) { c.Broadcast("activity", r) }
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
	changed := !reflect.DeepEqual(c.desktop, d)
	c.desktop = d
	c.mu.Unlock()
	if changed {
		c.Broadcast("desktop", d)
	}
	return d
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
			case _, ok := <-ch:
				if !ok {
					break loop
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
	shadowAlpha := t.Num("elevation.shadow")
	style := compositor.Style{
		CornerRadius:  int(t.Num("radius.window")),
		BorderWidth:   int(t.Num("window.border")),
		Gaps:          int(t.Num("window.gaps")),
		FocusColor:    t.Str("color.accent"),
		InactiveColor: t.Str("color.border"),
		UrgentColor:   t.Str("color.danger"),
		Shadows:       t.Bool("window.shadows"),
		ShadowColor:   theme.WithAlpha("#000000", shadowAlpha),
		ShadowBlur:    int(t.Num("elevation.blur")),
		Blur:          t.Bool("window.blur"),
		DimInactive:   t.Num("window.dimInactive"),
		Animations:    t.Str("motion") == "full",
		CursorTheme:   t.Str("apps.cursorTheme"),
		CursorSize:    int(t.Num("apps.cursorSize")),
	}
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
			r := appearance.Apply(ctx, t, lt, dt, c.ConfigDir)
			c.mu.Lock()
			c.lastApps = r
			c.mu.Unlock()
			for _, e := range r.Errors {
				log.Printf("appearance: %s", e)
			}
		}()
	}
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
func (c *Core) plan(ctx context.Context, calls []Call) (*Proposal, error) {
	if len(calls) == 0 {
		return nil, errors.New("no actions")
	}
	if len(calls) > 16 {
		return nil, errors.New("too many actions in one request (at most 16)")
	}
	p := c.planner(ctx)
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
		st, err := def.plan(ctx, p, call.Args)
		if err != nil {
			return nil, fmt.Errorf("%s: %w", def.Name, err)
		}
		pr.Steps = append(pr.Steps, st.Summary)
		if st.run != nil {
			pr.runs = append(pr.runs, st.run)
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
}

// Propose stores a proposal for the person to confirm in the shell UI.
func (c *Core) Propose(ctx context.Context, m Meta, calls []Call) (*Proposal, error) {
	origin, actor, request := m.Origin, m.Actor, m.Request
	pr, err := c.plan(ctx, calls)
	if err != nil {
		_, _ = c.Audit.Append("refuse", actor, "invalid request: "+err.Error(), map[string]any{"calls": calls, "request": request})
		return nil, err
	}
	pr.ID, pr.Origin, pr.Actor, pr.Request = newID(), origin, actor, request
	pr.Explain, pr.Backend = m.Explain, m.Backend
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
	c.mu.Unlock()
	_, _ = c.Audit.Append("request", actor, pr.summary(), map[string]any{"proposal": pr.ID, "origin": origin, "calls": calls, "diff": pr.Diff, "request": request})
	c.Broadcast("proposal", pr.public())
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
	cp.runs, cp.next, cp.done = nil, nil, nil
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
	c.mu.Unlock()
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
	c.mu.Lock()
	pr, ok := c.proposals[id]
	c.mu.Unlock()
	if !ok {
		return Proposal{}, fmt.Errorf("no proposal %s", id)
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
	return pr.public(), nil
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
	pr, err := c.plan(ctx, calls)
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
