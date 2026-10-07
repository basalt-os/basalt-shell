package assistant

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestKeyboardArgs(t *testing.T) {
	for _, ok := range [][]string{
		{"keyboard", "--json"}, {"keyboard", "set", "br", "--json"}, {"keyboard", "set", "br,us(intl)", "--json"},
		{"keyboard", "set", "us(alt-intl),br,de(nodeadkeys),fr", "--options", "grp:alt_shift_toggle,compose:ralt", "--json"},
	} {
		if err := readArgs(ok); err != nil {
			t.Errorf("%v refused: %v", ok, err)
		}
	}
	for _, bad := range [][]string{
		{"keyboard"}, {"keyboard", "set", "br"}, {"keyboard", "set", "br", "--json", "--apply"},
		{"keyboard", "set", "br;reboot", "--json"}, {"keyboard", "set", "br,us,de,fr,it", "--json"},
		{"keyboard", "set", "br", "--options", "", "--json"}, {"keyboard", "set", "br", "--options", "x", "--json"},
		{"keyboard", "set", "br", "--options", "grp:alt_shift_toggle;id", "--json"}, {"keyboard", "set", "$(id)", "--json"},
		{"keyboard", "apply", "br", "--json"}, {"keyboard", "set", "br", "--yes", "--json"},
	} {
		if err := readArgs(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	if a, err := KeyboardArgs("br,us(intl)", ""); err != nil || strings.Join(a, " ") != "keyboard set br,us(intl) --json" {
		t.Errorf("%v %v", a, err)
	}
	// Storing keyboard.system starts from Settings, never from a request
	// in natural language.
	if Understood("Understood as: basalt keyboard set br --json") != nil {
		t.Error("keyboard set runs from natural language")
	}
}

// The bridge stores the proposal through the read helper and shows it.
func TestKeyboardBridge(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	basalt := filepath.Join(dir, "basalt")
	script := `#!/bin/sh
echo "$*" >>` + log + `
case "$*" in
  "keyboard set br,us(intl) --options grp:alt_shift_toggle --json") echo '{"id":"p-0b0a0c","stored":true}' ;;
  "show p-0b0a0c --json") echo '{"id":"p-0b0a0c","title":"use br,us(intl) for the login screen","kind":"keyboard","status":"pending"}' ;;
  "show p-0b0a0c") echo "  (without a prompt: sudo basalt apply p-0b0a0c --yes --confirm 1a2b3c4d)" ;;
esac
`
	if err := os.WriteFile(basalt, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &Bridge{Basalt: basalt, Helper: filepath.Join(dir, "missing")}
	p, err := b.KeyboardPropose(context.Background(), "br,us(intl)", "grp:alt_shift_toggle")
	if err != nil || p.ID != "p-0b0a0c" || p.Code != "1a2b3c4d" {
		t.Fatalf("%v %+v", err, p)
	}
	if _, err := b.KeyboardPropose(context.Background(), "br\"; reboot", ""); err == nil {
		t.Error("a bad layout reached the helper")
	}
	got, _ := os.ReadFile(log)
	if strings.Contains(string(got), "reboot") || strings.Contains(string(got), "localectl") {
		t.Errorf("ran: %s", got)
	}
}
