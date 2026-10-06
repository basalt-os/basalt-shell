package assistant

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

// The desktop never runs dnf or rpm itself: package transactions run in
// the system assistant's executors (basalt_apply_t, basalt_gate_exec_t),
// started as systemd units. An rpm transition in the shell's policy would
// let the shell's domain run them; this test refuses it.
func TestShellPolicyHasNoRPMTransition(t *testing.T) {
	files, _ := filepath.Glob("../../selinux/*.te")
	if len(files) == 0 {
		t.Fatal("no policy files found")
	}
	re := regexp.MustCompile(`\brpm_(domtrans|run|exec|transition_script|domtrans_script)\b|\brpm_exec_t\b|\brpm_t\b|\brpm_script_t\b`)
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for i, line := range strings.Split(string(b), "\n") {
			code, _, _ := strings.Cut(line, "#")
			if re.MatchString(code) {
				t.Errorf("%s:%d: %s: the shell's domains must not run dnf or rpm (start the assistant's units instead)", f, i+1, strings.TrimSpace(line))
			}
		}
	}
}

// Apply starts the assistant's fixed unit with the proposal id and its
// confirmation code, never basalt apply or dnf.
func TestApplyStartsTheAssistantUnit(t *testing.T) {
	if u, err := applyUnit("p-1a2b3c", "3c140f0a"); err != nil || u != "basalt-apply@p-1a2b3c_3c140f0a.service" {
		t.Errorf("%q %v", u, err)
	}
	for _, bad := range [][2]string{{"p-1a2b3c; reboot", "3c140f0a"}, {"p-1a2b3c", "zz"}, {"../x", "3c140f0a"}} {
		if _, err := applyUnit(bad[0], bad[1]); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

// The read helper runs as root through pkexec, still in the desktop's
// domain: it must not start rpm or dnf, directly or through the assistant.
// It only passes requests the assistant answers from the reports its root
// units wrote (basalt-assistant's TestReadHelperCommandsRunNoRPMOrDNF runs
// these exact forms against a runner that refuses rpm and dnf).
func TestReadHelperRunsNoRPMOrDNF(t *testing.T) {
	b, err := os.ReadFile("../../libexec/assistant-read")
	if err != nil {
		t.Fatal(err)
	}
	re := regexp.MustCompile(`\b(rpm|rpmkeys|dnf|dnf5|yum|pkcon|packagekit)\b`)
	for i, line := range strings.Split(string(b), "\n") {
		code, _, _ := strings.Cut(line, "#")
		if re.MatchString(code) {
			t.Errorf("assistant-read:%d runs a package tool: %s", i+1, strings.TrimSpace(line))
		}
	}
	// Only the cached drivers forms are offered.
	for _, form := range []string{`"--json --cached"`, `"rollback --json --cached"`, `"install nvidia --json --cached"`} {
		if !strings.Contains(string(b), form) {
			t.Errorf("assistant-read lacks the drivers form %s", form)
		}
	}
	for _, live := range []string{`"" | "--json" | "license nvidia"`, `"rollback --json" |`, `"install nvidia --json" |`, `"check --json"`} {
		if strings.Contains(string(b), live) {
			t.Errorf("assistant-read still offers %s, which queries rpm or dnf", live)
		}
	}
	// The bridge refuses the live forms before the helper sees them.
	for _, args := range [][]string{{"drivers"}, {"drivers", "--json"}, {"drivers", "install", "nvidia", "--json"},
		{"drivers", "rollback", "--json"}, {"updates", "check", "--json"}} {
		if readArgs(args) == nil {
			t.Errorf("%v is accepted: it would query rpm or dnf in the desktop's domain", args)
		}
	}
}
