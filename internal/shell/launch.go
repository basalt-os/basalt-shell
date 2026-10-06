package shell

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

var reUnitChar = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

// sessionEnv is the session environment an application needs from the
// shell (display, toolkit and compositor variables). The session also
// imports it into the systemd user manager (basalt-session-init); it is
// passed again on each launch so headless and late-changed values work.
var sessionEnv = []string{
	"WAYLAND_DISPLAY", "DISPLAY", "XAUTHORITY",
	"XDG_CURRENT_DESKTOP", "XDG_SESSION_TYPE", "XDG_SESSION_DESKTOP",
	"SWAYSOCK", "I3SOCK", "NIRI_SOCKET",
	"QT_QPA_PLATFORM", "QT_QPA_PLATFORMTHEME", "QT_WAYLAND_DECORATION",
	"QT_WAYLAND_DISABLE_WINDOWDECORATION", "QT_QUICK_BACKEND",
	"GTK_THEME", "GTK_CSD", "GDK_BACKEND", "MOZ_ENABLE_WAYLAND",
	"ELECTRON_OZONE_PLATFORM_HINT", "SDL_VIDEODRIVER",
	"_JAVA_AWT_WM_NONREPARENTING", "XCURSOR_THEME", "XCURSOR_SIZE",
	"LIBGL_ALWAYS_SOFTWARE",
}

// Launch starts an application as a transient service of the systemd
// user manager (app-basalt-<id>-<random>.service), so it runs in the
// person's own SELinux domain (the manager's, unconfined_t or the login
// domain of a confined user), never in the daemon's basalt_shell_t: a
// scope (systemd-run --scope) would execute the program as a child of
// the daemon and inherit its domain. Without a user manager it falls back
// to the compositor's spawn, which also runs it outside the daemon.
func (c *Core) Launch(ctx context.Context, id string, argv []string) error {
	if sr, err := exec.LookPath("systemd-run"); err == nil && userManager() && os.Getenv("BASALT_SHELL_NO_UNIT") != "1" {
		cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
		defer cancel()
		cmd := exec.CommandContext(cctx, sr, launchArgs(id, randomSuffix(), argv, os.LookupEnv)...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
		if err := cmd.Run(); err == nil {
			return nil
		}
	}
	return c.Comp.Spawn(ctx, argv)
}

// launchArgs builds the systemd-run arguments for one application: a
// transient service (not a scope) of the user manager, of type exec (a
// missing program fails the launch), alive as long as any of its
// processes (ExitType=cgroup: launchers that fork and exit keep their
// app), unloaded when it ends, in app.slice.
func launchArgs(id, suffix string, argv []string, lookup func(string) (string, bool)) []string {
	unit := "app-basalt-" + strings.Trim(reUnitChar.ReplaceAllString(id, "_"), "_") + "-" + suffix + ".service"
	args := []string{
		"--user", "--quiet", "--collect",
		"--unit", unit,
		"--slice", "app.slice",
		"--service-type", "exec",
		"--property", "ExitType=cgroup",
		"--description", "Application " + id + " started by the Basalt shell",
	}
	for _, k := range sessionEnv {
		if v, ok := lookup(k); ok && v != "" {
			args = append(args, "--setenv", k+"="+v)
		}
	}
	args = append(args, "--")
	return append(args, argv...)
}

func randomSuffix() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return hex.EncodeToString(b)
}

// userManager reports whether a systemd user manager runs for this user
// (not the case in containers or nested test sessions).
func userManager() bool {
	rt := os.Getenv("XDG_RUNTIME_DIR")
	if rt == "" {
		return false
	}
	_, err := os.Stat(rt + "/systemd/private")
	return err == nil
}
