package shell

import (
	"context"
	"errors"
	"fmt"
	"math"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/basalt-os/basalt-shell/internal/apps"
	"github.com/basalt-os/basalt-shell/internal/compositor"
	"github.com/basalt-os/basalt-shell/internal/theme"
)

// Param describes one action parameter (rendered as JSON Schema for MCP).
type Param struct {
	Name        string   `json:"name"`
	Type        string   `json:"type"` // string, integer, number, boolean, object, array
	Description string   `json:"description"`
	Enum        []string `json:"enum,omitempty"`
	Required    bool     `json:"required,omitempty"`
}

// ActionDef is one typed action of the closed set. Nothing outside this
// set can be asked of the desktop: an agent, the command bar and the UI
// all go through the same definitions and validators.
type ActionDef struct {
	Name        string  `json:"name"`
	Title       string  `json:"title"`
	Description string  `json:"description"`
	Params      []Param `json:"params"`
	// Agent marks actions only an agent connection may propose (control
	// sessions, screenshots); the command bar never produces them.
	Agent bool `json:"agent,omitempty"`
	plan  func(ctx context.Context, p *planner, args map[string]any) (step, error)
}

// step is one planned action: a human summary and, for actions that are
// not theme changes, a function that performs it. Theme actions change
// the planner's working settings instead; the proposal commits them once.
type step struct {
	Summary string
	run     func(ctx context.Context) (any, error)
}

// planner carries what planning needs and the working theme settings,
// so that several theme actions in one proposal compose.
type planner struct {
	c        *Core
	windows  []compositor.Window
	spaces   []compositor.Workspace
	outputs  []compositor.Output
	settings theme.Settings
	touched  bool // settings changed
	meta     Meta // who asks
}

func (p *planner) window(ref any) (compositor.Window, error) {
	s := fmt.Sprint(ref)
	if s == "" || s == "<nil>" {
		return compositor.Window{}, errors.New("window: missing")
	}
	if s == "focused" {
		for _, w := range p.windows {
			if w.Focused {
				return w, nil
			}
		}
		return compositor.Window{}, errors.New("no focused window")
	}
	for _, w := range p.windows {
		if w.ID == s {
			return w, nil
		}
	}
	// Then by app id or title, case-insensitive.
	ls := strings.ToLower(s)
	for _, w := range p.windows {
		if strings.ToLower(w.AppID) == ls {
			return w, nil
		}
	}
	for _, w := range p.windows {
		if strings.Contains(strings.ToLower(w.AppID), ls) || strings.Contains(strings.ToLower(w.Title), ls) {
			return w, nil
		}
	}
	return compositor.Window{}, fmt.Errorf("no window matches %q", s)
}

func (p *planner) workspace(ref any) (compositor.Workspace, error) {
	s := strings.TrimSpace(fmt.Sprint(ref))
	if w, ok := compositor.FindWorkspace(p.spaces, s); ok {
		return w, nil
	}
	// A new numbered workspace is fine on sway (created on demand).
	if n, err := strconv.Atoi(s); err == nil && n > 0 && n < 100 {
		return compositor.Workspace{Index: n, Name: s}, nil
	}
	return compositor.Workspace{}, fmt.Errorf("no workspace %q", s)
}

func label(w compositor.Window) string {
	name := w.AppID
	if name == "" {
		name = "window"
	}
	t := w.Title
	if len(t) > 40 {
		t = t[:40]
	}
	if t != "" && t != name {
		return fmt.Sprintf("%s \"%s\" (id %s)", name, t, w.ID)
	}
	return fmt.Sprintf("%s (id %s)", name, w.ID)
}

func argInt(args map[string]any, k string) (int, bool) {
	switch v := args[k].(type) {
	case float64:
		return int(math.Round(v)), true
	case int:
		return v, true
	case string:
		n, err := strconv.Atoi(v)
		return n, err == nil
	}
	return 0, false
}

func argStr(args map[string]any, k string) string {
	if v, ok := args[k]; ok && v != nil {
		return strings.TrimSpace(fmt.Sprint(v))
	}
	return ""
}

func argBool(args map[string]any, k string) (bool, bool) {
	switch v := args[k].(type) {
	case bool:
		return v, true
	case string:
		switch strings.ToLower(v) {
		case "true", "yes", "on":
			return true, true
		case "false", "no", "off":
			return false, true
		}
	}
	return false, false
}

