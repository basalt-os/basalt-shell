// Package niri is the niri backend: newline-delimited JSON requests and
// replies on the socket named by $NIRI_SOCKET (the niri_ipc crate's
// externally tagged enums: {"Ok": ...} or {"Err": "..."}).
//
// niri has no runtime command for window styling, so ApplyStyle writes a
// managed config file that the main config includes; niri reloads its
// configuration when the file changes.
package niri

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/openbasalt/basalt-shell/internal/compositor"
)

func init() {
	compositor.Register(func() compositor.Adapter {
		p := os.Getenv("NIRI_SOCKET")
		if p == "" {
			return nil
		}
		if _, err := os.Stat(p); err != nil {
			return nil
		}
		return New(p)
	})
}

// Adapter talks to niri.
type Adapter struct {
	path string
	mu   sync.Mutex
	// StylePath is the managed include file: basalt-theme.kdl next to
	// $NIRI_CONFIG, else $XDG_CONFIG_HOME/niri/basalt-theme.kdl.
	StylePath string
}

// New returns an adapter for the socket at path.
func New(path string) *Adapter {
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		home, _ := os.UserHomeDir()
		cfg = filepath.Join(home, ".config")
	}
	style := filepath.Join(cfg, "niri", "basalt-theme.kdl")
	if nc := os.Getenv("NIRI_CONFIG"); nc != "" {
		style = filepath.Join(filepath.Dir(nc), "basalt-theme.kdl")
	}
	return &Adapter{path: path, StylePath: style}
}

// request opens a connection per request: niri closes the request side
// after one reply on some versions, and requests are cheap.
func (a *Adapter) request(req any, out any) error {
	c, err := net.DialTimeout("unix", a.path, 2*time.Second)
	if err != nil {
		return err
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(5 * time.Second))
	b, err := json.Marshal(req)
	if err != nil {
		return err
	}
	if _, err := c.Write(append(b, '\n')); err != nil {
		return err
	}
	r := bufio.NewReader(c)
	line, err := r.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		return err
	}
	var reply map[string]json.RawMessage
	if err := json.Unmarshal(line, &reply); err != nil {
		return fmt.Errorf("niri: bad reply: %v", err)
	}
	if e, ok := reply["Err"]; ok {
		var msg string
		_ = json.Unmarshal(e, &msg)
		return errors.New("niri: " + msg)
	}
	okv, ok := reply["Ok"]
	if !ok {
		return errors.New("niri: reply without Ok")
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(okv, out)
}

func (a *Adapter) action(name string, body any) error {
	return a.request(map[string]any{"Action": map[string]any{name: body}}, nil)
}

// Name is "niri".
func (a *Adapter) Name() string { return "niri" }

// Version asks niri for its version.
func (a *Adapter) Version(context.Context) string {
	var v struct {
		Version string `json:"Version"`
	}
	if err := a.request("Version", &v); err != nil {
		return ""
	}
	return "niri " + v.Version
}

// Caps of niri: floating windows exist (since 25.01) and can be placed;
// styling goes through the config file.
func (a *Adapter) Caps() compositor.Caps {
	return compositor.Caps{
		Floating:        true,
		MoveResize:      true,
		FloatByDefault:  true,
		LiveCorners:     true,
		LiveShadows:     true,
		LiveBorders:     true,
		Animations:      true,
		ConfigReload:    true,
		Events:          true,
		WorkspaceByName: true,
	}
}

type window struct {
	ID          uint64  `json:"id"`
	Title       *string `json:"title"`
	AppID       *string `json:"app_id"`
	PID         *int    `json:"pid"`
	WorkspaceID *uint64 `json:"workspace_id"`
	IsFocused   bool    `json:"is_focused"`
	IsFloating  bool    `json:"is_floating"`
	Layout      *struct {
		TileSize           [2]float64  `json:"tile_size"`
		WindowSize         [2]int      `json:"window_size"`
		TilePosInWorkspace *[2]float64 `json:"tile_pos_in_workspace_view"`
	} `json:"layout"`
}

