package shell

import (
	"slices"
	"strings"
	"testing"
)

// Applications must start as services of the user manager, never as a
// scope: a scope executes the program as a child of the daemon, in the
// daemon's SELinux domain (basalt_shell_t).
func TestLaunchArgsUseUserServiceNotScope(t *testing.T) {
	env := map[string]string{"WAYLAND_DISPLAY": "wayland-1", "SWAYSOCK": "/run/user/1000/sway-ipc.sock", "BASALT_SHELL_SOCKET": "/x", "HOME": "/home/u"}
	lookup := func(k string) (string, bool) { v, ok := env[k]; return v, ok }
	args := launchArgs("org.gnome.TextEditor", "0a1b2c3d", []string{"gnome-text-editor", "--new-window"}, lookup)

	if slices.Contains(args, "--scope") {
		t.Fatalf("launch uses a scope (inherits the daemon's domain): %q", args)
	}
	if !slices.Contains(args, "--user") {
		t.Fatalf("launch does not go through the user manager: %q", args)
	}
	i := slices.Index(args, "--unit")
	if i < 0 || args[i+1] != "app-basalt-org.gnome.TextEditor-0a1b2c3d.service" {
		t.Fatalf("unit name: %q", args)
	}
	if j := slices.Index(args, "--service-type"); j < 0 || args[j+1] != "exec" {
		t.Fatalf("service type: %q", args)
	}
	if !slices.Contains(args, "ExitType=cgroup") {
		t.Fatalf("forking launchers would lose their app: %q", args)
	}
	sep := slices.Index(args, "--")
	if sep < 0 || !slices.Equal(args[sep+1:], []string{"gnome-text-editor", "--new-window"}) {
		t.Fatalf("argv after --: %q", args)
	}
	joined := strings.Join(args[:sep], " ")
	for _, want := range []string{"--setenv WAYLAND_DISPLAY=wayland-1", "--setenv SWAYSOCK=/run/user/1000/sway-ipc.sock"} {
		if !strings.Contains(joined, want) {
			t.Fatalf("missing %q in %q", want, joined)
		}
	}
	// Only session display/toolkit variables are passed, nothing of the
	// daemon's own configuration.
	for _, no := range []string{"BASALT_SHELL_SOCKET", "HOME="} {
		if strings.Contains(joined, no) {
			t.Fatalf("leaks %q: %q", no, joined)
		}
	}
}

func TestLaunchArgsSanitizeUnitName(t *testing.T) {
	args := launchArgs("../evil app;rm", "ff", []string{"x"}, func(string) (string, bool) { return "", false })
	i := slices.Index(args, "--unit")
	if got := args[i+1]; got != "app-basalt-.._evil_app_rm-ff.service" {
		t.Fatalf("unit %q", got)
	}
}
