// Package compositor is the window-management seam of the shell: one
// interface that the rest of the shell (actions, MCP tools, the panel)
// uses, and one backend per compositor. Backends speak the compositor's
// own IPC (sway/SwayFX: the i3 JSON IPC; niri: its JSON socket), so the
// compositor can be swapped without touching the shell.
package compositor

import (
	"context"
	"errors"
	"os"
	"strconv"
)

// Rect is a rectangle in global layout coordinates (logical pixels).
type Rect struct {
	X int `json:"x"`
	Y int `json:"y"`
	W int `json:"width"`
	H int `json:"height"`
}

// Window is a toplevel as the shell sees it.
type Window struct {
	ID        string `json:"id"`
	AppID     string `json:"app_id"`
	Title     string `json:"title"`
	PID       int    `json:"pid,omitempty"`
	Workspace string `json:"workspace"`
	Focused   bool   `json:"focused"`
	Floating  bool   `json:"floating"`
	XWayland  bool   `json:"xwayland,omitempty"`
	Rect      Rect   `json:"rect"`
}

// Workspace is one workspace. ID is what Switch takes; Index is the
// number people use ("workspace 2").
type Workspace struct {
	ID      string `json:"id"`
	Index   int    `json:"index"`
	Name    string `json:"name"`
	Output  string `json:"output"`
	Focused bool   `json:"focused"`
	Visible bool   `json:"visible"`
	Windows int    `json:"windows"`
}

// Output is a monitor with its usable area.
type Output struct {
	Name    string  `json:"name"`
	Rect    Rect    `json:"rect"`
	Scale   float64 `json:"scale"`
	Focused bool    `json:"focused"`
}

// Caps says what a backend can do, so tools can be honest about it.
type Caps struct {
	Floating        bool `json:"floating"`         // windows can float
	MoveResize      bool `json:"move_resize"`      // floating windows can be placed exactly
	FloatByDefault  bool `json:"float_by_default"` // the shipped config floats new windows
	LiveCorners     bool `json:"live_corners"`     // window corner radius changes at runtime
	LiveShadows     bool `json:"live_shadows"`     // window shadows change at runtime
	LiveBorders     bool `json:"live_borders"`     // border colors and width change at runtime
	Animations      bool `json:"animations"`       // the compositor animates windows
	ConfigReload    bool `json:"config_reload"`    // style is applied by rewriting a watched config file
	Events          bool `json:"events"`           // event stream for live updates
	WorkspaceByName bool `json:"workspace_by_name"`
}

// Style is the part of the theme the compositor draws (window borders,
// corners, shadows, gaps, animations).
type Style struct {
	CornerRadius  int     `json:"corner_radius"`
	BorderWidth   int     `json:"border_width"`
	Gaps          int     `json:"gaps"`
	FocusColor    string  `json:"focus_color"`    // #RRGGBB
	InactiveColor string  `json:"inactive_color"` // #RRGGBB
	UrgentColor   string  `json:"urgent_color"`
	Shadows       bool    `json:"shadows"`
	ShadowColor   string  `json:"shadow_color"` // #RRGGBBAA
	ShadowBlur    int     `json:"shadow_blur"`
	Blur          bool    `json:"blur"`
	DimInactive   float64 `json:"dim_inactive"`
	Animations    bool    `json:"animations"`
	CursorTheme   string  `json:"cursor_theme"`
	CursorSize    int     `json:"cursor_size"`
}

// Event is a change notification. Kind is "windows", "workspaces" or
// "outputs"; consumers refetch what they need.
type Event struct {
	Kind string `json:"kind"`
}

// Adapter is one compositor backend.
type Adapter interface {
	Name() string
	Version(ctx context.Context) string
	Caps() Caps
	Windows(ctx context.Context) ([]Window, error)
	Workspaces(ctx context.Context) ([]Workspace, error)
	Outputs(ctx context.Context) ([]Output, error)
	Focus(ctx context.Context, id string) error
	Close(ctx context.Context, id string) error
	SetFloating(ctx context.Context, id string, on bool) error
	// MoveResize makes the window floating and places it at r.
	MoveResize(ctx context.Context, id string, r Rect) error
	MoveToWorkspace(ctx context.Context, id string, ws Workspace) error
	SwitchWorkspace(ctx context.Context, ws Workspace) error
	Spawn(ctx context.Context, argv []string) error
	ApplyStyle(ctx context.Context, s Style) error
	// Subscribe delivers events until ctx ends. The channel closes when
	// the connection drops.
	Subscribe(ctx context.Context) (<-chan Event, error)
}

// ErrUnsupported is returned for an operation a backend cannot do.
var ErrUnsupported = errors.New("not supported by this compositor")

// ErrNoCompositor means no supported compositor was found.
var ErrNoCompositor = errors.New("no supported compositor (set SWAYSOCK or NIRI_SOCKET)")

// Factory builds an adapter from the environment, or returns nil.
type Factory func() Adapter

type named struct {
	name string
	f    Factory
}

var factories []named

// Register adds a backend factory under a name; backends call it from init.
func Register(name string, f Factory) { factories = append(factories, named{name, f}) }

// Detect returns the backend named by BASALT_SHELL_COMPOSITOR (set by
// basalt-session; "none" for no compositor), else the first backend whose
// compositor is running, or a stub that answers every call with
// ErrNoCompositor.
func Detect() Adapter {
	want := os.Getenv("BASALT_SHELL_COMPOSITOR")
	if want == "none" {
		return None{}
	}
	for _, f := range factories {
		if want != "" && f.name != want {
			continue
		}
		if a := f.f(); a != nil {
			return a
		}
	}
	return None{}
}

// FindWorkspace resolves a reference (index number, id or name) against
// the current workspaces.
func FindWorkspace(list []Workspace, ref string) (Workspace, bool) {
	for _, w := range list {
		if w.ID == ref || w.Name == ref {
			return w, true
		}
	}
	for _, w := range list {
		if strconv.Itoa(w.Index) == ref {
			return w, true
		}
	}
	return Workspace{}, false
}
