package shell

import (
	"context"
	"errors"
	"fmt"
	"log"
	"math"
	"strings"
	"time"

	"github.com/basalt-os/basalt-shell/internal/agentio"
	"github.com/basalt-os/basalt-shell/internal/compositor"
	"github.com/basalt-os/basalt-shell/internal/wlvirt"
)

// The agents' last-resort access to the desktop ("computer use"): screen
// capture and synthetic keyboard and pointer input. Rules:
//
//   - A screenshot is shown to an agent only after the person confirmed
//     it (a proposal on the confirmation sheet), or inside a control
//     session the person granted with screen access.
//   - Synthetic input exists only inside a control session the person
//     granted (agent.control, confirmed on the sheet), bound to the
//     process that asked for it, limited in time (at most 15 minutes) and
//     stoppable at any moment by the person (panel indicator, Super+Shift+
//     Escape) or by the agent.
//   - While a session runs the shell shows that an agent is controlling
//     the desktop (panel indicator and a frame around the screens).
//   - Input is refused while the person has a dialog open (a confirmation
//     sheet, a choice, an authentication dialog), and a confirmation that
//     arrives less than InputQuiet after synthetic input is refused, so an
//     agent cannot click or type "Confirm" for itself.
//   - Every capture and input is written to the activity log (the text an
//     agent types too, so the person can see what was typed).

// InputQuiet is how long after synthetic input a confirmation is refused.
const InputQuiet = 1500 * time.Millisecond

// MaxControlMinutes bounds a control session.
const MaxControlMinutes = 15

// Control is a control session granted to one agent process.
type Control struct {
	ID       string    `json:"id"`
	Actor    string    `json:"actor"`
	Domain   string    `json:"domain,omitempty"`
	PID      int       `json:"pid"`
	Reason   string    `json:"reason"`
	Input    bool      `json:"input"`
	Screen   bool      `json:"screen"`
	Started  time.Time `json:"started"`
	Expires  time.Time `json:"expires"`
	Inputs   int       `json:"inputs"`
	Captures int       `json:"captures"`

	stop chan struct{}
}

// ControlState returns the running control session, or nil.
func (c *Core) ControlState() *Control {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.control == nil {
		return nil
	}
	cp := *c.control
	cp.stop = nil
	return &cp
}

func (c *Core) broadcastControl() {
	st := c.ControlState()
	if st == nil {
		c.Broadcast("control", nil)
		return
	}
	c.Broadcast("control", st)
}

func (c *Core) startControl(m Meta, minutes int, input, screen bool, reason string) (*Control, error) {
	if m.PID <= 0 {
		return nil, errors.New("a control session needs an agent connection")
	}
	c.StopControl("ui", "replaced by a new control session")
	now := time.Now().UTC()
	ct := &Control{ID: newID(), Actor: m.Actor, Domain: m.Domain, PID: m.PID, Reason: reason, Input: input, Screen: screen,
		Started: now, Expires: now.Add(time.Duration(minutes) * time.Minute), stop: make(chan struct{})}
	c.mu.Lock()
	c.control = ct
	c.mu.Unlock()
	if input && c.VirtualInput {
		if _, err := c.virtual(); err != nil {
			log.Printf("virtual input: %v (falling back to the compositor)", err)
		}
	}
	_, _ = c.Audit.Append("control", m.Actor, fmt.Sprintf("control session started (%d min, input %v, screen %v)", minutes, input, screen),
		map[string]any{"control": ct.ID, "pid": m.PID, "domain": m.Domain, "reason": reason, "expires": ct.Expires})
	c.broadcastControl()
	go func() {
		t := time.NewTimer(time.Until(ct.Expires))
		defer t.Stop()
		select {
		case <-ct.stop:
		case <-t.C:
			c.endControl(ct, "expire", m.Actor, "control session expired")
		}
	}()
	cp := *ct
	cp.stop = nil
	return &cp, nil
}

