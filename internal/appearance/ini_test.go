package appearance

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basalt-os/basalt-shell/internal/theme"
)

func TestSetINIKey(t *testing.T) {
	cases := []struct{ in, want string }{
		{"", "[text]\nk=v\n"},
		{"[text]\na=1\nk=old\nz=2\n\n[window]\nw=1\n", "[text]\na=1\nk=v\nz=2\n\n[window]\nw=1\n"},
		{"[text]\na=1\n\n[window]\nk=x\n", "[text]\na=1\nk=v\n\n[window]\nk=x\n"},
		{"[window]\nw=1\n", "[window]\nw=1\n\n[text]\nk=v\n"},
	}
	for _, c := range cases {
		if got := SetINIKey(c.in, "text", "k", "v"); got != c.want {
			t.Errorf("SetINIKey(%q) = %q, want %q", c.in, got, c.want)
		}
	}
}

func TestFeatherPadFollowsMode(t *testing.T) {
	dir := t.TempDir()
	p := filepath.Join(dir, "featherpad", "fp.conf")
	if err := writeFeatherPad(true, dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "[text]\ndarkColorScheme=true\n" {
		t.Fatalf("%q", b)
	}
	_ = os.WriteFile(p, []byte("[text]\ndarkColorScheme=true\nlineWrap=true\n"), 0o644)
	if err := writeFeatherPad(false, dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(p); string(b) != "[text]\ndarkColorScheme=false\nlineWrap=true\n" {
		t.Fatalf("%q", b)
	}
}

// foot draws its own title bar with buttons (close on sway, maximize and
// close on niri) in the theme's colors, instead of the compositor's bar
// without buttons.
func TestFootDrawsItsOwnTitleBar(t *testing.T) {
	tok := theme.Tokens{"font.family": "Inter", "font.size": 11.0, "spacing.unit": 4.0,
		"color.surfaceAlt": "#2b2f36", "color.text": "#e8e6e3"}
	ini := FootINI(tok)
	for _, want := range []string{"[csd]\n", "preferred=client\n", "size=32\n", "button-width=32\n",
		"color=ff2b2f36\n", "button-color=ffe8e6e3\n", "button-close-color=ff2b2f36\n", "font=Inter:weight=bold:pixelsize=13\n"} {
		if !strings.Contains(ini, want) {
			t.Errorf("foot ini lacks %q:\n%s", want, ini)
		}
	}
	dir := t.TempDir()
	if _, err := writeFoot(tok, dir); err != nil {
		t.Fatal(err)
	}
	main, _ := os.ReadFile(filepath.Join(dir, "foot", "foot.ini"))
	if !strings.Contains(string(main), "include="+filepath.Join(dir, "foot", "basalt-theme.ini")) {
		t.Fatalf("foot.ini does not include the theme: %q", main)
	}
	// An existing foot.ini is the person's: never rewritten.
	_ = os.WriteFile(filepath.Join(dir, "foot", "foot.ini"), []byte("[main]\nfont=mono:size=12\n"), 0o644)
	if _, err := writeFoot(tok, dir); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "foot", "foot.ini")); string(b) != "[main]\nfont=mono:size=12\n" {
		t.Fatalf("foot.ini changed: %q", b)
	}
}