func oneOf(v string, opts ...string) bool {
	for _, o := range opts {
		if v == o {
			return true
		}
	}
	return false
}

var windowStates = []string{"normal", "minimized", "maximized", "left", "right"}

var layouts = []string{"grid", "columns", "rows", "cascade", "center", "tile", "float"}
var surfaces = []string{"launcher", "commandbar", "quicksettings", "activity", "notifications", "settings"}
var pages = []string{"appearance", "tokens", "motion", "panel", "windows", "apps", "ai", "about"}

// Actions is the closed set, in display order.
var Actions = []*ActionDef{
	{
		Name: "window.focus", Title: "Focus a window",
		Description: "Bring a window to the front and focus it (switches to its workspace).",
		Params:      []Param{{Name: "window", Type: "string", Required: true, Description: "window id from desktop_state, or an app id / title fragment, or \"focused\""}},
		plan: func(ctx context.Context, p *planner, a map[string]any) (step, error) {
			w, err := p.window(a["window"])
			if err != nil {
				return step{}, err
			}
			return step{Summary: "Focus " + label(w), run: func(ctx context.Context) (any, error) {
				return nil, p.c.Comp.Focus(ctx, w.ID)
			}}, nil
		},
	},
	{
		Name: "window.close", Title: "Close a window",
		Description: "Ask a window to close (the app may ask to save first).",
		Params:      []Param{{Name: "window", Type: "string", Required: true, Description: "window id, app id, title fragment or \"focused\""}},
		plan: func(ctx context.Context, p *planner, a map[string]any) (step, error) {
			w, err := p.window(a["window"])
			if err != nil {
				return step{}, err
			}
			return step{Summary: "Close " + label(w), run: func(ctx context.Context) (any, error) {
				return nil, p.c.Comp.Close(ctx, w.ID)
			}}, nil
		},
	},
	{
		Name: "window.move", Title: "Move and resize a window",
		Description: "Float a window and place it at x, y with a size, in global logical pixels (see outputs in desktop_state).",
		Params: []Param{
			{Name: "window", Type: "string", Required: true, Description: "window id, app id, title fragment or \"focused\""},
			{Name: "x", Type: "integer", Required: true, Description: "left edge"},
			{Name: "y", Type: "integer", Required: true, Description: "top edge"},
			{Name: "width", Type: "integer", Description: "width (default: current)"},
			{Name: "height", Type: "integer", Description: "height (default: current)"},
		},
		plan: func(ctx context.Context, p *planner, a map[string]any) (step, error) {
			w, err := p.window(a["window"])
			if err != nil {
				return step{}, err
			}
			x, okx := argInt(a, "x")
			y, oky := argInt(a, "y")
			if !okx || !oky {
				return step{}, errors.New("x and y are required integers")
			}
			wd, ok := argInt(a, "width")
			if !ok || wd <= 0 {
				wd = w.Rect.W
			}
			ht, ok := argInt(a, "height")
			if !ok || ht <= 0 {
				ht = w.Rect.H
			}
			if wd < 120 || ht < 80 || wd > 16384 || ht > 16384 {
				return step{}, fmt.Errorf("size %dx%d is out of range", wd, ht)
			}
			r := compositor.Rect{X: x, Y: y, W: wd, H: ht}
			return step{Summary: fmt.Sprintf("Move %s to %d,%d size %dx%d", label(w), x, y, wd, ht), run: func(ctx context.Context) (any, error) {
				return nil, p.c.Comp.MoveResize(ctx, w.ID, r)
			}}, nil
		},
	},
	{
		Name: "window.set_floating", Title: "Float or tile a window",
		Description: "Make a window floating (free placement) or tiled.",
		Params: []Param{
			{Name: "window", Type: "string", Required: true, Description: "window id, app id, title fragment or \"focused\""},
			{Name: "floating", Type: "boolean", Required: true, Description: "true to float, false to tile"},
		},
		plan: func(ctx context.Context, p *planner, a map[string]any) (step, error) {
			w, err := p.window(a["window"])
			if err != nil {
				return step{}, err
			}
			on, ok := argBool(a, "floating")
			if !ok {
				return step{}, errors.New("floating must be true or false")
			}
			verb := "Tile "
			if on {
				verb = "Float "
			}
			return step{Summary: verb + label(w), run: func(ctx context.Context) (any, error) {
				return nil, p.c.Comp.SetFloating(ctx, w.ID, on)
			}}, nil
		},
	},
	{
		Name: "window.set_state", Title: "Minimize, maximize, snap or restore a window",
		Description: "Minimize a window (hidden; the panel's window list brings it back), maximize it to the usable area, snap it to the left or right half, or restore it (normal: back from minimized, or to its size before maximize or snap).",
		Params: []Param{
			{Name: "window", Type: "string", Required: true, Description: "window id, app id, title fragment or \"focused\""},
			{Name: "state", Type: "string", Required: true, Enum: windowStates, Description: "normal, minimized, maximized, left or right"},
		},
		plan: planSetState,
	},
	{
		Name: "window.to_workspace", Title: "Send a window to a workspace",
		Description: "Move a window to another workspace without following it.",
		Params: []Param{
			{Name: "window", Type: "string", Required: true, Description: "window id, app id, title fragment or \"focused\""},
			{Name: "workspace", Type: "string", Required: true, Description: "workspace number, id or name"},
		},
		plan: func(ctx context.Context, p *planner, a map[string]any) (step, error) {
			w, err := p.window(a["window"])
			if err != nil {
				return step{}, err
			}
			ws, err := p.workspace(a["workspace"])
			if err != nil {
				return step{}, err
			}
			return step{Summary: fmt.Sprintf("Send %s to workspace %s", label(w), ws.Name), run: func(ctx context.Context) (any, error) {
				return nil, p.c.Comp.MoveToWorkspace(ctx, w.ID, ws)
			}}, nil
		},
	},
	{
		Name: "windows.arrange", Title: "Arrange windows",
		Description: "Arrange the windows of a workspace: grid, columns, rows, cascade, center (floating placements), tile (hand them to the compositor's tiling) or float (all floating, cascaded).",
		Params: []Param{
			{Name: "layout", Type: "string", Required: true, Enum: layouts, Description: "layout"},
			{Name: "workspace", Type: "string", Description: "workspace (default: the focused one)"},
		},
		plan: planArrange,
	},
	{
		Name: "workspace.switch", Title: "Switch workspace",
		Description: "Show a workspace.",
		Params:      []Param{{Name: "workspace", Type: "string", Required: true, Description: "workspace number, id or name"}},
		plan: func(ctx context.Context, p *planner, a map[string]any) (step, error) {
			ws, err := p.workspace(a["workspace"])
			if err != nil {
				return step{}, err
			}
			return step{Summary: "Switch to workspace " + ws.Name, run: func(ctx context.Context) (any, error) {
				return nil, p.c.Comp.SwitchWorkspace(ctx, ws)
			}}, nil
		},
	},
	{
		Name: "app.launch", Title: "Launch an application",
		Description: "Start an installed application by desktop id or name (see apps_list). Only installed desktop entries can be started; there is no free-form command.",
		Params:      []Param{{Name: "app", Type: "string", Required: true, Description: "desktop id (org.gnome.TextEditor) or name (Text Editor)"}},
		plan: func(ctx context.Context, p *planner, a map[string]any) (step, error) {
			ref := argStr(a, "app")
			app, ok := apps.Find(p.c.Apps(), ref)
			if !ok {
				return step{}, fmt.Errorf("no installed application matches %q", ref)
			}
			argv, err := app.Argv()
			if err != nil {
				return step{}, err
			}
			return step{Summary: fmt.Sprintf("Launch %s (%s)", app.Name, app.ID), run: func(ctx context.Context) (any, error) {
				return map[string]any{"app": app.ID}, p.c.Launch(ctx, app.ID, argv)
			}}, nil
		},
	},
	{
		Name: "theme.set_tokens", Title: "Change theme tokens",
		Description: "Set one or more design tokens (see theme_get for keys, ranges and current values). Color tokens apply to the current mode unless mode is given.",
		Params: []Param{
			{Name: "tokens", Type: "object", Required: true, Description: "map of token key to value, e.g. {\"radius.md\": 14, \"color.accent\": \"#2f7d6d\"}"},
			{Name: "mode", Type: "string", Enum: []string{"current", "light", "dark"}, Description: "which mode color tokens apply to (default current)"},
		},
		plan: func(ctx context.Context, p *planner, a map[string]any) (step, error) {
			tok, ok := a["tokens"].(map[string]any)
			if !ok || len(tok) == 0 {
				return step{}, errors.New("tokens must be a non-empty object")
			}
			mode := argStr(a, "mode")
			if mode == "" || mode == "current" {
				mode = p.settings.Mode
			}
			if !oneOf(mode, "light", "dark") {
				return step{}, errors.New("mode must be current, light or dark")
			}
			keys := make([]string, 0, len(tok))
			for k := range tok {
				keys = append(keys, k)
			}
			sort.Strings(keys)
			for _, k := range keys {
				spec, ok := theme.Lookup(k)
				if !ok {
					return step{}, fmt.Errorf("unknown token %q", k)
				}
				v, err := theme.Normalize(k, tok[k])
				if err != nil {
					return step{}, err
				}
				p.setToken(spec, mode, v)
			}
			return step{Summary: "Set " + strings.Join(keys, ", ")}, nil
		},
	},
	{
		Name: "theme.switch", Title: "Switch theme or mode",
		Description: "Switch to another theme and/or between light and dark mode. A new theme starts from its own values (the person's token overrides are dropped unless keep_overrides is true); a mode switch keeps them.",
		Params: []Param{
			{Name: "theme", Type: "string", Description: "theme id (see theme_get)"},
			{Name: "mode", Type: "string", Enum: []string{"light", "dark", "toggle"}, Description: "mode"},
			{Name: "keep_overrides", Type: "boolean", Description: "keep the person's token overrides when switching theme"},
		},
		plan: func(ctx context.Context, p *planner, a map[string]any) (step, error) {
			id, mode := argStr(a, "theme"), argStr(a, "mode")
			if id == "" && mode == "" {
				return step{}, errors.New("give a theme, a mode or both")
			}
			var parts []string
			if id != "" {
				if _, ok := p.c.Themes.Theme(id); !ok {
					return step{}, fmt.Errorf("unknown theme %q", id)
				}
				if id != p.settings.Theme {
					if keep, _ := argBool(a, "keep_overrides"); !keep {
						p.settings.Overrides, p.settings.Light, p.settings.Dark = nil, nil, nil
					}
				}
				p.settings.Theme = id
				parts = append(parts, "theme "+id)
			}
			switch mode {
			case "":
			case "toggle":
				if p.settings.Mode == "dark" {
					p.settings.Mode = "light"
				} else {
					p.settings.Mode = "dark"
				}
				parts = append(parts, p.settings.Mode+" mode")
			case "light", "dark":
				p.settings.Mode = mode
				parts = append(parts, mode+" mode")
			default:
				return step{}, errors.New("mode must be light, dark or toggle")
			}
			p.touched = true
			return step{Summary: "Switch to " + strings.Join(parts, ", ")}, nil
		},
	},
	{
		Name: "theme.reset", Title: "Reset tokens",
		Description: "Drop the user's overrides (all, or the listed token keys) so the theme's values apply again.",
		Params:      []Param{{Name: "tokens", Type: "array", Description: "token keys to reset (default: all)"}},
		plan: func(ctx context.Context, p *planner, a map[string]any) (step, error) {
			list, _ := a["tokens"].([]any)
			if len(list) == 0 {
				p.settings.Overrides, p.settings.Light, p.settings.Dark = nil, nil, nil
				p.touched = true
				return step{Summary: "Reset every token to the theme's values"}, nil
			}
			var keys []string
			for _, x := range list {
				k := fmt.Sprint(x)
				if _, ok := theme.Lookup(k); !ok {
					return step{}, fmt.Errorf("unknown token %q", k)
				}
				delete(p.settings.Overrides, k)
				delete(p.settings.Light, k)
				delete(p.settings.Dark, k)
				keys = append(keys, k)
			}
			p.touched = true
			return step{Summary: "Reset " + strings.Join(keys, ", ")}, nil
		},
	},
	{
		Name: "motion.set", Title: "Set motion",
		Description: "Animations: auto (off on weak hardware), full, or reduced (no animations).",
		Params:      []Param{{Name: "motion", Type: "string", Required: true, Enum: []string{"auto", "full", "reduced"}, Description: "motion preference"}},
		plan: func(ctx context.Context, p *planner, a map[string]any) (step, error) {
			m := argStr(a, "motion")
			if !oneOf(m, "auto", "full", "reduced") {
				return step{}, errors.New("motion must be auto, full or reduced")
			}
			p.settings.Motion = m
			p.touched = true
			return step{Summary: "Motion: " + m}, nil
		},
	},
	{
		Name: "notification.show", Title: "Show a notification",
		Description: "Show a desktop notification from the assistant.",
		Params: []Param{
			{Name: "summary", Type: "string", Required: true, Description: "one line"},
			{Name: "body", Type: "string", Description: "details"},
			{Name: "urgency", Type: "string", Enum: []string{"low", "normal", "critical"}, Description: "urgency (default normal)"},
		},
		plan: func(ctx context.Context, p *planner, a map[string]any) (step, error) {
			sum, body, urg := argStr(a, "summary"), argStr(a, "body"), argStr(a, "urgency")
			if sum == "" || len(sum) > 200 || len(body) > 2000 {
				return step{}, errors.New("summary is required (at most 200 characters), body at most 2000")
			}
			if urg == "" {
				urg = "normal"
			}
			if !oneOf(urg, "low", "normal", "critical") {
				return step{}, errors.New("urgency must be low, normal or critical")
			}
			return step{Summary: "Notify: " + sum, run: func(ctx context.Context) (any, error) {
				p.c.Broadcast("notify", map[string]any{"summary": sum, "body": body, "urgency": urg, "app": "Basalt assistant"})
				return nil, nil
			}}, nil
		},
	},
	{
		Name: "settings.open", Title: "Open a settings page",
		Description: "Open the shell's settings on a page.",
		Params:      []Param{{Name: "page", Type: "string", Enum: pages, Description: "page (default appearance)"}},
		plan: func(ctx context.Context, p *planner, a map[string]any) (step, error) {
			pg := argStr(a, "page")
			if pg == "" {
				pg = "appearance"
			}
			if !oneOf(pg, pages...) {
				return step{}, fmt.Errorf("unknown page %q", pg)
			}
			return step{Summary: "Open settings: " + pg, run: func(ctx context.Context) (any, error) {
				p.c.Broadcast("ui", map[string]any{"open": "settings", "page": pg})
				return nil, nil
			}}, nil
		},
	},
	{
		Name: "shell.open", Title: "Open a shell surface",
		Description: "Open the launcher, command bar, quick settings, activity feed or notification center.",
		Params:      []Param{{Name: "surface", Type: "string", Required: true, Enum: surfaces, Description: "surface"}},
		plan: func(ctx context.Context, p *planner, a map[string]any) (step, error) {
			s := argStr(a, "surface")
			if !oneOf(s, surfaces...) {
				return step{}, fmt.Errorf("unknown surface %q", s)
			}
			return step{Summary: "Open " + s, run: func(ctx context.Context) (any, error) {
				p.c.Broadcast("ui", map[string]any{"open": s})
				return nil, nil
			}}, nil
		},
	},
}

