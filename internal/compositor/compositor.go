// Package compositor is the window-management seam of the shell: one
// interface that the rest of the shell (actions, MCP tools, the panel)
// uses, and one backend per compositor. Backends speak the compositor's
// own IPC (sway: the i3 JSON IPC; niri: its JSON socket), so the
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
	// ForeignID is the window's ext-foreign-toplevel-list identifier, when
	// the compositor reports it (sway 1.11+): screen capture of exactly
	// this window (grim -T), without the windows on top of it.
	ForeignID string `json:"foreign_id,omitempty"`
	// State is "" (normal), "minimized" (hidden; the panel's window list
	// restores it), "maximized", "left" or "right" (snapped halves).
	State string `json:"state,omitempty"`
	// Decoration is who draws the title bar: "client" (the app's own
	// headerbar), "server" (the compositor's themed title bar) or "".
	Decoration string `json:"decoration,omitempty"`
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
	// Rect is the usable area (the output minus panels), when known.
	Rect Rect `json:"rect"`
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
	Pointer         bool `json:"pointer"`          // absolute pointer moves and clicks (Pointer interface)
	ToplevelCapture bool `json:"toplevel_capture"` // windows carry a foreign-toplevel identifier
	Minimize        bool `json:"minimize"`         // windows can be minimized and restored (Minimizer)
	// ClientMaximize: the compositor honors the maximize and minimize
	// buttons of client-side decorations (xdg-shell requests). sway does
	// not (it advertises only fullscreen), so the shell offers those
	// through the panel, the window menu and keys instead, and asks GTK
	// to show only the close button.
	ClientMaximize bool `json:"client_maximize"`
	// ClientMinimize: the compositor honors the minimize button.
	ClientMinimize bool `json:"client_minimize"`
	// TitleBars: the compositor draws title bars for windows that do not
	// draw their own.
	TitleBars bool `json:"title_bars"`
}

// Style is the part of the theme the compositor draws (window borders,
// corners, shadows, gaps, animations).
type Style struct {
	CornerRadius  int    `json:"corner_radius"`
	BorderWidth   int    `json:"border_width"`
	Gaps          int    `json:"gaps"`
	FocusColor    string `json:"focus_color"`    // #RRGGBB
	Accent        string `json:"accent"`         // #RRGGBB: niri's focus ring
	InactiveColor string `json:"inactive_color"` // #RRGGBB
	UrgentColor   string `json:"urgent_color"`
	Shadows       bool   `json:"shadows"`
	ShadowColor   string `json:"shadow_color"` // #RRGGBBAA
	ShadowBlur    int    `json:"shadow_blur"`
	// ShadowOffsetY drops the shadow below the window (px); the inactive
	// windows' shadow is lighter (ShadowInactive, #RRGGBBAA).
	ShadowOffsetY  int    `json:"shadow_offset_y"`
	ShadowInactive string `json:"shadow_inactive_color,omitempty"`
	Blur           bool   `json:"blur"`
	// LayerBlur blurs what is behind the shell's translucent surfaces (the
	// panel and the popovers), where the compositor can; never behind
	// windows (Blur), and off without a GPU.
	LayerBlur   bool    `json:"layer_blur"`
	DimInactive float64 `json:"dim_inactive"`
	Animations  bool    `json:"animations"`
	CursorTheme string  `json:"cursor_theme"`
	CursorSize  int     `json:"cursor_size"`
	// Title bars (drawn by the compositor for windows without their own).
	Title TitleStyle `json:"title"`
}

