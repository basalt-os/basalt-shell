package shell

import (
	"context"
	"strings"
	"testing"
)

// The window states: minimize hides and the panel restores; maximize and
// snap place the window on the usable area and "normal" puts it back.
func TestWindowSetState(t *testing.T) {
	c, fk, _ := newCore(t)
	ctx := context.Background()
	exec := func(state string) {
		t.Helper()
		if _, err := c.Execute(ctx, "ui", []Call{{Action: "window.set_state", Args: map[string]any{"window": "foot", "state": state}}}); err != nil {
			t.Fatalf("%s: %v", state, err)
		}
		c.Refresh(ctx)
	}
	stateOf := func() string {
		for _, w := range c.DesktopState().Windows {
			if w.AppID == "foot" {
				return w.State
			}
		}
		return "gone"
	}

	exec("maximized")
	if s := stateOf(); s != "maximized" {
		t.Fatalf("state after maximize: %q (calls %v)", s, fk.Calls)
	}
	last := fk.Calls[len(fk.Calls)-2]
	// 1920x1080, panel 36 + 2*4 at the top, gaps 8 (basalt theme).
	if last != "move 1 8 52 1904 1020" {
		t.Errorf("maximize placed %q", last)
	}
	exec("left")
	if s := stateOf(); s != "left" {
		t.Fatalf("state after snap: %q", s)
	}
	exec("normal")
	if s := stateOf(); s != "" {
		t.Fatalf("state after restore: %q", s)
	}
	if !strings.Contains(strings.Join(fk.Calls, ","), "move 1 100 100 800 500") {
		t.Errorf("restore did not return to the first geometry: %v", fk.Calls)
	}

	exec("minimized")
	if s := stateOf(); s != "minimized" {
		t.Fatalf("state after minimize: %q", s)
	}
	exec("normal")
	if s := stateOf(); s != "" {
		t.Fatalf("state after unminimize: %q", s)
	}

	// Agents ask; nothing happens before the person confirms.
	pr, err := c.Propose(ctx, Meta{Origin: "agent", Actor: "agent:test"}, []Call{{Action: "window.set_state", Args: map[string]any{"window": "foot", "state": "minimized"}}})
	if err != nil {
		t.Fatal(err)
	}
	if pr.Status != StatusPending || stateOf() != "" {
		t.Fatalf("agent request ran without confirmation: %s %q", pr.Status, stateOf())
	}
	if _, err := c.Execute(ctx, "ui", []Call{{Action: "window.set_state", Args: map[string]any{"window": "foot", "state": "sideways"}}}); err == nil {
		t.Error("unknown state accepted")
	}
}
