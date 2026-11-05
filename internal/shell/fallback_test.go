package shell

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

// The fallback's state follows display-manager.service and the installed
// files; a proposal needs the assistant and, for GNOME, GNOME and GDM.
func TestFallbackState(t *testing.T) {
	c, _, dir := newCore(t)
	oldLink, oldGnome, oldGdm := displayManagerLink, gnomeSessionFile, gdmUnitFiles
	t.Cleanup(func() { displayManagerLink, gnomeSessionFile, gdmUnitFiles = oldLink, oldGnome, oldGdm })
	displayManagerLink = filepath.Join(dir, "display-manager.service")
	gnomeSessionFile = filepath.Join(dir, "gnome.desktop")
	gdmUnitFiles = []string{filepath.Join(dir, "gdm.service")}
	fi := c.FallbackState()
	if fi.GNOME || fi.GDM || fi.DisplayManager != "" {
		t.Errorf("nothing installed: %+v", fi)
	}
	_ = os.Symlink("/usr/lib/systemd/system/greetd.service", displayManagerLink)
	_ = os.WriteFile(gnomeSessionFile, []byte("[Desktop Entry]\n"), 0o644)
	_ = os.WriteFile(gdmUnitFiles[0], []byte("[Unit]\n"), 0o644)
	fi = c.FallbackState()
	if !fi.GNOME || !fi.GDM || fi.DisplayManager != "greetd" {
		t.Errorf("installed: %+v", fi)
	}
	c.Assistant = nil
	if _, err := c.FallbackProposal(context.Background(), "gnome"); err == nil || !strings.Contains(err.Error(), "assistant") {
		t.Errorf("no assistant: %v", err)
	}
	if _, err := c.FallbackProposal(context.Background(), "kde"); err == nil {
		t.Error("an unknown target was accepted")
	}
}
