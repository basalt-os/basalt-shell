package assistant

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadArgsDrivers(t *testing.T) {
	ok := [][]string{
		{"drivers"}, {"drivers", "--json"}, {"drivers", "license", "nvidia"},
		{"drivers", "install", "nvidia", "--json"}, {"drivers", "install", "nvidia", "compute", "--json"},
		{"drivers", "rollback", "--json"},
		{"status"}, {"why", "nginx.service"},
	}
	for _, a := range ok {
		if err := readArgs(a); err != nil {
			t.Errorf("%v refused: %v", a, err)
		}
	}
	bad := [][]string{
		{"drivers", "install", "nvidia"},                      // text output: not for the shell
		{"drivers", "install", "nvidia", "--apply", "--json"}, // never apply from here
		{"drivers", "install", "amdgpu", "--json"},
		{"drivers", "install", "nvidia", "all", "--json"},
		{"drivers", "rollback", "--apply"},
		{"drivers", "license", "other"},
		{"drivers", "--yes"},
		{"snapshots", "rollback", "3"},
		{"apply", "p-123456"},
	}
	for _, a := range bad {
		if err := readArgs(a); err == nil {
			t.Errorf("%v accepted", a)
		}
	}
}

// The read helper and the bridge agree: a fake `basalt` and a fake pkexec
// show what the bridge runs for the Additional drivers page.
func TestDriversBridge(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	basalt := filepath.Join(dir, "basalt")
	script := `#!/bin/sh
echo "$*" >>` + log + `
case "$*" in
  "drivers --json") echo '{"recommendation":{"action":"install"}}' ;;
  "drivers install nvidia --json") echo '{"id":"p-a1b2c3","stored":true}' ;;
  "drivers rollback --json") echo '{"id":"p-d4e5f6","stored":false}' ;;
  "show p-a1b2c3 --json") echo '{"id":"p-a1b2c3","title":"install the NVIDIA driver","status":"pending"}' ;;
  "show p-a1b2c3") echo "Apply it:   sudo basalt apply p-a1b2c3"; echo "  (without a prompt: sudo basalt apply p-a1b2c3 --yes --confirm 0a1b2c3d)" ;;
esac
`
	if err := os.WriteFile(basalt, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &Bridge{Basalt: basalt, Helper: filepath.Join(dir, "missing")}
	ctx := context.Background()
	raw, err := b.Drivers(ctx)
	if err != nil || !strings.Contains(string(raw), `"install"`) {
		t.Fatalf("drivers: %v %s", err, raw)
	}
	p, err := b.DriversPropose(ctx, "")
	if err != nil || p.ID != "p-a1b2c3" || p.Code != "0a1b2c3d" {
		t.Fatalf("propose: %v %+v", err, p)
	}
	if _, err := b.DriversRollback(ctx); err == nil {
		t.Error("a proposal that was not stored must be an error")
	}
	if _, err := b.DriversPropose(ctx, "everything"); err == nil {
		t.Error("bad variant accepted")
	}
}
