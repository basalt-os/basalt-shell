package theme

import (
	"path/filepath"
	"testing"
)

func TestSampleThemes(t *testing.T) {
	dir, _ := filepath.Abs("../../themes")
	st := NewStore([]string{dir}, t.TempDir())
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	if errs := st.LoadErrors(); len(errs) > 0 {
		t.Fatal(errs)
	}
	if len(st.Themes()) != 3 {
		t.Fatalf("want 3 themes, got %d", len(st.Themes()))
	}
	for _, m := range st.Themes() {
		f, _ := st.Theme(m.ID)
		for _, mode := range []string{"light", "dark"} {
			s := Settings{Theme: m.ID, Mode: mode, Motion: "full"}
			tok := Resolve(s, f, false)
			// Body text and text on accent must be readable (WCAG AA).
			for _, pair := range [][2]string{{"color.text", "color.bg"}, {"color.text", "color.surface"}, {"color.accentText", "color.accent"}} {
				if c := Contrast(tok.Str(pair[0]), tok.Str(pair[1])); c < 4.5 {
					t.Errorf("%s/%s: %s on %s contrast %.2f", m.ID, mode, pair[0], pair[1], c)
				}
			}
			if c := Contrast(tok.Str("color.textMuted"), tok.Str("color.surface")); c < 3 {
				t.Errorf("%s/%s: muted text contrast %.2f", m.ID, mode, c)
			}
		}
	}
	tok := Resolve(Settings{Theme: "basalt", Mode: "dark", Motion: "auto"}, mustTheme(t, st, "basalt"), true)
	if tok.Str("motion") != "reduced" || tok.Num("motion.normal") != 0 {
		t.Fatalf("weak hardware should reduce motion: %v %v", tok["motion"], tok["motion.normal"])
	}
}

func mustTheme(t *testing.T, st *Store, id string) *File {
	f, ok := st.Theme(id)
	if !ok {
		t.Fatal(id)
	}
	return f
}

func TestNearestAccent(t *testing.T) {
	for in, want := range map[string]string{"#a3472e": "orange", "#3b6fd1": "blue", "#4e7a3e": "green", "#777777": "slate"} {
		if got := NearestAccent(in); got != want {
			t.Errorf("%s: %s, want %s", in, got, want)
		}
	}
}

// The geometry tokens of the shipped themes stay inside the ranges the
// desktop is designed for (desktop space and polish spec): an attached
// panel of 24 to 56 px, gaps 0 to 40, 1 px frames by default, window
// corners under the popover radius, and the popover radius under the
// sheet radius.
func TestThemeGeometry(t *testing.T) {
	dir, _ := filepath.Abs("../../themes")
	st := NewStore([]string{dir}, t.TempDir())
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	for _, m := range st.Themes() {
		tok := Resolve(Settings{Theme: m.ID, Mode: "dark", Motion: "full"}, mustTheme(t, st, m.ID), false)
		if h := tok.Num("panel.height"); h < 24 || h > 56 {
			t.Errorf("%s: panel.height %v", m.ID, h)
		}
		if g := tok.Num("window.gaps"); g < 0 || g > 40 {
			t.Errorf("%s: window.gaps %v", m.ID, g)
		}
		if b := tok.Num("window.border"); b != 1 {
			t.Errorf("%s: window.border %v, want 1", m.ID, b)
		}
		if tok.Str("panel.style") != "attached" {
			t.Errorf("%s: panel.style %q, want attached", m.ID, tok.Str("panel.style"))
		}
		if tok.Num("radius.lg") > tok.Num("radius.xl") {
			t.Errorf("%s: radius.lg %v over radius.xl %v", m.ID, tok.Num("radius.lg"), tok.Num("radius.xl"))
		}
		// The attached panel's zone is its height; the floating pill adds
		// its margins.
		if z := PanelZone(tok); z != int(tok.Num("panel.height")) {
			t.Errorf("%s: attached panel zone %d", m.ID, z)
		}
		fl := tok.Clone()
		fl["panel.style"] = "floating"
		if z := PanelZone(fl); z != int(tok.Num("panel.height")+2*tok.Num("spacing.unit")) {
			t.Errorf("%s: floating panel zone %d", m.ID, z)
		}
	}
	b := Resolve(Settings{Theme: "basalt", Mode: "dark", Motion: "full"}, mustTheme(t, st, "basalt"), false)
	for k, want := range map[string]float64{"panel.height": 40, "window.gaps": 6, "radius.window": 8, "radius.lg": 12, "radius.xl": 16, "elevation.shadow": 0.35, "elevation.blur": 20} {
		if got := b.Num(k); got != want {
			t.Errorf("basalt %s = %v, want %v", k, got, want)
		}
	}
}

// A theme written before a token existed (a theme the person saved) still
// loads: tokens with a default get it.
func TestThemeWithoutNewTokens(t *testing.T) {
	f := mustTheme(t, func() *Store {
		dir, _ := filepath.Abs("../../themes")
		st := NewStore([]string{dir}, t.TempDir())
		if err := st.Load(); err != nil {
			t.Fatal(err)
		}
		return st
	}(), "basalt")
	old := &File{ID: "old", Name: "Old", Tokens: map[string]any{}, Light: map[string]any{}, Dark: map[string]any{}}
	for k, v := range f.Tokens {
		if k != "panel.style" && k != "radius.xl" {
			old.Tokens[k] = v
		}
	}
	for k, v := range f.Light {
		old.Light[k] = v
	}
	for k, v := range f.Dark {
		old.Dark[k] = v
	}
	if err := old.Validate(); err != nil {
		t.Fatal(err)
	}
	if old.Tokens["panel.style"] != "attached" || old.Tokens["radius.xl"] != 16.0 {
		t.Fatalf("defaults not filled: %v %v", old.Tokens["panel.style"], old.Tokens["radius.xl"])
	}
	delete(old.Tokens, "panel.height")
	if err := old.Validate(); err == nil {
		t.Fatal("a theme without a required token was accepted")
	}
}
