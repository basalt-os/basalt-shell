package sway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/openbasalt/basalt-shell/internal/compositor"
)

func init() {
	compositor.Register("sway", func() compositor.Adapter {
		p := os.Getenv("SWAYSOCK")
		if p == "" {
			return nil
		}
		if _, err := os.Stat(p); err != nil {
			return nil
		}
		return New(p)
	})
}

// Adapter talks to sway or SwayFX.
type Adapter struct {
	path string

	mu      sync.Mutex
	c       *conn
	version string
	fx      *bool // SwayFX detected (nil: not probed yet)
}

// New returns an adapter for the socket at path.
func New(path string) *Adapter { return &Adapter{path: path} }

func (a *Adapter) get() (*conn, error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if a.c != nil {
		return a.c, nil
	}
	c, err := dial(a.path)
	if err != nil {
		return nil, err
	}
	a.c = c
	return c, nil
}

// do runs one request, reconnecting once if the connection broke.
func (a *Adapter) do(typ uint32, payload []byte, out any) error {
	for attempt := 0; attempt < 2; attempt++ {
		c, err := a.get()
		if err != nil {
			return err
		}
		err = c.request(typ, payload, out)
		if err == nil {
			return nil
		}
		a.mu.Lock()
		_ = c.close()
		a.c = nil
		a.mu.Unlock()
		if attempt == 1 {
			return err
		}
	}
	return nil
}

// Name is "swayfx" or "sway".
func (a *Adapter) Name() string {
	if a.isFX() {
		return "swayfx"
	}
	return "sway"
}

// Version is the compositor's version string.
func (a *Adapter) Version(context.Context) string {
	a.isFX()
	return a.version
}

// isFX probes once: the version string, then a harmless SwayFX-only
// command (setting the dim of inactive windows to its default of 0 is
// what a fresh SwayFX does anyway).
func (a *Adapter) isFX() bool {
	a.mu.Lock()
	if a.fx != nil {
		v := *a.fx
		a.mu.Unlock()
		return v
	}
	a.mu.Unlock()
	var ver struct {
		HumanReadable string `json:"human_readable"`
	}
	fx := false
	if err := a.do(msgGetVersion, nil, &ver); err == nil {
		a.version = ver.HumanReadable
		if strings.Contains(strings.ToLower(ver.HumanReadable), "swayfx") {
			fx = true
		}
	}
	if !fx {
		if ok, _ := a.command("corner_radius 0"); ok {
			fx = true
			_, _ = a.command("corner_radius 0")
		}
	}
	a.mu.Lock()
	a.fx = &fx
	a.mu.Unlock()
	return fx
}

// Caps of sway and SwayFX.
func (a *Adapter) Caps() compositor.Caps {
	fx := a.isFX()
	return compositor.Caps{
		Floating:        true,
		MoveResize:      true,
		FloatByDefault:  true,
		LiveCorners:     fx,
		LiveShadows:     fx,
		LiveBorders:     true,
		Animations:      false,
		Events:          true,
		WorkspaceByName: true,
	}
}

// command runs a sway command and reports whether every part succeeded.
func (a *Adapter) command(cmd string) (bool, error) {
	var res []commandResult
	if err := a.do(msgRunCommand, []byte(cmd), &res); err != nil {
		return false, err
	}
	for _, r := range res {
		if !r.Success {
			return false, errors.New(r.Error)
		}
	}
	return true, nil
}

func (a *Adapter) run(cmd string) error {
	_, err := a.command(cmd)
	if err != nil {
		return fmt.Errorf("sway: %s: %w", cmd, err)
	}
	return nil
}

// node is the subset of GET_TREE we use.
type node struct {
	ID               int64           `json:"id"`
	Type             string          `json:"type"`
	Name             string          `json:"name"`
	Num              int             `json:"num"`
	AppID            *string         `json:"app_id"`
	PID              int             `json:"pid"`
	Focused          bool            `json:"focused"`
	Shell            string          `json:"shell"`
	Rect             rect            `json:"rect"`
	Window           *int64          `json:"window"`
	WindowProperties *winProps       `json:"window_properties"`
	Nodes            []node          `json:"nodes"`
	FloatingNodes    []node          `json:"floating_nodes"`
	Marks            json.RawMessage `json:"marks"`
}

type winProps struct {
	Class    string `json:"class"`
	Instance string `json:"instance"`
	Title    string `json:"title"`
}

type rect struct {
	X      int `json:"x"`
	Y      int `json:"y"`
	Width  int `json:"width"`
	Height int `json:"height"`
}

func (r rect) toRect() compositor.Rect {
	return compositor.Rect{X: r.X, Y: r.Y, W: r.Width, H: r.Height}
}

func isWindow(n *node) bool {
	return (n.Type == "con" || n.Type == "floating_con") && len(n.Nodes) == 0 &&
		(n.PID > 0 || n.AppID != nil || n.Window != nil)
}

