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
	"time"

	"github.com/basalt-os/basalt-shell/internal/compositor"
	"github.com/basalt-os/basalt-shell/internal/decor"
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

	// Windows known to draw their own decorations. sway reports border
	// "csd" only while such a window floats; tiled, it shows the border
	// it would return to, so the adapter remembers them (to never force
	// a server-side border on them when the style changes).
	csd map[int64]bool
	// border is the frame width of the last applied style.
	border int
	// WantCSD decides whether a new window should be asked to draw its
	// own decorations (default: decor.WantClientSide).
	WantCSD func(pid int) (bool, string)
}

// New returns an adapter for the socket at path.
func New(path string) *Adapter {
	return &Adapter{path: path, csd: map[int64]bool{}, border: -1,
		WantCSD: func(pid int) (bool, string) { return decor.WantClientSide("/", pid) }}
}

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
		Pointer:         true,
		ToplevelCapture: a.hasForeignIDs(),
		Minimize:        true,
		ClientMaximize:  false,
		TitleBars:       true,
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
	Border           string          `json:"border"`
	Rect             rect            `json:"rect"`
	DecoRect         rect            `json:"deco_rect"`
	Window           *int64          `json:"window"`
	WindowProperties *winProps       `json:"window_properties"`
	Nodes            []node          `json:"nodes"`
	FloatingNodes    []node          `json:"floating_nodes"`
	ForeignID        string          `json:"foreign_toplevel_identifier"`
	FullscreenMode   int             `json:"fullscreen_mode"`
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
	hidden := false
	a.mu.Lock()
	defer a.mu.Unlock()
	var walk func(n *node, ws string, floating bool)
	walk = func(n *node, ws string, floating bool) {
		if n.Type == "workspace" {
			// Minimized windows live in sway's scratchpad.
			hidden = n.Name == "__i3_scratch"
			ws = strconv.FormatInt(n.ID, 10)
			if hidden {
				ws = ""
			}
		}
		if isWindow(n) {
			if n.Border == "csd" {
				a.csd[n.ID] = true
			}
			w := compositor.Window{
				ID:         strconv.FormatInt(n.ID, 10),
				Title:      n.Name,
				PID:        n.PID,
				Workspace:  ws,
				Focused:    n.Focused,
				Floating:   floating || n.Type == "floating_con",
				XWayland:   n.Shell == "xwayland",
				Rect:       outer(n),
				ForeignID:  n.ForeignID,
				Decoration: decoration(n.Border, a.csd[n.ID]),
			}
			if hidden {
				w.State = "minimized"
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

// outer is a window's whole frame: sway reports the title bar apart
// (deco_rect) from the rest; MoveResize places the whole frame.
func outer(n *node) compositor.Rect {
	r := n.Rect.toRect()
	if h := n.DecoRect.Height; h > 0 && n.Border == "normal" {
		r.Y -= h
		r.H += h
	}
	return r
}

func decoration(border string, csd bool) string {
	switch {
	case border == "csd" || csd:
		return "client"
	case border == "normal":
		return "server"
	}
	return "none"
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
		Rect    rect   `json:"rect"`
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
			Rect: w.Rect.toRect(),
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
// than escaped: the shell launches applications as systemd user
// services (see shell.Launch) and uses this only as a fallback.
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

// ApplyStyle sets title bars, borders, gaps and colors; on SwayFX also
// corners, shadows, blur and dimming. Each command runs alone so that one
// unsupported option does not block the others; the first error is
// returned after all were tried.
//
// Windows that draw their own decorations keep them: their border is
// never set here (a "border" command on such a window would switch it
// to server-side decorations).
func (a *Adapter) ApplyStyle(ctx context.Context, s compositor.Style) error {
	var cmds []string
	t := s.Title
	if t.FocusedBg == "" {
		// No title colors: the frame in the focus color, as before.
		t.FocusedBg, t.InactiveBg, t.UrgentBg = s.FocusColor, s.InactiveColor, s.UrgentColor
		t.FocusedText, t.InactiveTxt, t.UrgentText = "#ffffff", "#ffffff", "#ffffff"
	}
	frameF, frameI := s.FocusColor, s.InactiveColor
	if frameF == "" {
		frameF = t.FocusedBg
	}
	if frameI == "" {
		frameI = t.InactiveBg
	}
	if t.InactiveBg == "" {
		t.InactiveBg, t.InactiveTxt = t.FocusedBg, t.FocusedText
	}
	if t.UrgentBg == "" {
		t.UrgentBg, t.UrgentText = t.FocusedBg, t.FocusedText
	}
	if t.FocusedBg != "" {
		// client.<class> border background text indicator child_border:
		// the title bar outline and the frame share one color, so title
		// and frame read as one window edge.
		cmds = append(cmds,
			fmt.Sprintf("client.focused %s %s %s %s %s", frameF, t.FocusedBg, t.FocusedText, frameF, frameF),
			fmt.Sprintf("client.focused_inactive %s %s %s %s %s", frameI, t.InactiveBg, t.InactiveTxt, frameI, frameI),
			fmt.Sprintf("client.unfocused %s %s %s %s %s", frameI, t.InactiveBg, t.InactiveTxt, frameI, frameI),
			fmt.Sprintf("client.urgent %s %s %s %s %s", t.UrgentBg, t.UrgentBg, t.UrgentText, t.UrgentBg, t.UrgentBg),
		)
	}
	if t.Font != "" {
		size := t.Size
		if size <= 0 {
			size = 10
		}
		cmds = append(cmds, fmt.Sprintf("font pango:%s %s", fontWord(t.Font), strconv.FormatFloat(size, 'f', -1, 64)))
	}
	if t.Align == "left" || t.Align == "center" || t.Align == "right" {
		cmds = append(cmds, "title_align "+t.Align)
	}
	if t.PadX > 0 || t.PadY > 0 {
		cmds = append(cmds, fmt.Sprintf("titlebar_padding %d %d", max(t.PadX, 1), max(t.PadY, 1)))
	}
	bw := s.BorderWidth
	cmds = append(cmds,
		fmt.Sprintf("titlebar_border_thickness %d", min(bw, 1)),
		fmt.Sprintf("default_border normal %d", bw),
		fmt.Sprintf("default_floating_border normal %d", bw),
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
			// Most apps draw their own title bar (CSD); without this
			// SwayFX gives only server-decorated windows a shadow.
			"shadows_on_csd "+onoff(s.Shadows),
			"blur "+onoff(s.Blur),
			fmt.Sprintf("default_dim_inactive %.2f", s.DimInactive),
			// default_dim_inactive only reaches new windows; set the
			// open ones too, so a theme change applies everywhere.
			fmt.Sprintf("[all] dim_inactive %.2f", s.DimInactive),
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
		if err := a.run(c); err != nil && first == nil {
			first = err
		}
	}
	// Existing windows with a compositor frame follow the new width.
	a.mu.Lock()
	changed := a.border != bw
	a.border = bw
	a.mu.Unlock()
	if changed {
		wins, _ := a.Windows(ctx)
		for _, w := range wins {
			if w.Decoration != "server" && w.Decoration != "none" {
				continue
			}
			if err := a.run(fmt.Sprintf("[con_id=%s] border normal %d", w.ID, bw)); err != nil && first == nil {
				first = err
			}
		}
	}
	return first
}

// fontWord keeps a font family usable in a Pango description inside a
// sway command (no separators or quotes).
func fontWord(f string) string {
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune(",;\"'\\\n", r) {
			return -1
		}
		return r
	}, f)
}

// SetVoiceKeys switches to the shell's "basalt-voice" binding mode (Escape
// cancels, Super+V stops) while the microphone is open in "press to start
// and stop", and back to the default mode. A sway config without that
// mode makes it fail, and the voice card then offers its Cancel button only.
func (a *Adapter) SetVoiceKeys(_ context.Context, on bool) error {
	if on {
		return a.run(`mode "basalt-voice"`)
	}
	return a.run("mode default")
}

// Minimize hides a window in sway's scratchpad.
func (a *Adapter) Minimize(_ context.Context, id string) error {
	c, err := conID(id)
	if err != nil {
		return err
	}
	return a.run(c + "move scratchpad")
}

// Unminimize shows a minimized window on the current workspace (at its
// old position when it fits) and focuses it.
func (a *Adapter) Unminimize(_ context.Context, id string) error {
	c, err := conID(id)
	if err != nil {
		return err
	}
	return a.run(c + "scratchpad show")
}

// decorate applies the decoration rule to a new window: a toolkit that
// draws good client-side decorations but asks for server-side ones (Qt
// with the Adwaita plugin) is switched to client-side.
func (a *Adapter) decorate(id int64, pid int, shell string) {
	if shell != "xdg_shell" || a.WantCSD == nil {
		return
	}
	ok, _ := a.WantCSD(pid)
	if !ok {
		return
	}
	if err := a.run(fmt.Sprintf("[con_id=%d] border csd", id)); err == nil {
		a.mu.Lock()
		a.csd[id] = true
		a.mu.Unlock()
	}
}

// hasForeignIDs reports whether this sway puts foreign-toplevel
// identifiers in its tree (1.11 and newer).
func (a *Adapter) hasForeignIDs() bool {
	a.mu.Lock()
	v := a.version
	a.mu.Unlock()
	var maj, min int
	if _, err := fmt.Sscanf(strings.TrimPrefix(v, "sway version "), "%d.%d", &maj, &min); err != nil {
		return false
	}
	return maj > 1 || (maj == 1 && min >= 11)
}

var buttons = map[string]string{"left": "button1", "middle": "button2", "right": "button3"}

// PointerMove warps the pointer to a layout position (sway's seat cursor
// command, which injects the motion like a real device would).
func (a *Adapter) PointerMove(_ context.Context, x, y int) error {
	return a.run(fmt.Sprintf("seat seat0 cursor set %d %d", x, y))
}

// PointerButton presses, releases or clicks a button.
func (a *Adapter) PointerButton(_ context.Context, button, action string) error {
	b, ok := buttons[button]
	if !ok {
		return fmt.Errorf("unknown button %q (left, middle, right)", button)
	}
	switch action {
	case "press", "release":
		return a.run("seat seat0 cursor " + action + " " + b)
	case "click", "":
		if err := a.run("seat seat0 cursor press " + b); err != nil {
			return err
		}
		return a.run("seat seat0 cursor release " + b)
	}
	return fmt.Errorf("unknown pointer action %q", action)
}

// PointerScroll scrolls with sway's axis buttons (4 up, 5 down, 6 left,
// 7 right), one step per unit.
func (a *Adapter) PointerScroll(_ context.Context, dx, dy int) error {
	step := func(n int, neg, pos string) error {
		b := pos
		if n < 0 {
			b, n = neg, -n
		}
		for i := 0; i < n && i < 50; i++ {
			if err := a.run("seat seat0 cursor press " + b); err != nil {
				return err
			}
			if err := a.run("seat seat0 cursor release " + b); err != nil {
				return err
			}
		}
		return nil
	}
	if err := step(dy, "button4", "button5"); err != nil {
		return err
	}
	return step(dx, "button6", "button7")
}

// Subscribe opens a second connection for window, workspace and output
// events.
func (a *Adapter) Subscribe(ctx context.Context) (<-chan compositor.Event, error) {
	c, err := dial(a.path)
	if err != nil {
		return nil, err
	}
	if err := c.write(msgSubscribe, []byte(`["window","workspace","output","input"]`)); err != nil {
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
			typ, body, err := c.read()
			if err != nil {
				return
			}
			if typ&^0x80000000 == 3 {
				a.windowEvent(body)
			}
			var kind string
			switch typ &^ 0x80000000 {
			case 0:
				kind = "workspaces"
			case 1:
				kind = "outputs"
			case 3:
				kind = "windows"
			case evInput:
				// A keyboard's layouts or its active layout changed, or a
				// keyboard came or went (the panel's indicator follows).
				if !keyboardEvent(body) {
					continue
				}
				kind = "keyboard"
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

// windowEvent handles the window events the adapter acts on itself: the
// decoration rule for new windows and forgetting closed ones.
func (a *Adapter) windowEvent(body []byte) {
	var ev struct {
		Change    string `json:"change"`
		Container node   `json:"container"`
	}
	if json.Unmarshal(body, &ev) != nil {
		return
	}
	switch ev.Change {
	case "new":
		n := ev.Container
		// Off the event reader: the decision reads /proc and runs a command.
		go a.decorate(n.ID, n.PID, n.Shell)
		go a.keepInside(n.ID)
	case "close":
		a.mu.Lock()
		delete(a.csd, ev.Container.ID)
		a.mu.Unlock()
	}
}

// fitDelays are when a new floating window is checked against the
// workspace's usable area: at once, and again after X11 apps had time to
// resize themselves (JetBrains IDEs map a small frame, then ask for the
// size they remember, which sway centers on the whole output).
var fitDelays = []time.Duration{0, 400 * time.Millisecond, 1500 * time.Millisecond, 4 * time.Second}

// keepInside keeps a new floating window inside the usable area of its
// workspace, so its title bar never ends up under the panel. sway
// centers a floating window on the output, ignoring the panel's
// exclusive zone, and its maximum floating size is the whole output: a
// window as tall as the screen (IntelliJ IDEA restoring its size, an
// X11 app asking for position 0,0) had its toolbar hidden behind the
// shell's top bar. Only windows that do not fit are touched, and only
// in their first seconds; fullscreen windows are left alone.
func (a *Adapter) keepInside(id int64) {
	for _, d := range fitDelays {
		time.Sleep(d)
		win, area, ok := a.floatingPlace(id)
		if !ok {
			return
		}
		r, changed := fitRect(win, area)
		if !changed {
			continue
		}
		_ = a.MoveResize(context.Background(), strconv.FormatInt(id, 10), r)
	}
}

// floatingPlace finds a floating, non-fullscreen window's frame and the
// usable area of its workspace (sway's workspace rect excludes the
// panels' exclusive zones). ok is false when the window is gone, tiled,
// fullscreen or minimized.
func (a *Adapter) floatingPlace(id int64) (win, area compositor.Rect, ok bool) {
	var root node
	if err := a.do(msgGetTree, nil, &root); err != nil {
		return win, area, false
	}
	var find func(n *node, ws *node) bool
	find = func(n *node, ws *node) bool {
		if n.Type == "workspace" {
			if n.Name == "__i3_scratch" {
				return false
			}
			ws = n
		}
		for i := range n.FloatingNodes {
			f := &n.FloatingNodes[i]
			if f.ID == id && ws != nil {
				if f.FullscreenMode != 0 {
					return false
				}
				win, area, ok = outer(f), ws.Rect.toRect(), true
				return true
			}
			if find(f, ws) {
				return true
			}
		}
		for i := range n.Nodes {
			if find(&n.Nodes[i], ws) {
				return true
			}
		}
		return false
	}
	find(&root, nil)
	return win, area, ok
}

// fitRect moves a window frame into area, shrinking it first when it is
// larger. It reports whether anything changed.
func fitRect(w, area compositor.Rect) (compositor.Rect, bool) {
	if area.W <= 0 || area.H <= 0 || w.W <= 0 || w.H <= 0 {
		return w, false
	}
	r := w
	if r.W > area.W {
		r.W = area.W
	}
	if r.H > area.H {
		r.H = area.H
	}
	if r.X < area.X {
		r.X = area.X
	}
	if r.Y < area.Y {
		r.Y = area.Y
	}
	if r.X+r.W > area.X+area.W {
		r.X = area.X + area.W - r.W
	}
	if r.Y+r.H > area.Y+area.H {
		r.Y = area.Y + area.H - r.H
	}
	return r, r != w
}