// TitleStyle is the compositor's title bar, from the theme tokens.
type TitleStyle struct {
	Font        string  `json:"font"`         // family
	Size        float64 `json:"size"`         // points
	Align       string  `json:"align"`        // left, center
	PadX        int     `json:"pad_x"`        // horizontal padding (px)
	PadY        int     `json:"pad_y"`        // vertical padding (px)
	FocusedBg   string  `json:"focused_bg"`   // #RRGGBB, also the focused frame
	FocusedText string  `json:"focused_text"` // #RRGGBB
	InactiveBg  string  `json:"inactive_bg"`
	InactiveTxt string  `json:"inactive_text"`
	UrgentBg    string  `json:"urgent_bg"`
	UrgentText  string  `json:"urgent_text"`
	Radius      int     `json:"radius"` // corner radius, where the compositor draws one
	ColorScheme string  `json:"color_scheme"`
}

// Event is a change notification. Kind is "windows", "workspaces",
// "outputs" or "keyboard" (the layouts or the active layout changed);
// consumers refetch what they need.
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

// Pointer is implemented by backends that can move the pointer and
// click, for the agents' last-resort input (after the person granted a
// control session). Coordinates are global layout coordinates.
type Pointer interface {
	PointerMove(ctx context.Context, x, y int) error
	// PointerButton presses, releases or clicks (press then release)
	// "left", "right" or "middle".
	PointerButton(ctx context.Context, button, action string) error
	// PointerScroll scrolls by steps (positive dy is down, dx is right).
	PointerScroll(ctx context.Context, dx, dy int) error
}

// Framer is implemented by backends that draw title bars and frames
// around windows and can take them away from one window and give them
// back (sway: "border none" and "border normal"). The shell uses it for
// maximized windows: the whole work area goes to the app, and the panel
// entry carries the title and the close button.
type Framer interface {
	// SetFrame gives a window the compositor's title bar and frame (on)
	// or takes them away (off). Windows that draw their own decorations
	// are never changed.
	SetFrame(ctx context.Context, id string, on bool) error
}

// Minimizer is implemented by backends that can hide a window and bring
// it back (sway: the scratchpad).
type Minimizer interface {
	Minimize(ctx context.Context, id string) error
	// Unminimize shows the window on the current workspace and focuses it.
	Unminimize(ctx context.Context, id string) error
}

// VoiceKeys is implemented by backends that can bind keys only while
// the microphone is open in "press to start and stop": Escape cancels
// the utterance, and Super+V keeps working. Outside of it Escape belongs
// to the apps. sway does it with a binding mode ("basalt-voice" in the
// shell's sway config); niri cannot change its bindings at run time.
type VoiceKeys interface {
	SetVoiceKeys(ctx context.Context, on bool) error
}

// KeyboardConfig is the keyboard of the session, in XKB names already
// checked against the system's registry (internal/keyboard). Zero repeat
// values leave the compositor's defaults.
type KeyboardConfig struct {
	Layout      string `json:"layout"`  // "br,us"
	Variant     string `json:"variant"` // ",intl"
	Model       string `json:"model"`
	Options     string `json:"options"` // "grp:alt_shift_toggle,compose:ralt"
	RepeatDelay int    `json:"repeat_delay"`
	RepeatRate  int    `json:"repeat_rate"`
	// SystemLayouts: the layouts are the system's. They are set on the
	// running session, but the file the next session starts with does
	// not name them, so it follows the system's layouts as they are then.
	SystemLayouts bool `json:"system_layouts"`
}

// KeyboardState is what the seat's keyboards use now.
type KeyboardState struct {
	Names   []string `json:"names"`   // the layouts, in the compositor's words
	Current int      `json:"current"` // index of the active one
	// Live: a keyboard reported them. Virtual keyboards (an agent's, wtype)
	// keep their own keymap, so a session with only those reports none.
	Live bool `json:"live"`
}

// Keyboard is implemented by backends that set the keyboard layouts of
// the running session and switch between them.
type Keyboard interface {
	SetKeyboard(ctx context.Context, kb KeyboardConfig) error
	KeyboardState(ctx context.Context) (KeyboardState, error)
	// SwitchLayout goes to the layout at index, or the next one when
	// index is negative.
	SwitchLayout(ctx context.Context, index int) error
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
