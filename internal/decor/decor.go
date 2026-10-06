// Package decor decides who draws a window's title bar.
//
// The rule of the shell: an application that can draw a proper title bar
// with buttons (client-side decorations, CSD) draws it; the compositor
// draws a themed title bar only for the rest (X11 apps on sway, tiled
// windows). GTK 4 / libadwaita, GTK 3 headerbar apps, Firefox, Chromium
// and Electron ask for CSD by themselves; the session makes every other
// GTK 3 window do so (GTK_CSD=1) and Basalt's foot settings make foot
// ask for it (appearance.FootINI). Qt asks the
// compositor for server-side decorations whenever the compositor offers
// them, so the shell switches a Qt window to CSD when the Adwaita
// decoration plugin for its Qt version is installed (Fedora:
// qt6-qtwayland-adwaita-decoration, qadwaitadecorations-qt5); without the
// plugin Qt would draw its plain fallback frame, so the window keeps the
// compositor's title bar.
package decor

import (
	"bufio"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Toolkit identifies what a process draws its windows with, as far as
// the decision needs: "qt5", "qt6" or "".
func Toolkit(root string, pid int) string {
	f, err := os.Open(filepath.Join(root, "proc", strconv.Itoa(pid), "maps"))
	if err != nil {
		return ""
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	sc.Buffer(make([]byte, 64*1024), 1024*1024)
	for sc.Scan() {
		line := sc.Text()
		switch {
		case strings.Contains(line, "libQt6WaylandClient.so"):
			return "qt6"
		case strings.Contains(line, "libQt5WaylandClient.so"):
			return "qt5"
		}
	}
	return ""
}

// Plugin directories relative to a process's root (the host, or a
// Flatpak runtime seen through /proc/PID/root).
var pluginDirs = map[string][]string{
	"qt6": {"usr/lib64/qt6/plugins", "usr/lib/qt6/plugins", "usr/lib/x86_64-linux-gnu/qt6/plugins", "usr/lib/aarch64-linux-gnu/qt6/plugins", "usr/lib/plugins"},
	"qt5": {"usr/lib64/qt5/plugins", "usr/lib/qt5/plugins", "usr/lib/x86_64-linux-gnu/qt5/plugins", "usr/lib/aarch64-linux-gnu/qt5/plugins", "usr/lib/plugins"},
}

// Plugin file names of the Adwaita decoration: Qt 6.8+ ships it in
// qtwayland (libadwaita.so); QAdwaitaDecorations for Qt 5 is
// libqadwaitadecorations.so.
var pluginFiles = []string{"libadwaita.so", "libqadwaitadecorations.so"}

// HasAdwaitaPlugin reports whether the Adwaita decoration plugin of a Qt
// version is visible from the process's root (procRoot is
// /proc/PID/root, or "/" in tests).
func HasAdwaitaPlugin(procRoot, qt string) bool {
	for _, d := range pluginDirs[qt] {
		for _, f := range pluginFiles {
			if _, err := os.Stat(filepath.Join(procRoot, d, "wayland-decoration-client", f)); err == nil {
				return true
			}
		}
	}
	return false
}

// decorationsDisabled reports whether the process runs with Qt's window
// decorations turned off (then it would have no title bar at all in CSD).
func decorationsDisabled(root string, pid int) bool {
	b, err := os.ReadFile(filepath.Join(root, "proc", strconv.Itoa(pid), "environ"))
	if err != nil {
		return false
	}
	for _, kv := range strings.Split(string(b), "\x00") {
		if kv == "QT_WAYLAND_DISABLE_WINDOWDECORATION=1" {
			return true
		}
	}
	return false
}

// WantClientSide reports whether the shell should ask the window of
// process pid to draw its own decorations, and why. root is "/" (tests
// use a fake tree).
func WantClientSide(root string, pid int) (bool, string) {
	if pid <= 0 {
		return false, ""
	}
	qt := Toolkit(root, pid)
	if qt == "" {
		return false, ""
	}
	if decorationsDisabled(root, pid) {
		return false, qt + " with decorations disabled"
	}
	procRoot := filepath.Join(root, "proc", strconv.Itoa(pid), "root")
	if root != "/" {
		procRoot = root
	}
	if !HasAdwaitaPlugin(procRoot, qt) {
		return false, qt + " without the Adwaita decoration plugin"
	}
	return true, qt + " with the Adwaita decoration plugin"
}

// ButtonLayout is the GTK title bar button layout for a compositor:
// minimize and maximize only where the compositor honors them (a button
// that does nothing is worse than none: the shell offers both through
// the panel's window list, the window menu and keys everywhere).
func ButtonLayout(maximize, minimize bool) string {
	b := []string{}
	if minimize {
		b = append(b, "minimize")
	}
	if maximize {
		b = append(b, "maximize")
	}
	return "appmenu:" + strings.Join(append(b, "close"), ",")
}
