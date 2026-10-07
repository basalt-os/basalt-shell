package shell

import (
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
)

// screenLockers are the lock screen programs a Basalt session may run
// besides the shell's own lock screen (which the UI reports, SetUILocked):
// basalt-lock starts swaylock when the shell UI is not running.
var screenLockers = map[string]bool{"swaylock": true, "gtklock": true, "waylock": true, "hyprlock": true}

// procDir is /proc (a variable for tests).
var procDir = "/proc"

// ScreenLocked reports whether a screen locker of this user is running.
//
// Push to talk stays off while the screen is locked, in two layers: the
// compositor does not run the shell's key bindings while a session lock is
// active (none of them is marked --locked in sway, allow-when-locked in
// niri), and the daemon refuses voice.press while the shell's lock screen
// holds the session or a locker runs (Core.Locked), so even a process that
// reaches the shell UI's IPC cannot open the microphone at the lock
// screen. A process named like a locker can only turn voice off, never on.
func ScreenLocked() bool {
	ents, err := os.ReadDir(procDir)
	if err != nil {
		return false
	}
	uid := os.Getuid()
	for _, e := range ents {
		if _, err := strconv.Atoi(e.Name()); err != nil {
			continue
		}
		dir := filepath.Join(procDir, e.Name())
		st, err := os.Stat(dir)
		if err != nil {
			continue
		}
		if sys, ok := st.Sys().(*syscall.Stat_t); !ok || int(sys.Uid) != uid {
			continue
		}
		comm, err := os.ReadFile(filepath.Join(dir, "comm"))
		if err != nil {
			continue
		}
		if screenLockers[strings.TrimSpace(string(comm))] {
			return true
		}
	}
	return false
}