func (c *Core) endControl(ct *Control, typ, by, why string) bool {
	c.mu.Lock()
	if c.control != ct {
		c.mu.Unlock()
		return false
	}
	c.control = nil
	close(ct.stop)
	c.mu.Unlock()
	if !c.VirtualInputAlways {
		c.closeVirtual()
	}
	_, _ = c.Audit.Append(typ, by, why, map[string]any{"control": ct.ID, "actor": ct.Actor, "inputs": ct.Inputs, "captures": ct.Captures})
	c.broadcastControl()
	return true
}

// StopControl ends the running control session (anyone may stop it).
func (c *Core) StopControl(by, why string) bool {
	c.mu.Lock()
	ct := c.control
	c.mu.Unlock()
	if ct == nil {
		return false
	}
	if why == "" {
		why = "control session stopped"
	}
	return c.endControl(ct, "control", by, why)
}

// virtual returns the daemon's virtual keyboard and pointer, opening them
// when needed.
func (c *Core) virtual() (*wlvirt.Device, error) {
	c.mu.Lock()
	d := c.virt
	c.mu.Unlock()
	if d != nil && d.Err() == nil {
		return d, nil
	}
	if d != nil {
		d.Close()
	}
	path, err := wlvirt.Socket()
	if err != nil {
		return nil, err
	}
	d, err = wlvirt.Open(path)
	if err != nil {
		return nil, err
	}
	c.mu.Lock()
	c.virt = d
	c.mu.Unlock()
	return d, nil
}

func (c *Core) closeVirtual() {
	c.mu.Lock()
	d := c.virt
	c.virt = nil
	c.mu.Unlock()
	if d != nil {
		d.Close()
	}
}

// OpenVirtualInput opens the virtual devices for the whole session.
func (c *Core) OpenVirtualInput() error {
	_, err := c.virtual()
	return err
}

// layoutSize is the bounding box of the outputs (absolute pointer moves
// are given in it).
func layoutSize(outs []compositor.Output) (int, int) {
	w, h := 0, 0
	for _, o := range outs {
		if o.Rect.X+o.Rect.W > w {
			w = o.Rect.X + o.Rect.W
		}
		if o.Rect.Y+o.Rect.H > h {
			h = o.Rect.Y + o.Rect.H
		}
	}
	return w, h
}

// virtualPointer adapts the wlvirt device to compositor.Pointer.
type virtualPointer struct {
	c *Core
	d *wlvirt.Device
}

func (v virtualPointer) PointerMove(ctx context.Context, x, y int) error {
	outs, _ := v.c.Comp.Outputs(ctx)
	w, h := layoutSize(outs)
	return v.d.MoveAbsolute(x, y, w, h)
}

func (v virtualPointer) PointerButton(_ context.Context, button, action string) error {
	switch action {
	case "press":
		return v.d.Button(button, true)
	case "release":
		return v.d.Button(button, false)
	}
	if err := v.d.Button(button, true); err != nil {
		return err
	}
	time.Sleep(20 * time.Millisecond)
	return v.d.Button(button, false)
}

func (v virtualPointer) PointerScroll(_ context.Context, dx, dy int) error { return v.d.Scroll(dx, dy) }

// pointer picks the daemon's virtual pointer, else the compositor's own
// pointer commands (sway's seat cursor).
func (c *Core) pointer() (compositor.Pointer, bool) {
	if c.VirtualInput {
		if d, err := c.virtual(); err == nil {
			return virtualPointer{c, d}, true
		}
	}
	p, ok := c.Comp.(compositor.Pointer)
	return p, ok && c.Comp.Caps().Pointer
}

// SetUIModal records whether the shell UI shows a modal dialog
// (authentication, confirmation, choice).
func (c *Core) SetUIModal(on bool) {
	c.mu.Lock()
	c.uiModal = on
	c.mu.Unlock()
}

// SetUILocked records whether the shell UI's lock screen holds the
// session. It can only take power away: while it is set, push to talk,
// agent input and agent screenshots are refused; it never unlocks
// anything (only PAM does, in the UI).
func (c *Core) SetUILocked(on bool) {
	c.mu.Lock()
	c.uiLocked = on
	c.mu.Unlock()
}