// ActionByName finds a definition.
func ActionByName(name string) (*ActionDef, bool) {
	for _, a := range Actions {
		if a.Name == name {
			return a, true
		}
	}
	return nil, false
}

// setToken writes a token into the working settings as an override.
func (p *planner) setToken(spec theme.Spec, mode string, v any) {
	put := func(m *map[string]any) {
		if *m == nil {
			*m = map[string]any{}
		}
		(*m)[spec.Key] = v
	}
	if spec.PerMode {
		if mode == "light" {
			put(&p.settings.Light)
		} else {
			put(&p.settings.Dark)
		}
	} else {
		put(&p.settings.Overrides)
	}
	p.touched = true
}

// usable returns the area of an output minus the panel.
func (p *planner) usable(o compositor.Output, t theme.Tokens) compositor.Rect {
	r := o.Rect
	gap := int(t.Num("window.gaps"))
	ph := int(t.Num("panel.height")) + int(t.Num("spacing.unit"))*2
	if t.Str("panel.position") == "bottom" {
		r.H -= ph
	} else {
		r.Y += ph
		r.H -= ph
	}
	r.X += gap
	r.Y += gap
	r.W -= 2 * gap
	r.H -= 2 * gap
	return r
}

func planArrange(ctx context.Context, p *planner, a map[string]any) (step, error) {
	layout := argStr(a, "layout")
	if !oneOf(layout, layouts...) {
		return step{}, fmt.Errorf("layout must be one of %s", strings.Join(layouts, ", "))
	}
	var ws compositor.Workspace
	if ref := argStr(a, "workspace"); ref != "" {
		w, err := p.workspace(ref)
		if err != nil {
			return step{}, err
		}
		ws = w
	} else {
		for _, w := range p.spaces {
			if w.Focused {
				ws = w
			}
		}
	}
	var wins []compositor.Window
	for _, w := range p.windows {
		if w.Workspace == ws.ID {
			wins = append(wins, w)
		}
	}
	if len(wins) == 0 {
		return step{}, fmt.Errorf("workspace %s has no windows", ws.Name)
	}
	sort.Slice(wins, func(i, j int) bool { return wins[i].ID < wins[j].ID })
	var out compositor.Output
	for _, o := range p.outputs {
		if o.Name == ws.Output || (ws.Output == "" && o.Focused) {
			out = o
		}
	}
	if out.Rect.W == 0 && len(p.outputs) > 0 {
		out = p.outputs[0]
	}
	tok, err := p.c.Themes.Resolve(p.settings, p.c.HW.Weak)
	if err != nil {
		return step{}, err
	}
	area := p.usable(out, tok)
	gap := int(tok.Num("window.gaps"))
	if gap < 4 {
		gap = 4
	}
	n := len(wins)
	rects := make([]compositor.Rect, n)
	switch layout {
	case "tile", "float":
	case "grid", "columns", "rows":
		cols, rows := 1, 1
		switch layout {
		case "grid":
			cols = int(math.Ceil(math.Sqrt(float64(n))))
			rows = int(math.Ceil(float64(n) / float64(cols)))
		case "columns":
			cols = n
		case "rows":
			rows = n
		}
		cw := (area.W - gap*(cols-1)) / cols
		rh := (area.H - gap*(rows-1)) / rows
		for i := range wins {
			c, r := i%cols, i/cols
			rects[i] = compositor.Rect{X: area.X + c*(cw+gap), Y: area.Y + r*(rh+gap), W: cw, H: rh}
		}
	case "cascade":
		w, h := area.W*3/5, area.H*3/4
		for i := range wins {
			off := 40 * i
			rects[i] = compositor.Rect{X: area.X + off, Y: area.Y + off, W: w, H: h}
		}
	case "center":
		w, h := area.W*3/5, area.H*4/5
		for i := range wins {
			rects[i] = compositor.Rect{X: area.X + (area.W-w)/2, Y: area.Y + (area.H-h)/2, W: w, H: h}
		}
	}
	summary := fmt.Sprintf("Arrange %d windows of workspace %s: %s", n, ws.Name, layout)
	return step{Summary: summary, run: func(ctx context.Context) (any, error) {
		for i, w := range wins {
			var err error
			switch layout {
			case "tile":
				err = p.c.Comp.SetFloating(ctx, w.ID, false)
			case "float":
				err = p.c.Comp.MoveResize(ctx, w.ID, compositor.Rect{X: area.X + 40*i, Y: area.Y + 40*i, W: area.W * 3 / 5, H: area.H * 3 / 4})
			default:
				err = p.c.Comp.MoveResize(ctx, w.ID, rects[i])
			}
			if err != nil {
				return nil, err
			}
		}
		return map[string]any{"windows": n}, nil
	}}, nil
}

