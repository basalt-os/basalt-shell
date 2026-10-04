// Package paths resolves the shell's configuration, state and data
// directories (XDG base directories, with fallbacks for a checkout).
package paths

import (
	"os"
	"path/filepath"
	"strings"
)

// ConfigDir is $XDG_CONFIG_HOME or ~/.config.
func ConfigDir() string {
	if d := os.Getenv("XDG_CONFIG_HOME"); d != "" {
		return d
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".config")
}

// StateDir is $BASALT_SHELL_STATE_DIR, else $XDG_STATE_HOME/basalt-shell or
// ~/.local/state/basalt-shell.
func StateDir() string {
	if d := os.Getenv("BASALT_SHELL_STATE_DIR"); d != "" {
		return d
	}
	if d := os.Getenv("XDG_STATE_HOME"); d != "" {
		return filepath.Join(d, "basalt-shell")
	}
	home, _ := os.UserHomeDir()
	return filepath.Join(home, ".local", "state", "basalt-shell")
}

// AuditLog is the activity log's path.
func AuditLog() string { return filepath.Join(StateDir(), "audit.jsonl") }

// ThemeDirs lists the directories with shipped themes: BASALT_SHELL_THEMES,
// else /usr/share/basalt-shell/themes plus the ones next to the program
// (a checkout or a ~/.local install).
func ThemeDirs() []string {
	if d := os.Getenv("BASALT_SHELL_THEMES"); d != "" {
		return strings.Split(d, ":")
	}
	dirs := []string{"/usr/share/basalt-shell/themes"}
	if exe, err := os.Executable(); err == nil {
		for _, rel := range []string{"../share/basalt-shell/themes", "../../themes", "../themes", "../../share/basalt-shell/themes"} {
			p := filepath.Clean(filepath.Join(filepath.Dir(exe), rel))
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				dirs = append(dirs, p)
			}
		}
	}
	return dirs
}
