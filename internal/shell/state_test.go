package shell

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/basalt-os/basalt-shell/internal/compositor"
	"github.com/basalt-os/basalt-shell/internal/theme"
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
	// 1920x1080, the attached panel 40 at the top: the whole work area,
	// no gaps at the edges (basalt theme).
	if last != "move 1 0 40 1920 1040" {
		t.Errorf("maximize placed %q", last)
	}
	exec("left")
	if s := stateOf(); s != "left" {
		t.Fatalf("state after snap: %q", s)
	}
	// Halves: the gap (6) only between them, none at the edges.
	if last := fk.Calls[len(fk.Calls)-2]; last != "move 1 0 40 957 1040" {
		t.Errorf("snap left placed %q", last)
	}
	exec("right")
	if last := fk.Calls[len(fk.Calls)-2]; last != "move 1 963 40 957 1040" {
		t.Errorf("snap right placed %q", last)
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

// A maximized window with the compositor's title bar loses the bar and
// frame while maximized and gets them back on restore, or when the
// person drags it out of its place; a window drawing its own header is
// never touched.
func TestMaximizeUnframes(t *testing.T) {
	c, fk, _ := newCore(t)
	ctx := context.Background()
	fk.Wins[0].Decoration = "server"
	fk.Wins[1].Decoration = "client"
	exec := func(win, state string) {
		t.Helper()
		if _, err := c.Execute(ctx, "ui", []Call{{Action: "window.set_state", Args: map[string]any{"window": win, "state": state}}}); err != nil {
			t.Fatalf("%s %s: %v", win, state, err)
		}
		c.Refresh(ctx)
	}
	calls := func() string { return strings.Join(fk.Calls, ",") }

	exec("foot", "maximized")
	if !strings.Contains(calls(), "frame 1 false,move 1 0 40 1920 1040") {
		t.Fatalf("maximize did not take the frame first: %v", fk.Calls)
	}
	exec("foot", "normal")
	if !strings.Contains(calls(), "frame 1 true,move 1 100 100 800 500") {
		t.Fatalf("restore did not give the frame back first: %v", fk.Calls)
	}

	// Maximized, then snapped: the frame comes back for the half.
	exec("foot", "maximized")
	exec("foot", "left")
	if n := strings.Count(calls(), "frame 1 true"); n != 2 {
		t.Fatalf("snap after maximize: frame restored %d times: %v", n, fk.Calls)
	}

	// Dragged out of its maximized place: the frame comes back.
	exec("foot", "maximized")
	fk.Wins[0].Rect = compositor.Rect{X: 200, Y: 200, W: 700, H: 400}
	c.mu.Lock()
	pl := c.placed["1"]
	pl.Since = pl.Since.Add(-time.Minute)
	c.placed["1"] = pl
	c.mu.Unlock()
	c.Refresh(ctx)
	if n := strings.Count(calls(), "frame 1 true"); n != 3 {
		t.Fatalf("drag out: frame restored %d times: %v", n, fk.Calls)
	}

	// Client-side decorations are left alone.
	exec("org.gnome.TextEditor", "maximized")
	if strings.Contains(calls(), "frame 2") {
		t.Fatalf("a CSD window was reframed: %v", fk.Calls)
	}
}

// The focused window's frame reaches 3:1 against the background and the
// surface in every shipped theme and mode, and the title bar keeps the
// spec's 12 x 5 padding with the 4 px unit.
func TestFocusFrameContrast(t *testing.T) {
	c, _, _ := newCore(t)
	for _, m := range c.Themes.Themes() {
		for _, mode := range []string{"light", "dark"} {
			tok, err := c.Themes.Resolve(theme.Settings{Theme: m.ID, Mode: mode, Motion: "full"}, false)
			if err != nil {
				t.Fatal(err)
			}
			f := FocusFrame(tok)
			for _, k := range []string{"color.bg", "color.surface"} {
				if r := theme.Contrast(f, tok.Str(k)); r < 3 {
					t.Errorf("%s/%s: frame %s on %s %.2f:1", m.ID, mode, f, k, r)
				}
			}
			if ts := TitleStyle(tok); ts.PadX != 12 || ts.PadY != 5 {
				t.Errorf("%s: title padding %dx%d", m.ID, ts.PadX, ts.PadY)
			}
		}
	}
}

// sway reports the workspace inset by its gaps; maximize still reaches
// the edges and the panel, with or without smart gaps.
func TestMaximizeOnGappedWorkspace(t *testing.T) {
	c, fk, _ := newCore(t)
	ctx := context.Background()
	for _, ws := range []compositor.Rect{{X: 6, Y: 46, W: 1908, H: 1028}, {X: 0, Y: 40, W: 1920, H: 1040}} {
		fk.Spaces[0].Rect = ws
		c.Refresh(ctx)
		if _, err := c.Execute(ctx, "ui", []Call{{Action: "window.set_state", Args: map[string]any{"window": "foot", "state": "maximized"}}}); err != nil {
			t.Fatal(err)
		}
		if last := fk.Calls[len(fk.Calls)-2]; last != "move 1 0 40 1920 1040" {
			t.Errorf("workspace %v: maximize placed %q", ws, last)
		}
	}
}
