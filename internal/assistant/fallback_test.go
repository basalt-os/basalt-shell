package assistant

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFallbackArgs(t *testing.T) {
	for _, ok := range [][]string{{"fallback", "--json"}, {"fallback", "gnome", "--json"}, {"fallback", "basalt", "--json"}} {
		if err := readArgs(ok); err != nil {
			t.Errorf("%v refused: %v", ok, err)
		}
	}
	for _, bad := range [][]string{
		{"fallback"}, {"fallback", "gnome"}, {"fallback", "gnome", "--json", "--apply"}, {"fallback", "kde", "--json"},
		{"fallback", "gnome", "--yes"}, {"fallback", "gnome;reboot", "--json"},
	} {
		if err := readArgs(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
	if _, err := FallbackArgs("gdm"); err == nil {
		t.Error("an unknown target was accepted")
	}
	// The switch starts from Settings, never from natural language.
	if Understood("Understood as: basalt fallback gnome --json") != nil {
		t.Error("fallback runs from natural language")
	}
}

// The bridge stores the proposal through the read helper and shows it;
// nothing but the read request runs.
func TestFallbackBridge(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	basalt := filepath.Join(dir, "basalt")
	script := `#!/bin/sh
echo "$*" >>` + log + `
case "$*" in
  "fallback gnome --json") echo '{"id":"p-0f0a11","stored":true}' ;;
  "show p-0f0a11 --json") echo '{"id":"p-0f0a11","title":"use GDM as the login screen while GNOME is the fallback","kind":"desktop.fallback","status":"pending"}' ;;
  "show p-0f0a11") echo "  (without a prompt: sudo basalt apply p-0f0a11 --yes --confirm 0a1b2c3d)" ;;
esac
`
	if err := os.WriteFile(basalt, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &Bridge{Basalt: basalt, Helper: filepath.Join(dir, "missing")}
	p, err := b.FallbackPropose(context.Background(), "gnome")
	if err != nil || p.ID != "p-0f0a11" || p.Code != "0a1b2c3d" || p.Kind != "desktop.fallback" {
		t.Fatalf("%v %+v", err, p)
	}
	got, _ := os.ReadFile(log)
	if strings.Contains(string(got), "systemctl") || strings.Contains(string(got), "apply") {
		t.Errorf("ran: %s", got)
	}
}