// Locked reports a locked screen: the shell's lock screen (as the UI
// reports it) or a locker program (swaylock, the fallback).
func (c *Core) Locked() bool {
	c.mu.Lock()
	ui := c.uiLocked
	c.mu.Unlock()
	return ui || (c.ScreenLocked != nil && c.ScreenLocked())
}

// modalOpen reports whether the person has something to answer, or the
// screen is locked (nobody can see what an agent does then).
func (c *Core) modalOpen() string {
	if c.Locked() {
		return "the screen is locked"
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.uiModal {
		return "a dialog is open"
	}
	if len(c.choices) > 0 {
		return "a choice is waiting for the person"
	}
	for _, id := range c.order {
		if c.proposals[id].Status == StatusPending {
			return "a request is waiting for the person's confirmation"
		}
	}
	return ""
}

// recentInput reports whether synthetic input ran less than InputQuiet ago.
func (c *Core) recentInput() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return !c.lastInput.IsZero() && time.Since(c.lastInput) < InputQuiet
}

// controlFor returns the session of the agent process pid.
func (c *Core) controlFor(pid int) *Control {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.control != nil && c.control.PID == pid && time.Now().Before(c.control.Expires) {
		return c.control
	}
	return nil
}

// planCapture builds the capture of an output or a window.
func (p *planner) planCapture(a map[string]any) (agentio.CaptureSpec, string, error) {
	target := argStr(a, "target")
	if target == "" {
		target = "output"
		if argStr(a, "window") != "" {
			target = "window"
		}
	}
	maxw, ok := argInt(a, "max_width")
	if !ok || maxw <= 0 {
		maxw = 1600
	}
	if maxw < 320 || maxw > 7680 {
		return agentio.CaptureSpec{}, "", errors.New("max_width must be 320 to 7680")
	}
	scale := func(w int) float64 {
		if w <= maxw || w == 0 {
			return 0
		}
		return math.Floor(float64(maxw)/float64(w)*1000) / 1000
	}
	switch target {
	case "output":
		var out compositor.Output
		name := argStr(a, "output")
		for _, o := range p.outputs {
			if (name == "" && o.Focused) || o.Name == name {
				out = o
			}
		}
		if out.Name == "" && name == "" && len(p.outputs) > 0 {
			out = p.outputs[0]
		}
		if out.Name == "" {
			return agentio.CaptureSpec{}, "", fmt.Errorf("no output %q", name)
		}
		s := agentio.CaptureSpec{Output: out.Name, Scale: scale(out.Rect.W), Label: "screen " + out.Name}
		return s, fmt.Sprintf("Show %s a screenshot of the whole screen %s (%dx%d)", p.meta.Actor, out.Name, out.Rect.W, out.Rect.H), nil
	case "window":
		w, err := p.window(a["window"])
		if err != nil {
			return agentio.CaptureSpec{}, "", err
		}
		s := agentio.CaptureSpec{Scale: scale(w.Rect.W), Label: "window " + label(w),
			Region: fmt.Sprintf("%d,%d %dx%d", w.Rect.X, w.Rect.Y, w.Rect.W, w.Rect.H)}
		how := "its area of the screen, including anything on top of it"
		if w.ForeignID != "" {
			// Only the window when the compositor can capture one toplevel.
			s.Toplevel = w.ForeignID
			how = "the window, or its area of the screen if the compositor cannot capture a single window"
		}
		return s, fmt.Sprintf("Show %s a screenshot of %s (%s)", p.meta.Actor, label(w), how), nil
	}
	return agentio.CaptureSpec{}, "", fmt.Errorf("target must be output or window")
}

// screenshot runs a capture (the Screenshot hook in tests).
func (c *Core) screenshot(ctx context.Context, s agentio.CaptureSpec) (*agentio.Capture, error) {
	if c.Screenshot != nil {
		return c.Screenshot(ctx, s)
	}
	return c.Tools.Screenshot(ctx, s)
}

