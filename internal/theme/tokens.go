// Package theme holds the design tokens of the shell: one flat set of
// named values (colors, typography, radius, spacing, elevation, motion,
// compositor style) resolved from a theme file, the light or dark mode
// and the user's overrides. Every surface of the shell, the compositor
// style and the application appearance (GTK, Qt, portals) are derived
// from the resolved tokens, so changing one token changes the desktop.
package theme

import (
	"fmt"
	"math"
	"regexp"
	"sort"
	"strconv"
	"strings"
)

// Kind of a token value.
type Kind string

const (
	Color  Kind = "color"  // "#RRGGBB" or "#RRGGBBAA"
	Number Kind = "number" // float within [Min, Max]
	Bool   Kind = "bool"
	Text   Kind = "text" // free text with a pattern (font families, icon theme names)
	Enum   Kind = "enum" // one of Options
)

// Spec describes one token.
type Spec struct {
	Key     string   `json:"key"`
	Kind    Kind     `json:"kind"`
	Group   string   `json:"group"`
	Label   string   `json:"label"`
	Min     float64  `json:"min,omitempty"`
	Max     float64  `json:"max,omitempty"`
	Step    float64  `json:"step,omitempty"`
	Options []string `json:"options,omitempty"`
	// PerMode tokens (colors) have one value for light and one for dark.
	PerMode bool `json:"per_mode,omitempty"`
	// Default is the value of a token added after themes were already
	// written: a theme file without it (a theme the person saved before)
	// gets this value instead of being rejected. nil means required.
	Default any `json:"default,omitempty"`
}