// planSetState plans window.set_state. Minimizing needs a compositor that
// can hide windows (sway: the scratchpad); maximize and snap are floating
// placements on the usable area of the window's output, remembered so
// that "normal" puts the window back.
func planSetState(ctx context.Context, p *planner, a map[string]any) (step, error) {
	w, err := p.window(a["window"])
	if err != nil {
		return step{}, err
	}
	state := argStr(a, "state")
	if !oneOf(state, windowStates...) {
		return step{}, fmt.Errorf("state must be one of %s", strings.Join(windowStates, ", "))
	}
	min, canMin := p.c.Comp.(compositor.Minimizer)
	canMin = canMin && p.c.Comp.Caps().Minimize
	switch state {
	case "minimized":
		if !canMin {
			return step{}, fmt.Errorf("%s cannot minimize windows", p.c.Comp.Name())
		}
		if w.State == "minimized" {
			return step{}, fmt.Errorf("%s is already minimized", label(w))
		}
		return step{Summary: "Minimize " + label(w), run: func(ctx context.Context) (any, error) {
			return nil, min.Minimize(ctx, w.ID)
		}}, nil
	case "normal":
		if w.State == "minimized" {
			if !canMin {
				return step{}, fmt.Errorf("%s cannot restore minimized windows", p.c.Comp.Name())
			}
			return step{Summary: "Restore " + label(w), run: func(ctx context.Context) (any, error) {
				return nil, min.Unminimize(ctx, w.ID)
			}}, nil
		}
		p.c.mu.Lock()
		pl, ok := p.c.placed[w.ID]
		p.c.mu.Unlock()
		if !ok {
			return step{Summary: "Focus " + label(w) + " (already in its normal state)", run: func(ctx context.Context) (any, error) {
				return nil, p.c.Comp.Focus(ctx, w.ID)
			}}, nil
		}
		return step{Summary: "Restore the size of " + label(w), run: func(ctx context.Context) (any, error) {
			p.c.mu.Lock()
			delete(p.c.placed, w.ID)
			p.c.mu.Unlock()
			if err := p.c.Comp.MoveResize(ctx, w.ID, pl.Before); err != nil {
				return nil, err
			}
			return nil, p.c.Comp.Focus(ctx, w.ID)
		}}, nil
	}
	// maximized, left, right: a floating placement on the usable area.
	if !p.c.Comp.Caps().MoveResize {
		return step{}, fmt.Errorf("%s cannot place windows", p.c.Comp.Name())
	}
	area, err := p.windowArea(w)
	if err != nil {
		return step{}, err
	}
	gap := 0
	if tok, err := p.c.Themes.Resolve(p.settings, p.c.HW.Weak); err == nil {
		gap = int(tok.Num("window.gaps"))
	}
	r := area
	switch state {
	case "left":
		r.W = (area.W - gap) / 2
	case "right":
		r.W = (area.W - gap) / 2
		r.X = area.X + area.W - r.W
	}
	verb := map[string]string{"maximized": "Maximize ", "left": "Snap to the left half: ", "right": "Snap to the right half: "}[state]
	return step{Summary: verb + label(w), run: func(ctx context.Context) (any, error) {
		p.c.mu.Lock()
		before := w.Rect
		if old, ok := p.c.placed[w.ID]; ok {
			before = old.Before // keep the size from before the first placement
		}
		p.c.placed[w.ID] = placement{State: state, Before: before, At: r, Since: time.Now()}
		p.c.mu.Unlock()
		if w.State == "minimized" {
			if canMin {
				if err := min.Unminimize(ctx, w.ID); err != nil {
					return nil, err
				}
			}
		}
		if err := p.c.Comp.MoveResize(ctx, w.ID, r); err != nil {
			return nil, err
		}
		return nil, p.c.Comp.Focus(ctx, w.ID)
	}}, nil
}

