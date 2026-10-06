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

// A person who is not an administrator is never asked for a password by
// the background refresh of the proposals: Pending does not run pkexec
// for them (zero setup: no password prompt at login).
func TestPendingWithoutAdmin(t *testing.T) {
	dir := t.TempDir()
	log := filepath.Join(dir, "log")
	pk := filepath.Join(dir, "pkexec")
	basalt := filepath.Join(dir, "basalt")
	helper := filepath.Join(dir, "assistant-read")
	for p, body := range map[string]string{
		pk:     "#!/bin/sh\necho \"pkexec $*\" >>" + log + "\necho '[{\"id\":\"p-root00\"}]'\n",
		basalt: "#!/bin/sh\necho \"basalt $*\" >>" + log + "\necho '[]'\n",
		helper: "#!/bin/sh\n",
	} {
		if err := os.WriteFile(p, []byte(body), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	b := &Bridge{Basalt: basalt, Helper: helper, Pkexec: pk}
	ps, err := b.Pending(context.Background())
	if err != nil || len(ps) != 0 {
		t.Fatalf("not an administrator: %v %v", ps, err)
	}
	if got, _ := os.ReadFile(log); strings.Contains(string(got), "pkexec") {
		t.Errorf("pkexec ran for a person who is not an administrator: %s", got)
	}
	b.Admin = true
	ps, err = b.Pending(context.Background())
	if err != nil || len(ps) != 1 || ps[0].ID != "p-root00" {
		t.Fatalf("administrator: %v %v", ps, err)
	}
}