// Specs is the closed set of tokens. A theme file or an override with an
// unknown key or an out-of-range value is rejected.
var Specs = []Spec{
	// Colors (per mode).
	{Key: "color.bg", Kind: Color, Group: "color", Label: "Background", PerMode: true},
	{Key: "color.surface", Kind: Color, Group: "color", Label: "Surface", PerMode: true},
	{Key: "color.surfaceAlt", Kind: Color, Group: "color", Label: "Raised surface", PerMode: true},
	{Key: "color.border", Kind: Color, Group: "color", Label: "Border", PerMode: true},
	{Key: "color.text", Kind: Color, Group: "color", Label: "Text", PerMode: true},
	{Key: "color.textMuted", Kind: Color, Group: "color", Label: "Muted text", PerMode: true},
	{Key: "color.accent", Kind: Color, Group: "color", Label: "Accent", PerMode: true},
	{Key: "color.accentText", Kind: Color, Group: "color", Label: "Text on accent", PerMode: true},
	{Key: "color.success", Kind: Color, Group: "color", Label: "Success", PerMode: true},
	{Key: "color.warning", Kind: Color, Group: "color", Label: "Warning", PerMode: true},
	{Key: "color.danger", Kind: Color, Group: "color", Label: "Danger", PerMode: true},
	{Key: "color.scrim", Kind: Color, Group: "color", Label: "Scrim behind sheets", PerMode: true},

	// Typography.
	{Key: "font.family", Kind: Text, Group: "typography", Label: "Interface font"},
	{Key: "font.mono", Kind: Text, Group: "typography", Label: "Monospace font"},
	{Key: "font.size", Kind: Number, Group: "typography", Label: "Base size (pt)", Min: 8, Max: 20, Step: 0.5},
	{Key: "font.scale", Kind: Number, Group: "typography", Label: "Type scale ratio", Min: 1.05, Max: 1.5, Step: 0.01},

	// Shape.
	{Key: "radius.sm", Kind: Number, Group: "shape", Label: "Small radius", Min: 0, Max: 24, Step: 1},
	{Key: "radius.md", Kind: Number, Group: "shape", Label: "Medium radius", Min: 0, Max: 32, Step: 1},
	{Key: "radius.lg", Kind: Number, Group: "shape", Label: "Large radius", Min: 0, Max: 40, Step: 1},
	{Key: "radius.xl", Kind: Number, Group: "shape", Label: "Sheet radius", Min: 0, Max: 48, Step: 1, Default: 16.0},
	{Key: "radius.window", Kind: Number, Group: "shape", Label: "Window corners", Min: 0, Max: 32, Step: 1},

	// Spacing.
	{Key: "spacing.unit", Kind: Number, Group: "spacing", Label: "Spacing unit (px)", Min: 2, Max: 8, Step: 1},
	{Key: "panel.height", Kind: Number, Group: "spacing", Label: "Panel height", Min: 24, Max: 56, Step: 1},
	{Key: "panel.position", Kind: Enum, Group: "spacing", Label: "Panel position", Options: []string{"top", "bottom"}},
	{Key: "panel.opacity", Kind: Number, Group: "spacing", Label: "Panel opacity", Min: 0.5, Max: 1, Step: 0.05},
	// attached: a full-width bar on the screen edge with a hairline toward
	// the windows; floating: the earlier pill, 2 spacing units from the
	// edges with the large radius.
	{Key: "panel.style", Kind: Enum, Group: "spacing", Label: "Panel style", Options: []string{"attached", "floating"}, Default: "attached"},

	// Elevation.
	{Key: "elevation.shadow", Kind: Number, Group: "elevation", Label: "Shadow strength", Min: 0, Max: 1, Step: 0.05},
	{Key: "elevation.blur", Kind: Number, Group: "elevation", Label: "Shadow softness", Min: 0, Max: 64, Step: 1},

	// Motion.
	{Key: "motion.fast", Kind: Number, Group: "motion", Label: "Fast (ms)", Min: 0, Max: 400, Step: 10},
	{Key: "motion.normal", Kind: Number, Group: "motion", Label: "Normal (ms)", Min: 0, Max: 800, Step: 10},
	{Key: "motion.slow", Kind: Number, Group: "motion", Label: "Slow (ms)", Min: 0, Max: 1200, Step: 10},
	{Key: "motion.easing", Kind: Enum, Group: "motion", Label: "Easing", Options: []string{"OutCubic", "OutQuart", "InOutQuad", "OutBack", "Linear"}},

	// Compositor (window decorations).
	{Key: "window.gaps", Kind: Number, Group: "windows", Label: "Gaps", Min: 0, Max: 40, Step: 1},
	{Key: "window.border", Kind: Number, Group: "windows", Label: "Border width", Min: 0, Max: 8, Step: 1},
	{Key: "window.shadows", Kind: Bool, Group: "windows", Label: "Window shadows"},
	{Key: "window.blur", Kind: Bool, Group: "windows", Label: "Blur behind translucent surfaces"},
	{Key: "window.dimInactive", Kind: Number, Group: "windows", Label: "Dim inactive windows", Min: 0, Max: 0.5, Step: 0.05},

	// Application appearance.
	{Key: "apps.iconTheme", Kind: Text, Group: "apps", Label: "Icon theme"},
	{Key: "apps.cursorTheme", Kind: Text, Group: "apps", Label: "Cursor theme"},
	{Key: "apps.cursorSize", Kind: Number, Group: "apps", Label: "Cursor size", Min: 16, Max: 64, Step: 4},
	{Key: "apps.palette", Kind: Enum, Group: "apps", Label: "Apply the palette to apps", Options: []string{"full", "accent", "off"}},
}

var specByKey = func() map[string]Spec {
	m := map[string]Spec{}
	for _, s := range Specs {
		m[s.Key] = s
	}
	return m
}()

// Lookup returns the spec of a key.
func Lookup(key string) (Spec, bool) {
	s, ok := specByKey[key]
	return s, ok
}

var (
	reColor = regexp.MustCompile(`^#[0-9A-Fa-f]{6}([0-9A-Fa-f]{2})?$`)
	reText  = regexp.MustCompile(`^[A-Za-z0-9 ._+-]{1,64}$`)
)

