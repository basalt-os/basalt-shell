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