type workspace struct {
	ID             uint64  `json:"id"`
	Idx            int     `json:"idx"`
	Name           *string `json:"name"`
	Output         *string `json:"output"`
	IsActive       bool    `json:"is_active"`
	IsFocused      bool    `json:"is_focused"`
	ActiveWindowID *uint64 `json:"active_window_id"`
}

func (a *Adapter) rawWorkspaces() ([]workspace, error) {
	var r struct {
		Workspaces []workspace `json:"Workspaces"`
	}
	if err := a.request("Workspaces", &r); err != nil {
		return nil, err
	}
	sort.Slice(r.Workspaces, func(i, j int) bool {
		wi, wj := r.Workspaces[i], r.Workspaces[j]
		oi, oj := str(wi.Output), str(wj.Output)
		if oi != oj {
			return oi < oj
		}
		return wi.Idx < wj.Idx
	})
	return r.Workspaces, nil
}

func str(p *string) string {
	if p == nil {
		return ""
	}
	return *p
}

// Windows lists toplevels. Positions are known for floating windows
// (relative to their output); tiled windows report their size only.
func (a *Adapter) Windows(ctx context.Context) ([]compositor.Window, error) {
	var r struct {
		Windows []window `json:"Windows"`
	}
	if err := a.request("Windows", &r); err != nil {
		return nil, err
	}
	outs, _ := a.Outputs(ctx)
	wss, _ := a.rawWorkspaces()
	wsOut := map[uint64]compositor.Rect{}
	for _, ws := range wss {
		for _, o := range outs {
			if o.Name == str(ws.Output) {
				wsOut[ws.ID] = o.Rect
			}
		}
	}
	out := make([]compositor.Window, 0, len(r.Windows))
	for _, w := range r.Windows {
		cw := compositor.Window{
			ID:       strconv.FormatUint(w.ID, 10),
			AppID:    str(w.AppID),
			Title:    str(w.Title),
			Focused:  w.IsFocused,
			Floating: w.IsFloating,
		}
		if w.PID != nil {
			cw.PID = *w.PID
		}
		if w.WorkspaceID != nil {
			cw.Workspace = strconv.FormatUint(*w.WorkspaceID, 10)
		}
		if w.Layout != nil {
			cw.Rect.W, cw.Rect.H = w.Layout.WindowSize[0], w.Layout.WindowSize[1]
			if p := w.Layout.TilePosInWorkspace; p != nil {
				base := compositor.Rect{}
				if w.WorkspaceID != nil {
					base = wsOut[*w.WorkspaceID]
				}
				cw.Rect.X, cw.Rect.Y = base.X+int(p[0]), base.Y+int(p[1])
			}
		}
		out = append(out, cw)
	}
	return out, nil
}

// Workspaces lists workspaces in output order. niri's workspaces are
// dynamic: there is always one empty workspace at the end of each output.
func (a *Adapter) Workspaces(ctx context.Context) ([]compositor.Workspace, error) {
	raw, err := a.rawWorkspaces()
	if err != nil {
		return nil, err
	}
	wins, _ := a.Windows(ctx)
	count := map[string]int{}
	for _, w := range wins {
		count[w.Workspace]++
	}
	out := make([]compositor.Workspace, 0, len(raw))
	for _, w := range raw {
		id := strconv.FormatUint(w.ID, 10)
		name := str(w.Name)
		if name == "" {
			name = strconv.Itoa(w.Idx)
		}
		out = append(out, compositor.Workspace{
			ID: id, Index: w.Idx, Name: name, Output: str(w.Output),
			Focused: w.IsFocused, Visible: w.IsActive, Windows: count[id],
		})
	}
	return out, nil
}