// Windows lists every toplevel with its workspace.
func (a *Adapter) Windows(context.Context) ([]compositor.Window, error) {
	var root node
	if err := a.do(msgGetTree, nil, &root); err != nil {
		return nil, err
	}
	var out []compositor.Window
	var walk func(n *node, ws string, floating bool)
	walk = func(n *node, ws string, floating bool) {
		if n.Type == "workspace" {
			if n.Name == "__i3_scratch" {
				return
			}
			ws = strconv.FormatInt(n.ID, 10)
		}
		if isWindow(n) {
			w := compositor.Window{
				ID:        strconv.FormatInt(n.ID, 10),
				Title:     n.Name,
				PID:       n.PID,
				Workspace: ws,
				Focused:   n.Focused,
				Floating:  floating || n.Type == "floating_con",
				XWayland:  n.Shell == "xwayland",
				Rect:      n.Rect.toRect(),
			}
			if n.AppID != nil {
				w.AppID = *n.AppID
			} else if n.WindowProperties != nil {
				w.AppID = n.WindowProperties.Class
			}
			out = append(out, w)
		}
		for i := range n.Nodes {
			walk(&n.Nodes[i], ws, floating)
		}
		for i := range n.FloatingNodes {
			walk(&n.FloatingNodes[i], ws, true)
		}
	}
	walk(&root, "", false)
	return out, nil
}

// Workspaces lists workspaces with window counts.
func (a *Adapter) Workspaces(ctx context.Context) ([]compositor.Workspace, error) {
	var raw []struct {
		ID      int64  `json:"id"`
		Num     int    `json:"num"`
		Name    string `json:"name"`
		Output  string `json:"output"`
		Focused bool   `json:"focused"`
		Visible bool   `json:"visible"`
	}
	if err := a.do(msgGetWorkspaces, nil, &raw); err != nil {
		return nil, err
	}
	wins, _ := a.Windows(ctx)
	count := map[string]int{}
	for _, w := range wins {
		count[w.Workspace]++
	}
	out := make([]compositor.Workspace, 0, len(raw))
	for _, w := range raw {
		out = append(out, compositor.Workspace{
			ID: strconv.FormatInt(w.ID, 10), Index: w.Num, Name: w.Name, Output: w.Output,
			Focused: w.Focused, Visible: w.Visible, Windows: count[strconv.FormatInt(w.ID, 10)],
		})
	}
	return out, nil
}

// Outputs lists active outputs.
func (a *Adapter) Outputs(context.Context) ([]compositor.Output, error) {
	var raw []struct {
		Name    string  `json:"name"`
		Active  bool    `json:"active"`
		Focused bool    `json:"focused"`
		Scale   float64 `json:"scale"`
		Rect    rect    `json:"rect"`
	}
	if err := a.do(msgGetOutputs, nil, &raw); err != nil {
		return nil, err
	}
	var out []compositor.Output
	for _, o := range raw {
		if !o.Active {
			continue
		}
		out = append(out, compositor.Output{Name: o.Name, Rect: o.Rect.toRect(), Scale: o.Scale, Focused: o.Focused})
	}
	return out, nil
}

func conID(id string) (string, error) {
	if _, err := strconv.ParseInt(id, 10, 64); err != nil {
		return "", fmt.Errorf("invalid window id %q", id)
	}
	return "[con_id=" + id + "] ", nil
}

// Focus focuses a window (switching to its workspace).
func (a *Adapter) Focus(_ context.Context, id string) error {
	c, err := conID(id)
	if err != nil {
		return err
	}
	return a.run(c + "focus")
}

// Close asks a window to close.
func (a *Adapter) Close(_ context.Context, id string) error {
	c, err := conID(id)
	if err != nil {
		return err
	}
	return a.run(c + "kill")
}

// SetFloating floats or tiles a window.
func (a *Adapter) SetFloating(_ context.Context, id string, on bool) error {
	c, err := conID(id)
	if err != nil {
		return err
	}
	if on {
		return a.run(c + "floating enable")
	}
	return a.run(c + "floating disable")
}

// MoveResize floats a window and places it.
func (a *Adapter) MoveResize(_ context.Context, id string, r compositor.Rect) error {
	c, err := conID(id)
	if err != nil {
		return err
	}
	return a.run(fmt.Sprintf("%sfloating enable, resize set width %d px height %d px, move absolute position %d px %d px",
		c, r.W, r.H, r.X, r.Y))
}

func wsRef(ws compositor.Workspace) string {
	if ws.Index > 0 {
		return "number " + strconv.Itoa(ws.Index)
	}
	return quote(ws.Name)
}

// MoveToWorkspace sends a window to a workspace without following it.
func (a *Adapter) MoveToWorkspace(_ context.Context, id string, ws compositor.Workspace) error {
	c, err := conID(id)
	if err != nil {
		return err
	}
	return a.run(c + "move container to workspace " + wsRef(ws))
}