// windowArea is the usable area (output minus panels, inset by the
// gaps) where a window is: its workspace's area when the compositor
// reports one, else its output minus the panel.
func (p *planner) windowArea(w compositor.Window) (compositor.Rect, error) {
	tok, err := p.c.Themes.Resolve(p.settings, p.c.HW.Weak)
	if err != nil {
		return compositor.Rect{}, err
	}
	gap := int(tok.Num("window.gaps"))
	var ws compositor.Workspace
	for _, s := range p.spaces {
		if s.ID == w.Workspace || (w.Workspace == "" && s.Focused) {
			ws = s
		}
	}
	if ws.Rect.W > 0 && ws.Rect.H > 0 {
		r := ws.Rect
		return compositor.Rect{X: r.X + gap, Y: r.Y + gap, W: r.W - 2*gap, H: r.H - 2*gap}, nil
	}
	cx, cy := w.Rect.X+w.Rect.W/2, w.Rect.Y+w.Rect.H/2
	var out compositor.Output
	for _, o := range p.outputs {
		if cx >= o.Rect.X && cx < o.Rect.X+o.Rect.W && cy >= o.Rect.Y && cy < o.Rect.Y+o.Rect.H {
			out = o
		}
	}
	if out.Rect.W == 0 {
		for _, o := range p.outputs {
			if o.Focused || out.Rect.W == 0 {
				out = o
			}
		}
	}
	if out.Rect.W == 0 {
		return compositor.Rect{}, errors.New("no output to place the window on")
	}
	return p.usable(out, tok), nil
}