// Outputs lists outputs with their logical geometry.
func (a *Adapter) Outputs(context.Context) ([]compositor.Output, error) {
	var r struct {
		Outputs map[string]struct {
			Name    string `json:"name"`
			Logical *struct {
				X      int     `json:"x"`
				Y      int     `json:"y"`
				Width  int     `json:"width"`
				Height int     `json:"height"`
				Scale  float64 `json:"scale"`
			} `json:"logical"`
		} `json:"Outputs"`
	}
	if err := a.request("Outputs", &r); err != nil {
		return nil, err
	}
	var focused string
	var fo struct {
		FocusedOutput *struct {
			Name string `json:"name"`
		} `json:"FocusedOutput"`
	}
	if a.request("FocusedOutput", &fo) == nil && fo.FocusedOutput != nil {
		focused = fo.FocusedOutput.Name
	}
	var out []compositor.Output
	for name, o := range r.Outputs {
		if o.Logical == nil {
			continue
		}
		out = append(out, compositor.Output{
			Name:    name,
			Rect:    compositor.Rect{X: o.Logical.X, Y: o.Logical.Y, W: o.Logical.Width, H: o.Logical.Height},
			Scale:   o.Logical.Scale,
			Focused: name == focused,
		})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out, nil
}

func parseID(id string) (uint64, error) {
	n, err := strconv.ParseUint(id, 10, 64)
	if err != nil {
		return 0, fmt.Errorf("invalid window id %q", id)
	}
	return n, nil
}

// Focus focuses a window.
func (a *Adapter) Focus(_ context.Context, id string) error {
	n, err := parseID(id)
	if err != nil {
		return err
	}
	return a.action("FocusWindow", map[string]any{"id": n})
}

// Close asks a window to close.
func (a *Adapter) Close(_ context.Context, id string) error {
	n, err := parseID(id)
	if err != nil {
		return err
	}
	return a.action("CloseWindow", map[string]any{"id": n})
}

// SetFloating moves a window between the floating and tiling layers.
func (a *Adapter) SetFloating(_ context.Context, id string, on bool) error {
	n, err := parseID(id)
	if err != nil {
		return err
	}
	if on {
		return a.action("MoveWindowToFloating", map[string]any{"id": n})
	}
	return a.action("MoveWindowToTiling", map[string]any{"id": n})
}

// MoveResize floats the window, sizes it, then places it. niri takes
// floating positions relative to the output, in logical pixels.
func (a *Adapter) MoveResize(ctx context.Context, id string, r compositor.Rect) error {
	n, err := parseID(id)
	if err != nil {
		return err
	}
	if err := a.action("MoveWindowToFloating", map[string]any{"id": n}); err != nil {
		return err
	}
	if err := a.action("SetWindowWidth", map[string]any{"id": n, "change": map[string]any{"SetFixed": r.W}}); err != nil {
		return err
	}
	if err := a.action("SetWindowHeight", map[string]any{"id": n, "change": map[string]any{"SetFixed": r.H}}); err != nil {
		return err
	}
	x, y := float64(r.X), float64(r.Y)
	if outs, err := a.Outputs(ctx); err == nil {
		for _, o := range outs {
			if r.X >= o.Rect.X && r.X < o.Rect.X+o.Rect.W && r.Y >= o.Rect.Y && r.Y < o.Rect.Y+o.Rect.H {
				x, y = float64(r.X-o.Rect.X), float64(r.Y-o.Rect.Y)
				break
			}
		}
	}
	return a.action("MoveFloatingWindow", map[string]any{
		"id": n, "x": map[string]any{"SetFixed": x}, "y": map[string]any{"SetFixed": y},
	})
}

func wsRef(ws compositor.Workspace) map[string]any {
	if id, err := strconv.ParseUint(ws.ID, 10, 64); err == nil {
		return map[string]any{"Id": id}
	}
	if ws.Index > 0 {
		return map[string]any{"Index": ws.Index}
	}
	return map[string]any{"Name": ws.Name}
}

// MoveToWorkspace sends a window to a workspace without following it.
func (a *Adapter) MoveToWorkspace(_ context.Context, id string, ws compositor.Workspace) error {
	n, err := parseID(id)
	if err != nil {
		return err
	}
	return a.action("MoveWindowToWorkspace", map[string]any{"window_id": n, "reference": wsRef(ws), "focus": false})
}

// SwitchWorkspace focuses a workspace.
func (a *Adapter) SwitchWorkspace(_ context.Context, ws compositor.Workspace) error {
	return a.action("FocusWorkspace", map[string]any{"reference": wsRef(ws)})
}

// Spawn starts a program in niri's environment.
func (a *Adapter) Spawn(_ context.Context, argv []string) error {
	if len(argv) == 0 {
		return errors.New("empty command")
	}
	return a.action("Spawn", map[string]any{"command": argv})
}

// kdlString quotes a KDL string.
func kdlString(s string) string {
	b, _ := json.Marshal(s)
	return string(b)
}

// StyleKDL renders the managed include for a style.
func StyleKDL(s compositor.Style) string {
	var b strings.Builder
	b.WriteString("// Managed by basalt-shell: rewritten on every theme change. Do not edit.\n")
	fmt.Fprintf(&b, "layout {\n    gaps %d\n", s.Gaps)
	fmt.Fprintf(&b, "    focus-ring {\n        width %d\n        active-color %s\n        inactive-color %s\n        urgent-color %s\n    }\n",
		max(1, s.BorderWidth), kdlString(s.FocusColor), kdlString(s.InactiveColor), kdlString(orDefault(s.UrgentColor, s.FocusColor)))
	b.WriteString("    border {\n        off\n    }\n")
	if s.Shadows {
		blur := s.ShadowBlur
		if blur <= 0 {
			blur = 30
		}
		fmt.Fprintf(&b, "    shadow {\n        on\n        softness %d\n        spread 4\n        offset x=0 y=6\n        color %s\n    }\n",
			blur, kdlString(orDefault(s.ShadowColor, "#00000070")))
	} else {
		b.WriteString("    shadow {\n        off\n    }\n")
	}
	b.WriteString("}\n")
	fmt.Fprintf(&b, "window-rule {\n    geometry-corner-radius %d\n    clip-to-geometry true\n}\n", s.CornerRadius)
	if !s.Animations {
		b.WriteString("animations {\n    off\n}\n")
	}
	if s.CursorTheme != "" {
		size := s.CursorSize
		if size <= 0 {
			size = 24
		}
		fmt.Fprintf(&b, "cursor {\n    xcursor-theme %s\n    xcursor-size %d\n}\n", kdlString(s.CursorTheme), size)
	}
	return b.String()
}

func orDefault(s, d string) string {
	if s == "" {
		return d
	}
	return s
}

// ApplyStyle rewrites the managed include (only when it changed, to
// avoid needless reloads); niri watches its config and reloads it.
func (a *Adapter) ApplyStyle(_ context.Context, s compositor.Style) error {
	a.mu.Lock()
	defer a.mu.Unlock()
	body := StyleKDL(s)
	if old, err := os.ReadFile(a.StylePath); err == nil && string(old) == body {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(a.StylePath), 0o755); err != nil {
		return err
	}
	tmp := a.StylePath + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, a.StylePath)
}