// SwitchWorkspace shows a workspace (created if needed).
func (a *Adapter) SwitchWorkspace(_ context.Context, ws compositor.Workspace) error {
	return a.run("workspace " + wsRef(ws))
}

// quote makes one sway word (double quotes; embedded quotes and
// backslashes dropped: workspace and cursor theme names never need them).
func quote(s string) string {
	s = strings.NewReplacer(`"`, "", `\`, "").Replace(s)
	return `"` + s + `"`
}

// Spawn starts a program through sway's exec (sh -c). Arguments with
// quotes, backslashes or sway's command separators are refused rather
// than escaped: the shell launches applications in their own systemd
// scope (see shell.Launch) and uses this only as a fallback.
func (a *Adapter) Spawn(_ context.Context, argv []string) error {
	if len(argv) == 0 {
		return errors.New("empty command")
	}
	parts := make([]string, len(argv))
	for i, s := range argv {
		if strings.ContainsAny(s, "'\"\\,;\n") {
			return fmt.Errorf("sway exec: unsupported character in argument %q", s)
		}
		parts[i] = "'" + s + "'"
	}
	return a.run("exec " + strings.Join(parts, " "))
}

// ApplyStyle sets borders, gaps and colors; on SwayFX also corners,
// shadows, blur and dimming. Each command runs alone so that one
// unsupported option does not block the others; the first error is
// returned after all were tried.
func (a *Adapter) ApplyStyle(_ context.Context, s compositor.Style) error {
	var cmds []string
	if s.FocusColor != "" {
		fc, ic, uc := s.FocusColor, s.InactiveColor, s.UrgentColor
		if ic == "" {
			ic = fc
		}
		if uc == "" {
			uc = fc
		}
		cmds = append(cmds,
			fmt.Sprintf("client.focused %s %s #ffffff %s %s", fc, fc, fc, fc),
			fmt.Sprintf("client.focused_inactive %s %s #ffffff %s %s", ic, ic, ic, ic),
			fmt.Sprintf("client.unfocused %s %s #ffffff %s %s", ic, ic, ic, ic),
			fmt.Sprintf("client.urgent %s %s #ffffff %s %s", uc, uc, uc, uc),
		)
	}
	cmds = append(cmds,
		fmt.Sprintf("default_border pixel %d", s.BorderWidth),
		fmt.Sprintf("default_floating_border pixel %d", s.BorderWidth),
		fmt.Sprintf("[all] border pixel %d", s.BorderWidth),
		fmt.Sprintf("gaps inner all set %d", s.Gaps),
	)
	if s.CursorTheme != "" {
		size := s.CursorSize
		if size <= 0 {
			size = 24
		}
		cmds = append(cmds, fmt.Sprintf("seat * xcursor_theme %s %d", quote(s.CursorTheme), size))
	}
	if a.isFX() {
		onoff := func(b bool) string {
			if b {
				return "enable"
			}
			return "disable"
		}
		cmds = append(cmds,
			fmt.Sprintf("corner_radius %d", s.CornerRadius),
			"shadows "+onoff(s.Shadows),
			"blur "+onoff(s.Blur),
			fmt.Sprintf("default_dim_inactive %.2f", s.DimInactive),
		)
		if s.ShadowColor != "" {
			cmds = append(cmds, "shadow_color "+s.ShadowColor)
		}
		if s.ShadowBlur > 0 {
			cmds = append(cmds, fmt.Sprintf("shadow_blur_radius %d", s.ShadowBlur))
		}
	}
	var first error
	for _, c := range cmds {
		// "[all] ..." with no windows yet is not an error.
		if err := a.run(c); err != nil && first == nil && !strings.Contains(err.Error(), "No matching node") {
			first = err
		}
	}
	return first
}

// Subscribe opens a second connection for window, workspace and output
// events.
func (a *Adapter) Subscribe(ctx context.Context) (<-chan compositor.Event, error) {
	c, err := dial(a.path)
	if err != nil {
		return nil, err
	}
	if err := c.write(msgSubscribe, []byte(`["window","workspace","output"]`)); err != nil {
		_ = c.close()
		return nil, err
	}
	if _, body, err := c.read(); err != nil {
		_ = c.close()
		return nil, err
	} else {
		var ok struct {
			Success bool `json:"success"`
		}
		if json.Unmarshal(body, &ok) != nil || !ok.Success {
			_ = c.close()
			return nil, errors.New("sway: subscribe refused")
		}
	}
	ch := make(chan compositor.Event, 16)
	go func() { <-ctx.Done(); _ = c.close() }()
	go func() {
		defer close(ch)
		for {
			typ, _, err := c.read()
			if err != nil {
				return
			}
			var kind string
			switch typ &^ 0x80000000 {
			case 0:
				kind = "workspaces"
			case 1:
				kind = "outputs"
			case 3:
				kind = "windows"
			default:
				continue
			}
			select {
			case ch <- compositor.Event{Kind: kind}:
			default: // consumer is behind; it refetches anyway
			}
		}
	}()
	return ch, nil
}
