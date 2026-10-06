package sway

import (
	"net"
	"path/filepath"
	"testing"

	"github.com/basalt-os/basalt-shell/internal/compositor"
)

func TestFitRect(t *testing.T) {
	// A 1920x1080 output with a 44 px top panel: the usable area.
	area := compositor.Rect{X: 0, Y: 44, W: 1920, H: 1036}
	cases := []struct {
		name    string
		in, out compositor.Rect
		changed bool
	}{
		{"fits", compositor.Rect{X: 100, Y: 100, W: 800, H: 600}, compositor.Rect{X: 100, Y: 100, W: 800, H: 600}, false},
		{"whole output (IntelliJ IDEA)", compositor.Rect{X: 0, Y: 0, W: 1920, H: 1080}, compositor.Rect{X: 0, Y: 44, W: 1920, H: 1036}, true},
		{"under the panel", compositor.Rect{X: 200, Y: 10, W: 800, H: 600}, compositor.Rect{X: 200, Y: 44, W: 800, H: 600}, true},
		{"taller than the area, centered", compositor.Rect{X: 300, Y: -20, W: 1000, H: 1120}, compositor.Rect{X: 300, Y: 44, W: 1000, H: 1036}, true},
		{"off the right and bottom", compositor.Rect{X: 1500, Y: 900, W: 600, H: 400}, compositor.Rect{X: 1320, Y: 680, W: 600, H: 400}, true},
		{"no size yet", compositor.Rect{X: 0, Y: 0, W: 0, H: 0}, compositor.Rect{X: 0, Y: 0, W: 0, H: 0}, false},
	}
	for _, c := range cases {
		got, changed := fitRect(c.in, area)
		if got != c.out || changed != c.changed {
			t.Errorf("%s: fitRect(%+v) = %+v, %v; want %+v, %v", c.name, c.in, got, changed, c.out, c.changed)
		}
	}
}

func TestFloatingPlace(t *testing.T) {
	a, _ := newFakeAdapter(t)
	win, area, ok := a.floatingPlace(11)
	if !ok || win != (compositor.Rect{X: 100, Y: 100, W: 200, H: 200}) || area != (compositor.Rect{X: 0, Y: 44, W: 1920, H: 1036}) {
		t.Fatalf("floatingPlace(11) = %+v, %+v, %v", win, area, ok)
	}
	// Tiled and minimized windows are never moved.
	for _, id := range []int64{10, 12, 99} {
		if _, _, ok := a.floatingPlace(id); ok {
			t.Errorf("floatingPlace(%d) reported a floating window", id)
		}
	}
}

// newFakeAdapter starts the canned sway IPC server of sway_test.go.
func newFakeAdapter(t *testing.T) (*Adapter, *fakeSway) {
	t.Helper()
	p := filepath.Join(t.TempDir(), "sway.sock")
	l, err := net.Listen("unix", p)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	f := &fakeSway{}
	go f.serve(t, l)
	return New(p), f
}
