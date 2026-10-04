// Package fake is an in-memory compositor for tests and for running the
// daemon without a compositor (BASALT_SHELL_COMPOSITOR=fake).
package fake

import (
	"context"
	"fmt"
	"os"
	"strconv"
	"sync"

	"github.com/basalt-os/basalt-shell/internal/compositor"
)

func init() {
	compositor.Register("fake", func() compositor.Adapter {
		if os.Getenv("BASALT_SHELL_COMPOSITOR") != "fake" {
			return nil
		}
		return New()
	})
}

// Adapter keeps windows and workspaces in memory and records calls.
type Adapter struct {
	mu     sync.Mutex
	Wins   []compositor.Window
	Spaces []compositor.Workspace
	Outs   []compositor.Output
	Style  compositor.Style
	Calls  []string
	nextID int
}

// New returns a desktop with two workspaces and two windows.
func New() *Adapter {
	return &Adapter{
		Spaces: []compositor.Workspace{
			{ID: "10", Index: 1, Name: "1", Output: "Virtual-1", Focused: true, Visible: true},
			{ID: "11", Index: 2, Name: "2", Output: "Virtual-1"},
		},
		Outs: []compositor.Output{{Name: "Virtual-1", Rect: compositor.Rect{W: 1920, H: 1080}, Scale: 1, Focused: true}},
		Wins: []compositor.Window{
			{ID: "1", AppID: "foot", Title: "foot", Workspace: "10", Focused: true, Floating: true, Rect: compositor.Rect{X: 100, Y: 100, W: 800, H: 500}},
			{ID: "2", AppID: "org.gnome.TextEditor", Title: "Text Editor", Workspace: "10", Floating: true, Rect: compositor.Rect{X: 300, Y: 200, W: 900, H: 600}},
		},
		nextID: 3,
	}
}

func (a *Adapter) rec(s string) { a.mu.Lock(); a.Calls = append(a.Calls, s); a.mu.Unlock() }

func (a *Adapter) Name() string                   { return "fake" }
func (a *Adapter) Version(context.Context) string { return "fake 1" }
func (a *Adapter) Caps() compositor.Caps {
	return compositor.Caps{Floating: true, MoveResize: true, FloatByDefault: true, LiveCorners: true, LiveBorders: true, Events: true, Pointer: true, Minimize: true, TitleBars: true}
}
func (a *Adapter) Windows(context.Context) ([]compositor.Window, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	return append([]compositor.Window(nil), a.Wins...), nil
}
func (a *Adapter) Workspaces(context.Context) ([]compositor.Workspace, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	out := append([]compositor.Workspace(nil), a.Spaces...)
	for i := range out {
		for _, w := range a.Wins {
			if w.Workspace == out[i].ID {
				out[i].Windows++
			}
		}
	}
	return out, nil
}
func (a *Adapter) Outputs(context.Context) ([]compositor.Output, error) { return a.Outs, nil }
func (a *Adapter) find(id string) (int, error) {
	for i, w := range a.Wins {
		if w.ID == id {
			return i, nil
		}
	}
	return -1, fmt.Errorf("no window %s", id)
}
func (a *Adapter) Focus(_ context.Context, id string) error {
	a.rec("focus " + id)
	a.mu.Lock()
	defer a.mu.Unlock()
	i, err := a.find(id)
	if err != nil {
		return err
	}
	for j := range a.Wins {
		a.Wins[j].Focused = j == i
	}
	return nil
}
func (a *Adapter) Close(_ context.Context, id string) error {
	a.rec("close " + id)
	a.mu.Lock()
	defer a.mu.Unlock()
	i, err := a.find(id)
	if err != nil {
		return err
	}
	a.Wins = append(a.Wins[:i], a.Wins[i+1:]...)
	return nil
}
func (a *Adapter) SetFloating(_ context.Context, id string, on bool) error {
	a.rec("floating " + id + " " + strconv.FormatBool(on))
	a.mu.Lock()
	defer a.mu.Unlock()
	i, err := a.find(id)
	if err != nil {
		return err
	}
	a.Wins[i].Floating = on
	return nil
}
func (a *Adapter) MoveResize(_ context.Context, id string, r compositor.Rect) error {
	a.rec(fmt.Sprintf("move %s %d %d %d %d", id, r.X, r.Y, r.W, r.H))
	a.mu.Lock()
	defer a.mu.Unlock()
	i, err := a.find(id)
	if err != nil {
		return err
	}
	a.Wins[i].Floating, a.Wins[i].Rect = true, r
	return nil
}
func (a *Adapter) MoveToWorkspace(_ context.Context, id string, ws compositor.Workspace) error {
	a.rec("to_workspace " + id + " " + ws.Name)
	a.mu.Lock()
	defer a.mu.Unlock()
	i, err := a.find(id)
	if err != nil {
		return err
	}
	a.Wins[i].Workspace = ws.ID
	return nil
}
func (a *Adapter) SwitchWorkspace(_ context.Context, ws compositor.Workspace) error {
	a.rec("workspace " + ws.Name)
	a.mu.Lock()
	defer a.mu.Unlock()
	for i := range a.Spaces {
		a.Spaces[i].Focused = a.Spaces[i].ID == ws.ID || a.Spaces[i].Name == ws.Name
	}
	return nil
}
func (a *Adapter) Spawn(_ context.Context, argv []string) error {
	a.rec(fmt.Sprintf("spawn %q", argv))
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Wins = append(a.Wins, compositor.Window{ID: strconv.Itoa(a.nextID), AppID: argv[0], Title: argv[0], Workspace: "10", Floating: true})
	a.nextID++
	return nil
}
func (a *Adapter) ApplyStyle(_ context.Context, s compositor.Style) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	a.Style = s
	a.Calls = append(a.Calls, fmt.Sprintf("style radius=%d", s.CornerRadius))
	return nil
}
func (a *Adapter) Subscribe(ctx context.Context) (<-chan compositor.Event, error) {
	ch := make(chan compositor.Event)
	go func() { <-ctx.Done(); close(ch) }()
	return ch, nil
}

// PointerMove records a pointer move.
func (a *Adapter) PointerMove(_ context.Context, x, y int) error {
	a.rec(fmt.Sprintf("pointer move %d %d", x, y))
	return nil
}

// PointerButton records a button action.
func (a *Adapter) PointerButton(_ context.Context, button, action string) error {
	a.rec("pointer " + action + " " + button)
	return nil
}

// PointerScroll records a scroll.
func (a *Adapter) PointerScroll(_ context.Context, dx, dy int) error {
	a.rec(fmt.Sprintf("pointer scroll %d %d", dx, dy))
	return nil
}

// Minimize hides a window (no workspace, state minimized).
func (a *Adapter) Minimize(_ context.Context, id string) error {
	a.rec("minimize " + id)
	a.mu.Lock()
	defer a.mu.Unlock()
	i, err := a.find(id)
	if err != nil {
		return err
	}
	a.Wins[i].Workspace, a.Wins[i].State, a.Wins[i].Focused = "", "minimized", false
	return nil
}

// Unminimize shows a window on the focused workspace and focuses it.
func (a *Adapter) Unminimize(_ context.Context, id string) error {
	a.rec("unminimize " + id)
	a.mu.Lock()
	defer a.mu.Unlock()
	i, err := a.find(id)
	if err != nil {
		return err
	}
	ws := ""
	for _, s := range a.Spaces {
		if s.Focused {
			ws = s.ID
		}
	}
	for j := range a.Wins {
		a.Wins[j].Focused = j == i
	}
	a.Wins[i].Workspace, a.Wins[i].State = ws, ""
	return nil
}