// Normalize validates a value for a key and returns its canonical form
// (colors in lower case, numbers as float64 rounded to the step).
func Normalize(key string, v any) (any, error) {
	s, ok := specByKey[key]
	if !ok {
		return nil, fmt.Errorf("unknown token %q", key)
	}
	switch s.Kind {
	case Color:
		str, ok := v.(string)
		if !ok || !reColor.MatchString(str) {
			return nil, fmt.Errorf("%s: want a color like #a3472e, got %v", key, v)
		}
		return strings.ToLower(str), nil
	case Number:
		f, ok := toFloat(v)
		if !ok {
			return nil, fmt.Errorf("%s: want a number, got %v", key, v)
		}
		if f < s.Min || f > s.Max {
			return nil, fmt.Errorf("%s: %v is outside %v..%v", key, f, s.Min, s.Max)
		}
		if s.Step > 0 {
			f = math.Round(f/s.Step) * s.Step
			f = math.Round(f*1000) / 1000
		}
		return f, nil
	case Bool:
		switch b := v.(type) {
		case bool:
			return b, nil
		case string:
			switch strings.ToLower(b) {
			case "true", "on", "yes":
				return true, nil
			case "false", "off", "no":
				return false, nil
			}
		}
		return nil, fmt.Errorf("%s: want true or false, got %v", key, v)
	case Text:
		str, ok := v.(string)
		if !ok || !reText.MatchString(str) {
			return nil, fmt.Errorf("%s: want a short name, got %v", key, v)
		}
		return str, nil
	case Enum:
		str, ok := v.(string)
		if ok {
			for _, o := range s.Options {
				if o == str {
					return str, nil
				}
			}
		}
		return nil, fmt.Errorf("%s: want one of %s, got %v", key, strings.Join(s.Options, ", "), v)
	}
	return nil, fmt.Errorf("%s: unsupported kind", key)
}

func toFloat(v any) (float64, bool) {
	switch n := v.(type) {
	case float64:
		return n, true
	case float32:
		return float64(n), true
	case int:
		return float64(n), true
	case int64:
		return float64(n), true
	case string:
		f, err := strconv.ParseFloat(n, 64)
		return f, err == nil
	}
	return 0, false
}

// Tokens is a resolved token set.
type Tokens map[string]any

// Clone copies a token set.
func (t Tokens) Clone() Tokens {
	c := make(Tokens, len(t))
	for k, v := range t {
		c[k] = v
	}
	return c
}

// Num returns a number token (0 if missing).
func (t Tokens) Num(k string) float64 {
	f, _ := toFloat(t[k])
	return f
}

// Str returns a string token.
func (t Tokens) Str(k string) string {
	s, _ := t[k].(string)
	return s
}

// Bool returns a bool token.
func (t Tokens) Bool(k string) bool {
	b, _ := t[k].(bool)
	return b
}

// PanelZone is the strip the panel takes from the screen edge (its
// exclusive zone, in logical pixels): the panel's height when it is
// attached to the edge, plus the two spacing units around the floating
// pill. The shell UI (Bar.qml) uses the same rule.
func PanelZone(t Tokens) int {
	h := int(t.Num("panel.height"))
	if t.Str("panel.style") == "floating" {
		h += int(t.Num("spacing.unit")) * 2
	}
	return h
}

// Change is one entry of a token diff.
type Change struct {
	Key  string `json:"key"`
	From any    `json:"from"`
	To   any    `json:"to"`
}

// Diff lists the keys whose value differs, sorted by key, plus the
// computed keys "mode" and "theme" when they change.
func Diff(a, b Tokens) []Change {
	keys := map[string]bool{}
	for k := range a {
		keys[k] = true
	}
	for k := range b {
		keys[k] = true
	}
	var out []Change
	for k := range keys {
		if fmt.Sprint(a[k]) != fmt.Sprint(b[k]) {
			out = append(out, Change{Key: k, From: a[k], To: b[k]})
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Key < out[j].Key })
	return out
}
