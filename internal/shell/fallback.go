package shell

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"

	"github.com/basalt-os/basalt-shell/internal/assistant"
	"github.com/basalt-os/basalt-shell/internal/i18n"
)

// Settings, About: the GNOME fallback's login screen (release audit
// 2026-11, S15). GNOME, installed next to Basalt as a fallback
// (basalt-desktop-gnome-fallback), has no screen lock when greetd is the
// display manager. While the person uses GNOME, GDM should start instead;
// afterwards greetd again. That is a system change: the shell stores the
// assistant's desktop.fallback proposal and the person applies it with an
// administrator's password (or the approval gate), like the keyboard of
// the login screen. It takes effect at the next restart.

// Paths read for the state (tests move them).
var (
	displayManagerLink = "/etc/systemd/system/display-manager.service"
	gnomeSessionFile   = "/usr/share/wayland-sessions/gnome.desktop"
	gdmUnitFiles       = []string{"/usr/lib/systemd/system/gdm.service", "/etc/systemd/system/gdm.service"}
)

// FallbackInfo is what Settings shows about the fallback.
type FallbackInfo struct {
	// GNOME: the GNOME session is installed. GDM: its display manager.
	GNOME bool `json:"gnome"`
	GDM   bool `json:"gdm"`
	// DisplayManager: the unit display-manager.service points at
	// (greetd, gdm, or "" when none is set).
	DisplayManager string `json:"display_manager"`
	// Assistant: the system assistant can store the proposal.
	Assistant bool `json:"assistant"`
}

func readDisplayManager() string {
	t, err := os.Readlink(displayManagerLink)
	if err != nil {
		return ""
	}
	return strings.TrimSuffix(filepath.Base(t), ".service")
}

// FallbackState reports the fallback's state.
func (c *Core) FallbackState() FallbackInfo {
	fi := FallbackInfo{DisplayManager: readDisplayManager()}
	if _, err := os.Stat(gnomeSessionFile); err == nil {
		fi.GNOME = true
	}
	for _, p := range gdmUnitFiles {
		if _, err := os.Stat(p); err == nil {
			fi.GDM = true
		}
	}
	fi.Assistant = c.Assistant != nil && c.Assistant.Available()
	return fi
}

// FallbackProposal stores the assistant's desktop.fallback proposal for
// target (gnome: GDM as the display manager; basalt: greetd again).
func (c *Core) FallbackProposal(ctx context.Context, target string) (assistant.Proposal, error) {
	if target != "gnome" && target != "basalt" {
		return assistant.Proposal{}, errors.New("target must be gnome or basalt")
	}
	if c.Assistant == nil || !c.Assistant.Available() {
		return assistant.Proposal{}, errors.New(i18n.G("The system assistant (basalt) is not installed: changing the login screen needs it."))
	}
	fi := c.FallbackState()
	if target == "gnome" && (!fi.GNOME || !fi.GDM) {
		return assistant.Proposal{}, errors.New(i18n.G("GNOME and its login screen (GDM) are not installed. Install basalt-desktop-gnome-fallback first."))
	}
	p, err := c.Assistant.FallbackPropose(ctx, target)
	if err != nil && strings.Contains(err.Error(), `unknown command "fallback"`) {
		// An assistant older than basalt fallback.
		err = errors.New(i18n.G("The system assistant on this computer is older than this page. A coming update of Basalt OS brings it."))
	}
	data := map[string]any{"assistant_proposal": p.ID, "target": target, "display_manager": fi.DisplayManager}
	if err != nil {
		data["error"] = err.Error()
	}
	_, _ = c.Audit.Append("propose", "ui", "desktop.fallback", data)
	return p, err
}
