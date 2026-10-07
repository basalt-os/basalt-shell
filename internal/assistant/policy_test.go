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

// The system's keyboard (the login screen, the console, new accounts) is
// set by the assistant's executor (keyboard.system: localectl, after the
// person applied the proposal), never from the shell's domains: no code of
// the shell, its UI, its session scripts or its helper runs localectl or
// talks to systemd-localed, and its policy grants no localed D-Bus access.
func TestShellNeverSetsTheSystemKeyboard(t *testing.T) {
	re := regexp.MustCompile(`\blocalectl\b|org\.freedesktop\.locale1|\bset-x11-keymap\b|\bset-keymap\b|systemd_dbus_chat_localed|\blocaled\b`)
	var files []string
	for _, glob := range []string{"../../internal/*/*.go", "../../internal/*/*/*.go", "../../cmd/*/*.go", "../../shell/*.qml",
		"../../greeter/*.qml", "../../greeter/*.js", "../../greeter/bin/*", "../../bin/*", "../../libexec/*", "../../selinux/*.te", "../../selinux/*.if"} {
		m, _ := filepath.Glob(glob)
		files = append(files, m...)
	}
	if len(files) < 50 {
		t.Fatalf("only %d files scanned", len(files))
	}
	for _, f := range files {
		if strings.HasSuffix(f, "_test.go") {
			continue
		}
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		hash := !strings.HasSuffix(f, ".go") && !strings.HasSuffix(f, ".qml") && !strings.HasSuffix(f, ".js")
		for i, line := range strings.Split(string(b), "\n") {
			code := line
			if hash {
				code, _, _ = strings.Cut(code, "#")
			} else {
				code, _, _ = strings.Cut(code, "//")
				if t := strings.TrimSpace(code); strings.HasPrefix(t, "*") || strings.HasPrefix(t, "/*") {
					continue
				}
			}
			if re.MatchString(code) {
				t.Errorf("%s:%d: %s: the system's keyboard is the assistant's keyboard.system proposal, not the shell's", f, i+1, strings.TrimSpace(line))
			}
		}
	}
	// The helper only stores the proposal (basalt keyboard set ... --json).
	b, _ := os.ReadFile("../../libexec/assistant-read")
	if !strings.Contains(string(b), `exec basalt keyboard "$@"`) {
		t.Error("assistant-read does not offer the keyboard forms")
	}
}