// CaptureResult is what an agent receives for a capture request.
type CaptureResult struct {
	Proposal *Proposal        `json:"proposal,omitempty"`
	Capture  *agentio.Capture `json:"capture,omitempty"`
	PNG      []byte           `json:"png,omitempty"` // base64 in JSON
	Status   string           `json:"status"`
	Message  string           `json:"message,omitempty"`
}

// Capture handles a screenshot request from an agent: directly inside a
// control session with screen access, else as a proposal the person
// confirms (waiting up to wait).
func (c *Core) Capture(ctx context.Context, m Meta, args map[string]any, wait time.Duration) (CaptureResult, error) {
	if c.Locked() {
		_, _ = c.Audit.Append("refuse", m.Actor, "screenshot refused: the screen is locked", map[string]any{"pid": m.PID})
		return CaptureResult{}, errors.New("screenshot refused: the screen is locked; try again after the person unlocks it")
	}
	if ct := c.controlFor(m.PID); ct != nil && ct.Screen {
		p := c.planner(ctx)
		p.meta = m
		spec, summary, err := p.planCapture(args)
		if err != nil {
			return CaptureResult{}, err
		}
		cp, err := c.screenshot(ctx, spec)
		if err != nil {
			_, _ = c.Audit.Append("fail", m.Actor, summary, map[string]any{"error": err.Error(), "control": ct.ID})
			return CaptureResult{}, err
		}
		c.mu.Lock()
		if c.control == ct {
			ct.Captures++
		}
		c.mu.Unlock()
		_, _ = c.Audit.Append("capture", m.Actor, summary+" (control session)", map[string]any{"control": ct.ID, "width": cp.Width, "height": cp.Height, "method": cp.Method})
		c.Broadcast("agent-activity", map[string]any{"kind": "capture", "actor": m.Actor, "target": cp.Target})
		return CaptureResult{Capture: cp, PNG: cp.PNG, Status: StatusApplied}, nil
	}
	pr, err := c.Propose(ctx, m, []Call{{Action: "screen.capture", Args: args}})
	if err != nil {
		return CaptureResult{}, err
	}
	wctx, cancel := context.WithTimeout(ctx, wait)
	defer cancel()
	p, err := c.Wait(wctx, pr.ID)
	if err != nil && !errors.Is(err, context.DeadlineExceeded) {
		return CaptureResult{}, err
	}
	res := CaptureResult{Proposal: &p, Status: p.Status}
	if p.Status == StatusApplied {
		c.mu.Lock()
		if full, ok := c.proposals[p.ID]; ok {
			for _, r := range full.Result {
				if cp, ok := r.(*agentio.Capture); ok {
					res.Capture, res.PNG = cp, cp.PNG
				}
			}
			// The image goes to the agent once; it is not kept.
			full.Result = nil
		}
		c.mu.Unlock()
		if res.Capture != nil {
			c.Broadcast("agent-activity", map[string]any{"kind": "capture", "actor": m.Actor, "target": res.Capture.Target})
		}
	}
	return res, nil
}

// InputRequest is one synthetic input step.
type InputRequest struct {
	Kind   string `json:"kind"` // type, key, move, click, scroll
	Text   string `json:"text,omitempty"`
	Keys   string `json:"keys,omitempty"`
	X      *int   `json:"x,omitempty"`
	Y      *int   `json:"y,omitempty"`
	Button string `json:"button,omitempty"`
	Action string `json:"action,omitempty"` // click (default), press, release, double
	DX     int    `json:"dx,omitempty"`
	DY     int    `json:"dy,omitempty"`
}

