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
