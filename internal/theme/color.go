package theme

import (
	"fmt"
	"math"
	"strconv"
)

// RGB is a color with components in 0..1 and alpha.
type RGB struct{ R, G, B, A float64 }

// ParseColor reads #RRGGBB or #RRGGBBAA.
func ParseColor(s string) (RGB, error) {
	if !reColor.MatchString(s) {
		return RGB{}, fmt.Errorf("not a color: %q", s)
	}
	p := func(i int) float64 {
		v, _ := strconv.ParseUint(s[i:i+2], 16, 8)
		return float64(v) / 255
	}
	c := RGB{R: p(1), G: p(3), B: p(5), A: 1}
	if len(s) == 9 {
		c.A = p(7)
	}
	return c, nil
}

// Hex formats a color (alpha only when not opaque).
func (c RGB) Hex() string {
	b := func(f float64) int { return int(math.Round(clamp01(f) * 255)) }
	if c.A < 0.999 {
		return fmt.Sprintf("#%02x%02x%02x%02x", b(c.R), b(c.G), b(c.B), b(c.A))
	}
	return fmt.Sprintf("#%02x%02x%02x", b(c.R), b(c.G), b(c.B))
}

func clamp01(f float64) float64 { return math.Max(0, math.Min(1, f)) }

// HSL returns hue (0..360), saturation and lightness (0..1).
func (c RGB) HSL() (h, s, l float64) {
	mx := math.Max(c.R, math.Max(c.G, c.B))
	mn := math.Min(c.R, math.Min(c.G, c.B))
	l = (mx + mn) / 2
	if mx == mn {
		return 0, 0, l
	}
	d := mx - mn
	if l > 0.5 {
		s = d / (2 - mx - mn)
	} else {
		s = d / (mx + mn)
	}
	switch mx {
	case c.R:
		h = (c.G - c.B) / d
		if c.G < c.B {
			h += 6
		}
	case c.G:
		h = (c.B-c.R)/d + 2
	default:
		h = (c.R-c.G)/d + 4
	}
	return h * 60, s, l
}

// FromHSL builds a color.
func FromHSL(h, s, l, a float64) RGB {
	if s == 0 {
		return RGB{l, l, l, a}
	}
	var q float64
	if l < 0.5 {
		q = l * (1 + s)
	} else {
		q = l + s - l*s
	}
	p := 2*l - q
	hk := h / 360
	f := func(t float64) float64 {
		if t < 0 {
			t++
		}
		if t > 1 {
			t--
		}
		switch {
		case t < 1.0/6:
			return p + (q-p)*6*t
		case t < 0.5:
			return q
		case t < 2.0/3:
			return p + (q-p)*(2.0/3-t)*6
		}
		return p
	}
	return RGB{f(hk + 1.0/3), f(hk), f(hk - 1.0/3), a}
}

// ScaleLightness multiplies the lightness of a color (factor < 1 darkens).
func ScaleLightness(hex string, factor float64) (string, error) {
	c, err := ParseColor(hex)
	if err != nil {
		return "", err
	}
	h, s, l := c.HSL()
	return FromHSL(h, s, clamp01(l*factor), c.A).Hex(), nil
}

// Mix blends a toward b by t (0..1).
func Mix(a, b string, t float64) string {
	ca, err1 := ParseColor(a)
	cb, err2 := ParseColor(b)
	if err1 != nil || err2 != nil {
		return a
	}
	m := func(x, y float64) float64 { return x + (y-x)*t }
	return RGB{m(ca.R, cb.R), m(ca.G, cb.G), m(ca.B, cb.B), m(ca.A, cb.A)}.Hex()
}

// WithAlpha sets the alpha of a color.
func WithAlpha(hex string, a float64) string {
	c, err := ParseColor(hex)
	if err != nil {
		return hex
	}
	c.A = a
	if a >= 0.999 {
		c.A = 0.9989 // force the 8-digit form
	}
	b := func(f float64) int { return int(math.Round(clamp01(f) * 255)) }
	return fmt.Sprintf("#%02x%02x%02x%02x", b(c.R), b(c.G), b(c.B), b(a))
}

// Luminance is the relative luminance (WCAG).
func Luminance(hex string) float64 {
	c, err := ParseColor(hex)
	if err != nil {
		return 0
	}
	lin := func(v float64) float64 {
		if v <= 0.03928 {
			return v / 12.92
		}
		return math.Pow((v+0.055)/1.055, 2.4)
	}
	return 0.2126*lin(c.R) + 0.7152*lin(c.G) + 0.0722*lin(c.B)
}

// Contrast is the WCAG contrast ratio of two colors.
func Contrast(a, b string) float64 {
	la, lb := Luminance(a), Luminance(b)
	if la < lb {
		la, lb = lb, la
	}
	return (la + 0.05) / (lb + 0.05)
}

// NamedAccents are the accent names GNOME and libadwaita know
// (org.gnome.desktop.interface accent-color), with libadwaita's colors.
var NamedAccents = []struct {
	Name string
	Hex  string
}{
	{"blue", "#3584e4"}, {"teal", "#2190a4"}, {"green", "#3a944a"}, {"yellow", "#c88800"},
	{"orange", "#ed5b00"}, {"red", "#e62d42"}, {"pink", "#d56199"}, {"purple", "#9141ac"}, {"slate", "#6f8396"},
}

// NearestAccent maps any color to the closest named accent by hue (gray
// colors map to slate).
func NearestAccent(hex string) string {
	c, err := ParseColor(hex)
	if err != nil {
		return "blue"
	}
	h, s, _ := c.HSL()
	if s < 0.18 {
		return "slate"
	}
	best, bestD := "blue", 1e9
	for _, a := range NamedAccents {
		ac, _ := ParseColor(a.Hex)
		ah, as, _ := ac.HSL()
		if as < 0.18 {
			continue
		}
		d := math.Abs(h - ah)
		if d > 180 {
			d = 360 - d
		}
		if d < bestD {
			best, bestD = a.Name, d
		}
	}
	return best
}