// Subscribe reads niri's event stream.
func (a *Adapter) Subscribe(ctx context.Context) (<-chan compositor.Event, error) {
	c, err := net.DialTimeout("unix", a.path, 2*time.Second)
	if err != nil {
		return nil, err
	}
	if _, err := c.Write([]byte("\"EventStream\"\n")); err != nil {
		c.Close()
		return nil, err
	}
	r := bufio.NewReaderSize(c, 1<<20)
	first, err := r.ReadBytes('\n')
	if err != nil {
		c.Close()
		return nil, err
	}
	if !strings.Contains(string(first), "Ok") {
		c.Close()
		return nil, fmt.Errorf("niri: event stream refused: %s", strings.TrimSpace(string(first)))
	}
	ch := make(chan compositor.Event, 16)
	go func() { <-ctx.Done(); c.Close() }()
	go func() {
		defer close(ch)
		for {
			line, err := r.ReadBytes('\n')
			if err != nil {
				return
			}
			var ev map[string]json.RawMessage
			if json.Unmarshal(line, &ev) != nil {
				continue
			}
			for k := range ev {
				kind := ""
				switch {
				case strings.HasPrefix(k, "Workspace"):
					kind = "workspaces"
				case strings.HasPrefix(k, "Window"):
					kind = "windows"
				}
				if kind == "" {
					continue
				}
				select {
				case ch <- compositor.Event{Kind: kind}:
				default:
				}
			}
		}
	}()
	return ch, nil
}
