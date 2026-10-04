package decor

import (
	"os"
	"path/filepath"
	"testing"
)

func write(t *testing.T, path, body string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestWantClientSide(t *testing.T) {
	root := t.TempDir()
	qt6 := "7f00-7f10 r-xp 0 fd:00 1 /usr/lib64/libQt6WaylandClient.so.6.11.2\n"
	write(t, filepath.Join(root, "proc/10/maps"), qt6)
	write(t, filepath.Join(root, "proc/10/environ"), "HOME=/home/a\x00QT_QPA_PLATFORM=wayland\x00")
	write(t, filepath.Join(root, "proc/11/maps"), "7f00-7f10 r-xp 0 fd:00 1 /usr/lib64/libgtk-4.so.1\n")
	write(t, filepath.Join(root, "proc/12/maps"), qt6)
	write(t, filepath.Join(root, "proc/12/environ"), "QT_WAYLAND_DISABLE_WINDOWDECORATION=1\x00")

	if ok, why := WantClientSide(root, 10); ok {
		t.Errorf("Qt 6 without the plugin switched to CSD (%s)", why)
	}
	write(t, filepath.Join(root, "usr/lib64/qt6/plugins/wayland-decoration-client/libadwaita.so"), "")
	if ok, why := WantClientSide(root, 10); !ok {
		t.Errorf("Qt 6 with the plugin kept server-side (%s)", why)
	}
	if ok, _ := WantClientSide(root, 11); ok {
		t.Error("GTK 4 process switched (it chooses CSD itself)")
	}
	if ok, _ := WantClientSide(root, 12); ok {
		t.Error("Qt with decorations disabled switched to CSD (no title bar at all)")
	}
	if ok, _ := WantClientSide(root, 99); ok {
		t.Error("unknown process switched")
	}
}

func TestButtonLayout(t *testing.T) {
	if ButtonLayout(false, false) != "appmenu:close" || ButtonLayout(true, false) != "appmenu:maximize,close" || ButtonLayout(true, true) != "appmenu:minimize,maximize,close" {
		t.Error("button layouts")
	}
}
