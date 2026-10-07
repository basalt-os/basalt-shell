package niri

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basalt-os/basalt-shell/internal/compositor"
)

func TestKeyboardKDL(t *testing.T) {
	got, err := KeyboardKDL(compositor.KeyboardConfig{Layout: "br,us", Variant: ",intl", Options: "grp:alt_shift_toggle", RepeatDelay: 300, RepeatRate: 40})
	if err != nil {
		t.Fatal(err)
	}
	want := "input {\n    keyboard {\n        xkb {\n            layout \"br,us\"\n            variant \",intl\"\n            options \"grp:alt_shift_toggle\"\n        }\n        repeat-delay 300\n        repeat-rate 40\n    }\n}\n"
	if !strings.HasSuffix(got, want) {
		t.Errorf("got\n%s", got)
	}
	sys, _ := KeyboardKDL(compositor.KeyboardConfig{Layout: "br", SystemLayouts: true})
	if strings.Contains(sys, "input {") {
		t.Errorf("the system's layouts are not written:\n%s", sys)
	}
	if _, err := KeyboardKDL(compositor.KeyboardConfig{Layout: "br\" }\nbinds { Mod+X { spawn \"x\"; } }"}); err == nil {
		t.Error("a quote was written")
	}
	// SetKeyboard writes next to the theme include.
	dir := t.TempDir()
	a := &Adapter{StylePath: filepath.Join(dir, "basalt-theme.kdl")}
	if err := a.SetKeyboard(context.Background(), compositor.KeyboardConfig{Layout: "de"}); err != nil {
		t.Fatal(err)
	}
	if b, _ := os.ReadFile(filepath.Join(dir, "basalt-keyboard.kdl")); !strings.Contains(string(b), `layout "de"`) {
		t.Errorf("file:\n%s", b)
	}
}