// Input runs one synthetic input step for the agent process that holds
// the control session.
func (c *Core) Input(ctx context.Context, m Meta, in InputRequest) (map[string]any, error) {
	ct := c.controlFor(m.PID)
	if ct == nil {
		err := errors.New("no control session for this agent: ask with agent_control_request first; the person must confirm it")
		_, _ = c.Audit.Append("refuse", m.Actor, "input without a control session", map[string]any{"kind": in.Kind, "pid": m.PID})
		return nil, err
	}
	if !ct.Input {
		return nil, errors.New("the control session does not include input")
	}
	if why := c.modalOpen(); why != "" {
		_, _ = c.Audit.Append("refuse", m.Actor, "input refused: "+why, map[string]any{"kind": in.Kind, "control": ct.ID})
		return nil, fmt.Errorf("input refused: %s; wait until the person answers it", why)
	}
	var summary string
	var run func(ctx context.Context) error
	ptr, hasPtr := c.pointer()
	needPtr := func() error {
		if !hasPtr {
			return fmt.Errorf("%s does not support pointer input", c.Comp.Name())
		}
		return nil
	}
	switch in.Kind {
	case "type":
		summary = fmt.Sprintf("Type %q", truncate(in.Text, 200))
		run = func(ctx context.Context) error { return c.Tools.TypeText(ctx, in.Text) }
		if c.TypeText != nil {
			run = func(ctx context.Context) error { return c.TypeText(ctx, in.Text) }
		}
	case "key":
		if _, _, err := agentio.ParseCombo(in.Keys); err != nil {
			return nil, err
		}
		summary = "Press " + in.Keys
		run = func(ctx context.Context) error { return c.Tools.Key(ctx, in.Keys) }
		if c.KeyCombo != nil {
			run = func(ctx context.Context) error { return c.KeyCombo(ctx, in.Keys) }
		}
	case "move":
		if err := needPtr(); err != nil {
			return nil, err
		}
		if in.X == nil || in.Y == nil {
			return nil, errors.New("move needs x and y")
		}
		x, y := *in.X, *in.Y
		summary = fmt.Sprintf("Move the pointer to %d,%d", x, y)
		run = func(ctx context.Context) error { return ptr.PointerMove(ctx, x, y) }
	case "click":
		if err := needPtr(); err != nil {
			return nil, err
		}
		b := in.Button
		if b == "" {
			b = "left"
		}
		act := in.Action
		if act == "" {
			act = "click"
		}
		if !oneOf(b, "left", "middle", "right") || !oneOf(act, "click", "double", "press", "release") {
			return nil, errors.New("button must be left, middle or right; action click, double, press or release")
		}
		summary = fmt.Sprintf("%s %s button", strings.Title(act), b) //nolint:staticcheck // ASCII words
		if in.X != nil && in.Y != nil {
			summary += fmt.Sprintf(" at %d,%d", *in.X, *in.Y)
		}
		run = func(ctx context.Context) error {
			if in.X != nil && in.Y != nil {
				if err := ptr.PointerMove(ctx, *in.X, *in.Y); err != nil {
					return err
				}
			}
			n, a := 1, act
			if act == "double" {
				n, a = 2, "click"
			}
			for i := 0; i < n; i++ {
				if err := ptr.PointerButton(ctx, b, a); err != nil {
					return err
				}
			}
			return nil
		}
	case "scroll":
		if err := needPtr(); err != nil {
			return nil, err
		}
		if in.DX == 0 && in.DY == 0 || in.DX > 50 || in.DX < -50 || in.DY > 50 || in.DY < -50 {
			return nil, errors.New("scroll needs dx or dy between -50 and 50")
		}
		summary = fmt.Sprintf("Scroll %d,%d", in.DX, in.DY)
		run = func(ctx context.Context) error { return ptr.PointerScroll(ctx, in.DX, in.DY) }
	default:
		return nil, fmt.Errorf("unknown input kind %q (type, key, move, click, scroll)", in.Kind)
	}
	c.mu.Lock()
	c.lastInput = time.Now()
	c.mu.Unlock()
	err := run(ctx)
	c.mu.Lock()
	c.lastInput = time.Now()
	if c.control == ct {
		ct.Inputs++
	}
	c.mu.Unlock()
	data := map[string]any{"control": ct.ID, "kind": in.Kind}
	if in.Kind == "type" {
		data["text"] = in.Text
	}
	if err != nil {
		data["error"] = err.Error()
		_, _ = c.Audit.Append("fail", m.Actor, summary, data)
		return nil, err
	}
	_, _ = c.Audit.Append("input", m.Actor, summary, data)
	c.Broadcast("agent-activity", map[string]any{"kind": "input", "actor": m.Actor, "summary": summary})
	return map[string]any{"ok": true, "done": summary}, nil
}

