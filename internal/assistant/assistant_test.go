package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestReadArgsDrivers(t *testing.T) {
	ok := [][]string{
		{"drivers", "--json", "--cached"}, {"drivers", "license", "nvidia"},
		{"drivers", "install", "nvidia", "--json", "--cached"}, {"drivers", "install", "nvidia", "compute", "--json", "--cached"},
		{"drivers", "rollback", "--json", "--cached"},
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
  "drivers --json --cached") echo '{"recommendation":{"action":"install"}}' ;;
  "drivers install nvidia --json --cached") echo '{"id":"p-a1b2c3","stored":true}' ;;
  "drivers rollback --json --cached") echo '{"id":"p-d4e5f6","stored":false}' ;;
  "show p-a1b2c3 --json") echo '{"id":"p-a1b2c3","title":"install the NVIDIA driver","status":"pending"}' ;;
  "show p-a1b2c3") echo "Apply it:   sudo basalt apply p-a1b2c3"; echo "  (without a prompt: sudo basalt apply p-a1b2c3 --yes --confirm 0a1b2c3d)" ;;
esac
`
	if err := os.WriteFile(basalt, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &Bridge{Basalt: basalt, Helper: filepath.Join(dir, "missing")}
	fakeUnits(t, dir)
	ctx := context.Background()
	raw, err := b.Drivers(ctx)
	if got, _ := os.ReadFile(filepath.Join(dir, "systemctl.log")); !strings.Contains(string(got), "start --no-block basalt-drivers-refresh.service") {
		t.Errorf("the refresh unit was not started: %s", got)
	}
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

// An assistant older than `basalt drivers` (0.9.0 answers `unknown
// command "drivers"`) is ErrDriversUnsupported, which the page shows as
// "coming soon"; any other failure stays an error.
func TestDriversOldAssistant(t *testing.T) {
	dir := t.TempDir()
	basalt := filepath.Join(dir, "basalt")
	script := "#!/bin/sh\necho 'basalt: unknown command \"drivers\" (basalt help)' >&2\nexit 2\n"
	if err := os.WriteFile(basalt, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &Bridge{Basalt: basalt, Helper: filepath.Join(dir, "missing")}
	fakeUnits(t, dir)
	if _, err := b.Drivers(context.Background()); !errors.Is(err, ErrDriversUnsupported) {
		t.Fatalf("old assistant: %v", err)
	}
	if err := os.WriteFile(basalt, []byte("#!/bin/sh\necho 'basalt: cannot read the state' >&2\nexit 1\n"), 0o755); err != nil {
		t.Fatal(err)
	}
	if _, err := b.Drivers(context.Background()); err == nil || errors.Is(err, ErrDriversUnsupported) {
		t.Fatalf("other failure: %v", err)
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

// fakeUnits installs the assistant's units in a temporary directory and a
// systemctl that logs what it is asked.
func fakeUnits(t *testing.T, dir string) {
	t.Helper()
	units := filepath.Join(dir, "units")
	_ = os.MkdirAll(units, 0o755)
	for _, u := range []string{"basalt-drivers-refresh.service", "basalt-updates-check.service", "basalt-apply@.service", "basalt-offline-reboot.service"} {
		_ = os.WriteFile(filepath.Join(units, u), nil, 0o644)
	}
	sc := filepath.Join(dir, "systemctl")
	_ = os.WriteFile(sc, []byte("#!/bin/sh\necho \"$*\" >>"+filepath.Join(dir, "systemctl.log")+"\necho inactive\n"), 0o755)
	oldDirs, oldBin, oldGrace := unitDirs, systemctlBin, unitGrace
	unitDirs, systemctlBin, unitGrace = []string{units}, sc, 0
	t.Cleanup(func() { unitDirs, systemctlBin, unitGrace = oldDirs, oldBin, oldGrace })
}

// Without the refresh unit (an assistant older than 0.12.1) the page says
// that driver installation is coming soon.
func TestDriversWithoutRefreshUnit(t *testing.T) {
	old := unitDirs
	unitDirs = []string{t.TempDir()}
	defer func() { unitDirs = old }()
	b := &Bridge{Basalt: "/bin/true"}
	if _, err := b.Drivers(context.Background()); !errors.Is(err, ErrDriversUnsupported) {
		t.Fatal(err)
	}
}

func TestReportOnly(t *testing.T) {
	var ps []Proposal
	raw := `[{"id":"p-8bd6c8","title":"SELinux blocked x","actions":null},
		{"id":"p-000001","title":"fix","actions":[{"kind":"unit.restart","params":{"unit":"a.service"}}]},
		{"id":"p-000002","title":"hint","hints":[{"actions":[],"reason":"r"}]}]`
	if err := json.Unmarshal([]byte(raw), &ps); err != nil {
		t.Fatal(err)
	}
	ps = markReports(ps)
	if !ps[0].ReportOnly || ps[1].ReportOnly || ps[2].ReportOnly {
		t.Errorf("report_only: %v %v %v", ps[0].ReportOnly, ps[1].ReportOnly, ps[2].ReportOnly)
	}
}

func TestSecurityArgs(t *testing.T) {
	for _, ok := range [][]string{{"security", "audit"}, {"security", "accept", "encryption"}, {"security", "review", "tpm"}, {"security", "risks", "--json"}} {
		if err := readArgs(ok); err != nil {
			t.Errorf("%v: %v", ok, err)
		}
	}
	for _, bad := range [][]string{{"security", "accept", "selinux"}, {"security", "audit", "--apply"}, {"security", "accept", "encryption", "x"}} {
		if err := readArgs(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}
