// Command basalt-shell-ui-launch starts the shell UI (Quickshell with the
// shell's QML). It is the only entry point of the SELinux domain
// basalt_shell_ui_t, the one domain the daemon lets confirm requests, so
// it pins what runs there: Quickshell from the system, the QML installed
// next to this program, and an environment without variables that would
// load other code (plugin and import paths, preloads).
package main

import (
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
)

// dropped are variables that make Qt, QML or the loader read code from
// elsewhere. The kernel already marks a domain transition AT_SECURE (the
// loader ignores LD_PRELOAD and friends); Qt does not look at that flag.
var dropped = []string{
	"QT_PLUGIN_PATH", "QML_IMPORT_PATH", "QML2_IMPORT_PATH", "QT_QPA_PLATFORM_PLUGIN_PATH",
	"QT_QUICK_CONTROLS_STYLE_PATH", "QML_DISK_CACHE_PATH", "QT_QML_GENERATE_QMLLS_INI",
	"BASALT_SHELL_QML", "GCONV_PATH", "GIO_EXTRA_MODULES", "GTK_PATH", "PYTHONPATH",
}

func clean(env []string) []string {
	var out []string
	for _, kv := range env {
		k, _, _ := strings.Cut(kv, "=")
		drop := strings.HasPrefix(k, "LD_") || strings.HasPrefix(k, "QS_")
		for _, d := range dropped {
			if k == d {
				drop = true
			}
		}
		if !drop {
			out = append(out, kv)
		}
	}
	return out
}

// qmlDir finds the shell's QML: installed next to this program
// (PREFIX/libexec/basalt-shell -> PREFIX/share/basalt-shell/qml).
func qmlDir() (string, error) {
	exe, err := os.Executable()
	if err != nil {
		return "", err
	}
	exe, _ = filepath.EvalSymlinks(exe)
	for _, rel := range []string{"../../share/basalt-shell/qml", "../share/basalt-shell/qml", "../../shell"} {
		d := filepath.Clean(filepath.Join(filepath.Dir(exe), rel))
		if st, err := os.Stat(filepath.Join(d, "shell.qml")); err == nil && st.Mode().IsRegular() {
			return d, nil
		}
	}
	return "", fmt.Errorf("shell QML not found next to %s", exe)
}

func quickshell() (string, error) {
	for _, p := range []string{"/usr/bin/quickshell", "/usr/bin/qs"} {
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			return p, nil
		}
	}
	// Development installs (a container, a prefix): from PATH.
	if p, err := exec.LookPath("quickshell"); err == nil {
		return p, nil
	}
	return exec.LookPath("qs")
}

func main() {
	qml, err := qmlDir()
	if err != nil {
		fmt.Fprintln(os.Stderr, "basalt-shell-ui-launch:", err)
		os.Exit(1)
	}
	qs, err := quickshell()
	if err != nil {
		fmt.Fprintln(os.Stderr, "basalt-shell-ui-launch: quickshell is not installed")
		os.Exit(1)
	}
	argv := []string{qs, "-p", qml}
	if err := syscall.Exec(qs, argv, clean(os.Environ())); err != nil {
		fmt.Fprintln(os.Stderr, "basalt-shell-ui-launch:", err)
		os.Exit(1)
	}
}
