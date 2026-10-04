package appearance

import (
	"os"
	"path/filepath"
	"testing"
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
