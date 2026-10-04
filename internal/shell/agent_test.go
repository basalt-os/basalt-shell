package shell

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/basalt-os/basalt-shell/internal/agentio"
)

func agentMeta(pid int) Meta {
	return Meta{Origin: "mcp", Actor: "agent:mcp (basalt_agent_mcp_t)", PID: pid, Domain: "basalt_agent_mcp_t"}
}

func TestControlSessionInputRules(t *testing.T) {
	c, fk, _ := newCore(t)
	ctx := context.Background()
	var typed []string
	c.TypeText = func(_ context.Context, s string) error { typed = append(typed, s); return nil }
	m := agentMeta(4242)

	// No session: input refused.
	if _, err := c.Input(ctx, m, InputRequest{Kind: "type", Text: "hi"}); err == nil {
		t.Fatal("input without a control session ran")
	}
	// The command bar or the UI cannot hold a session.
	if _, err := c.Propose(ctx, Meta{Origin: "commandbar", Actor: "commandbar"}, []Call{{Action: "agent.control", Args: map[string]any{"reason": "x"}}}); err == nil {
		t.Fatal("control session proposed without an agent connection")
	}
	if _, err := c.Execute(ctx, "ui", []Call{{Action: "screen.capture", Args: map[string]any{}}}); err == nil {
		t.Fatal("ui executed an agent action")
	}
	pr, err := c.Propose(ctx, m, []Call{{Action: "agent.control", Args: map[string]any{"reason": "fill a form in an app without actions", "minutes": 2}}})
	if err != nil {
		t.Fatal(err)
	}
	// While the request is pending, input stays refused.
	if _, err := c.Decide(ctx, pr.ID, true, "ui"); err != nil {
		t.Fatal(err)
	}
	st := c.ControlState()
	if st == nil || st.PID != 4242 || !st.Input || !st.Screen {
		t.Fatalf("control state %+v", st)
	}
	if _, err := c.Input(ctx, m, InputRequest{Kind: "type", Text: "hello"}); err != nil {
		t.Fatal(err)
	}
	if len(typed) != 1 || typed[0] != "hello" {
		t.Fatalf("typed %v", typed)
	}
	// Another process cannot use the session.
	if _, err := c.Input(ctx, agentMeta(999), InputRequest{Kind: "type", Text: "x"}); err == nil {
		t.Fatal("another process used the session")
	}
	x, y := 50, 60
	if _, err := c.Input(ctx, m, InputRequest{Kind: "click", X: &x, Y: &y}); err != nil {
		t.Fatal(err)
	}
	found := false
	for _, call := range fk.Calls {
		if call == "pointer click left" {
			found = true
		}
	}
	if !found {
		t.Fatalf("no click in %v", fk.Calls)
	}
	// A confirmation right after synthetic input is refused.
	other, err := c.Propose(ctx, agentMeta(7), []Call{{Action: "theme.switch", Args: map[string]any{"mode": "light"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Decide(ctx, other.ID, true, "ui"); err == nil || !strings.Contains(err.Error(), "agent input") {
		t.Fatalf("confirmation right after input accepted: %v", err)
	}
	// And input is refused while that request waits for the person.
	if _, err := c.Input(ctx, m, InputRequest{Kind: "type", Text: "Enter"}); err == nil {
		t.Fatal("input while a confirmation is pending")
	}
	if _, err := c.Decide(ctx, other.ID, false, "ui"); err != nil {
		t.Fatal(err)
	}
	// A dialog in the UI also blocks input.
	c.SetUIModal(true)
	if _, err := c.Input(ctx, m, InputRequest{Kind: "key", Keys: "ctrl+s"}); err == nil {
		t.Fatal("input while a dialog is open")
	}
	c.SetUIModal(false)
	// After the quiet time the person can confirm again.
	c.mu.Lock()
	c.lastInput = time.Now().Add(-2 * InputQuiet)
	c.mu.Unlock()
	if !c.StopControl("ui", "") {
		t.Fatal("stop")
	}
	if _, err := c.Input(ctx, m, InputRequest{Kind: "type", Text: "x"}); err == nil {
		t.Fatal("input after stop")
	}
	var kinds []string
	for _, r := range c.Audit.Tail(100) {
		kinds = append(kinds, r.Type)
	}
	joined := strings.Join(kinds, ",")
	for _, k := range []string{"control", "input", "refuse"} {
		if !strings.Contains(joined, k) {
			t.Errorf("audit lacks %s: %s", k, joined)
		}
	}
}

func TestCaptureNeedsConfirmationOutsideSession(t *testing.T) {
	c, _, _ := newCore(t)
	ctx := context.Background()
	shots := 0
	c.Screenshot = func(_ context.Context, s agentio.CaptureSpec) (*agentio.Capture, error) {
		shots++
		return &agentio.Capture{PNG: []byte("png"), Width: 10, Height: 10, Target: s.Label, Method: "output"}, nil
	}
	m := agentMeta(31)
	// Declined: no image.
	done := make(chan CaptureResult, 1)
	go func() {
		r, _ := c.Capture(ctx, m, map[string]any{"target": "window", "window": "foot"}, 5*time.Second)
		done <- r
	}()
	var id string
	for i := 0; i < 100 && id == ""; i++ {
		time.Sleep(10 * time.Millisecond)
		if p := c.Pending(); len(p) > 0 {
			id = p[0].ID
		}
	}
	if id == "" {
		t.Fatal("no capture proposal")
	}
	if _, err := c.Decide(ctx, id, false, "ui"); err != nil {
		t.Fatal(err)
	}
	r := <-done
	if r.Status != StatusDeclined || len(r.PNG) != 0 || shots != 0 {
		t.Fatalf("declined capture: %+v shots=%d", r, shots)
	}
	// Confirmed: the agent gets the image once.
	go func() {
		r, _ := c.Capture(ctx, m, map[string]any{}, 5*time.Second)
		done <- r
	}()
	id = ""
	for i := 0; i < 100 && id == ""; i++ {
		time.Sleep(10 * time.Millisecond)
		if p := c.Pending(); len(p) > 0 {
			id = p[0].ID
		}
	}
	if _, err := c.Decide(ctx, id, true, "ui"); err != nil {
		t.Fatal(err)
	}
	r = <-done
	if r.Status != StatusApplied || string(r.PNG) != "png" {
		t.Fatalf("confirmed capture: %+v", r)
	}
	if p, _ := c.Get(id); len(p.Result) != 0 {
		t.Fatal("image kept in the proposal")
	}
}