func truncate(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n] + "..."
}

// agentActions are the actions only an agent connection may propose.
var agentActions = []*ActionDef{
	{
		Name: "agent.control", Title: "Let an agent control the desktop", Agent: true,
		Description: "Last resort for apps without a typed action: ask the person for a time-limited control session in which this agent may see the screen and use a virtual keyboard and pointer. The shell shows that an agent is in control and the person can stop it at any time.",
		Params: []Param{
			{Name: "reason", Type: "string", Required: true, Description: "what you need to do and why typed actions are not enough (shown to the person)"},
			{Name: "minutes", Type: "integer", Description: "duration, 1 to 15 (default 5)"},
			{Name: "input", Type: "boolean", Description: "keyboard and pointer input (default true)"},
			{Name: "screen", Type: "boolean", Description: "screenshots without asking each time (default true)"},
		},
		plan: func(ctx context.Context, p *planner, a map[string]any) (step, error) {
			if p.meta.PID <= 0 {
				return step{}, errors.New("only an agent connection can ask for control")
			}
			reason := argStr(a, "reason")
			if reason == "" || len(reason) > 300 {
				return step{}, errors.New("reason is required (at most 300 characters)")
			}
			min, ok := argInt(a, "minutes")
			if !ok {
				min = 5
			}
			if min < 1 || min > MaxControlMinutes {
				return step{}, fmt.Errorf("minutes must be 1 to %d", MaxControlMinutes)
			}
			input, ok := argBool(a, "input")
			if !ok {
				input = true
			}
			screen, ok := argBool(a, "screen")
			if !ok {
				screen = true
			}
			if !input && !screen {
				return step{}, errors.New("ask for input, screen or both")
			}
			var what []string
			if screen {
				what = append(what, "see the screen")
			}
			if input {
				what = append(what, "type and use the pointer")
			}
			m := p.meta
			sum := fmt.Sprintf("Let %s control the desktop for %d minutes: %s. Reason: %s", m.Actor, min, strings.Join(what, ", "), reason)
			return step{Summary: sum, run: func(ctx context.Context) (any, error) {
				return p.c.startControl(m, min, input, screen, reason)
			}}, nil
		},
	},
	{
		Name: "screen.capture", Title: "Show a screenshot to an agent", Agent: true,
		Description: "A screenshot of a screen or of one window, for the agent that asked.",
		Params: []Param{
			{Name: "target", Type: "string", Enum: []string{"output", "window"}, Description: "output (a whole screen, default) or window"},
			{Name: "output", Type: "string", Description: "output name from desktop_state (default: the focused one)"},
			{Name: "window", Type: "string", Description: "window id, app id, title fragment or \"focused\" (target window)"},
			{Name: "max_width", Type: "integer", Description: "scale the image down to at most this width (default 1600)"},
		},
		plan: func(ctx context.Context, p *planner, a map[string]any) (step, error) {
			spec, sum, err := p.planCapture(a)
			if err != nil {
				return step{}, err
			}
			return step{Summary: sum, run: func(ctx context.Context) (any, error) {
				return p.c.screenshot(ctx, spec)
			}}, nil
		},
	},
}

func init() { Actions = append(Actions, agentActions...) }

// AgentIO says which last-resort capabilities exist on this desktop.
func (c *Core) AgentIO() map[string]any {
	caps := c.Comp.Caps()
	_, ptr := c.Comp.(compositor.Pointer)
	ptr = ptr && caps.Pointer || c.VirtualInput
	return map[string]any{
		"screen_capture":   c.Tools.Grim != "" || c.Screenshot != nil,
		"toplevel_capture": caps.ToplevelCapture && (c.Tools.Grim != "" || c.Screenshot != nil),
		"keyboard":         c.Tools.Wtype != "" || c.TypeText != nil,
		"pointer":          ptr,
		"virtual_devices":  c.VirtualInput,
	}
}
