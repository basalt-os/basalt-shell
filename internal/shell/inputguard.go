package shell

import (
	"context"
	"fmt"
	"strings"

	"github.com/basalt-os/basalt-shell/internal/compositor"
)

// Where an agent's synthetic input may go. A control session lets an
// agent type and click as the person, outside its own SELinux domain, its
// egress allowlist and the approval gate. Terminals and the system's own
// tools would turn that into anything the person can do (a shell command,
// a cached sudo, a change of the agent's own permissions), so input is
// refused while one of them has the keyboard focus or lies under the
// pointer target. The check fails closed: when the focused window cannot
// be identified (no window has the focus, the compositor does not answer,
// the window does not name its app), input is refused too.

// inputBlockedApps are app ids (compared in lower case) that never receive
// an agent's synthetic input: terminals, and tools that change the system,
// its security settings or the agents' own permissions.
var inputBlockedApps = map[string]bool{
	// Terminals.
	"foot": true, "footclient": true, "org.codeberg.dnkl.foot": true,
	"kitty": true, "alacritty": true, "org.alacritty.alacritty": true,
	"org.wezfurlong.wezterm": true, "com.mitchellh.ghostty": true,
	"org.gnome.ptyxis": true, "app.devsuite.ptyxis": true, "ptyxis": true,
	"org.gnome.console": true, "kgx": true, "org.gnome.terminal": true, "gnome-terminal-server": true,
	"org.kde.konsole": true, "konsole": true, "xterm": true, "uxterm": true, "urxvt": true,
	"tilix": true, "com.gexperts.tilix": true, "terminator": true, "xfce4-terminal": true,
	"st": true, "st-256color": true, "cotty": true, "org.openbasalt.cotty": true,
	// The desktop's and the system's settings (they hold the agents' permissions).
	"org.basalt.settings": true, "gnome-control-center": true, "org.gnome.settings": true,
	"org.kde.systemsettings": true, "systemsettings": true,
	// Firewall, disks, packages, virtual machines, the installer.
	"firewall-config": true, "org.fedoraproject.firewallconfig": true,
	"gparted": true, "org.gnome.disks": true, "gnome-disks": true,
	"org.gnome.software": true, "virt-manager": true, "basalt-installer": true,
	// Password managers and key stores.
	"org.keepassxc.keepassxc": true, "keepassxc": true, "1password": true, "bitwarden": true,
	"com.bitwarden.desktop": true, "org.gnome.seahorse.application": true, "seahorse": true,
}

// inputBlockedWords catch terminals and administration tools that are not
// in the list by name (an app id or X11 class that contains one of them).
var inputBlockedWords = []string{"term", "console", "konsole", "shell", "sudo", "polkit", "pkexec", "root"}

// inputBlockedApp reports why an app id may not receive an agent's input,
// or "" when it may.
func inputBlockedApp(appID string) string {
	id := strings.ToLower(strings.TrimSpace(appID))
	if id == "" {
		return "a window that does not name its app"
	}
	if inputBlockedApps[id] {
		return fmt.Sprintf("%s (a terminal or a system tool)", appID)
	}
	for _, w := range inputBlockedWords {
		if strings.Contains(id, w) {
			return fmt.Sprintf("%s (a terminal or a system tool)", appID)
		}
	}
	return ""
}

// inputTargetRefused reports why the agent's next input step may not run
// where it would land, or "" when it may: the focused window, and for
// pointer steps with a position every window under that position on a
// visible workspace.
func (c *Core) inputTargetRefused(ctx context.Context, in InputRequest) string {
	wins, err := c.Comp.Windows(ctx)
	if err != nil {
		return "the focused window cannot be identified (" + err.Error() + ")"
	}
	var focused *compositor.Window
	for i := range wins {
		if wins[i].Focused {
			focused = &wins[i]
			break
		}
	}
	if focused == nil {
		return "no app window has the keyboard focus"
	}
	if why := inputBlockedApp(focused.AppID); why != "" {
		return "the focused window is " + why
	}
	if in.X == nil || in.Y == nil || (in.Kind != "move" && in.Kind != "click" && in.Kind != "scroll") {
		return ""
	}
	// Windows on a hidden workspace or minimized are not under the
	// pointer. Without the workspace list every window counts.
	visible := map[string]bool{}
	spaces, werr := c.Comp.Workspaces(ctx)
	for _, s := range spaces {
		if s.Visible || s.Focused {
			visible[s.ID] = true
		}
	}
	x, y := *in.X, *in.Y
	for _, w := range wins {
		if w.State == "minimized" || (werr == nil && !visible[w.Workspace]) {
			continue
		}
		r := w.Rect
		if x < r.X || y < r.Y || x >= r.X+r.W || y >= r.Y+r.H {
			continue
		}
		if why := inputBlockedApp(w.AppID); why != "" {
			return fmt.Sprintf("the pointer would land on %s", why)
		}
	}
	return ""
}

// typeSegments splits text after each line break, so the focus is checked
// again before the text that follows a line (Return may open or focus
// another window).
func typeSegments(text string) []string {
	var segs []string
	for text != "" {
		i := strings.IndexAny(text, "\n\r")
		if i < 0 {
			segs = append(segs, text)
			break
		}
		segs = append(segs, text[:i+1])
		text = text[i+1:]
	}
	return segs
}
