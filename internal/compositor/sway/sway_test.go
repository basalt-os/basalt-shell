package sway

import (
	"context"
	"encoding/binary"
	"encoding/json"
	"io"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/basalt-os/basalt-shell/internal/compositor"
)

// fakeSway answers the i3 IPC with canned replies and records commands.
type fakeSway struct {
	mu   sync.Mutex
	cmds []string
}

func (f *fakeSway) serve(t *testing.T, l net.Listener) {
	for {
		c, err := l.Accept()
		if err != nil {
			return
		}
		go func(c net.Conn) {
			defer c.Close()
			for {
				hdr := make([]byte, 14)
				if _, err := io.ReadFull(c, hdr); err != nil {
					return
				}
				n := binary.LittleEndian.Uint32(hdr[6:])
				typ := binary.LittleEndian.Uint32(hdr[10:])
				body := make([]byte, n)
				io.ReadFull(c, body)
				var reply string
				switch typ {
				case msgRunCommand:
					f.mu.Lock()
					f.cmds = append(f.cmds, string(body))
					f.mu.Unlock()
					if strings.HasPrefix(string(body), "corner_radius") {
						reply = `[{"success":false,"error":"Unknown/invalid command"}]`
					} else {
						reply = `[{"success":true}]`
					}
				case msgGetVersion:
					reply = `{"human_readable":"1.11"}`
				case msgGetWorkspaces:
					reply = `[{"id":5,"num":1,"name":"1","output":"Virtual-1","focused":true,"visible":true}]`
				case msgGetOutputs:
					reply = `[{"name":"Virtual-1","active":true,"focused":true,"scale":1,"rect":{"x":0,"y":0,"width":1920,"height":1080}}]`
				case msgGetTree:
					reply = `{"id":1,"type":"root","nodes":[{"id":3,"type":"output","name":"__i3","nodes":[{"id":4,"type":"workspace","name":"__i3_scratch","nodes":[],"floating_nodes":[{"id":12,"type":"floating_con","name":"notes","app_id":"org.gnome.TextEditor","pid":44,"shell":"xdg_shell","border":"csd","rect":{"x":0,"y":0,"width":600,"height":400},"nodes":[]}]}]},{"id":2,"type":"output","name":"Virtual-1","nodes":[{"id":5,"type":"workspace","name":"1","rect":{"x":0,"y":44,"width":1920,"height":1036},"nodes":[{"id":10,"type":"con","name":"term","app_id":"foot","pid":42,"focused":true,"border":"normal","rect":{"x":0,"y":0,"width":800,"height":600},"nodes":[],"floating_nodes":[]}],"floating_nodes":[{"id":11,"type":"floating_con","name":"xeyes","app_id":null,"pid":43,"shell":"xwayland","border":"pixel","window":4194307,"window_properties":{"class":"XEyes"},"rect":{"x":100,"y":100,"width":200,"height":200},"nodes":[]},{"id":13,"type":"floating_con","name":"Firefox","app_id":"firefox","pid":45,"shell":"xdg_shell","border":"csd","rect":{"x":300,"y":100,"width":900,"height":700},"nodes":[]}]}]}]}`
				}
				out := make([]byte, 14)
				copy(out, magic)
				binary.LittleEndian.PutUint32(out[6:], uint32(len(reply)))
				binary.LittleEndian.PutUint32(out[10:], typ)
				c.Write(append(out, reply...))
			}
		}(c)
	}
}

func TestSwayAdapter(t *testing.T) {
	p := filepath.Join(t.TempDir(), "sway.sock")
	l, err := net.Listen("unix", p)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	f := &fakeSway{}
	go f.serve(t, l)
	a := New(p)
	ctx := context.Background()
	if a.Name() != "sway" {
		t.Fatalf("name %s", a.Name())
	}
	ws, err := a.Windows(ctx)
	if err != nil || len(ws) != 4 {
		t.Fatalf("windows %v %+v", err, ws)
	}
	// Order: the scratchpad (minimized) first, then the output.
	if ws[0].State != "minimized" || ws[0].Workspace != "" || ws[0].Decoration != "client" {
		t.Fatalf("minimized window: %+v", ws[0])
	}
	if ws[2].AppID != "XEyes" || !ws[2].XWayland || !ws[2].Floating || ws[1].Workspace != "5" {
		t.Fatalf("parse: %+v", ws)
	}
	if ws[1].Decoration != "server" || ws[2].Decoration != "none" || ws[3].Decoration != "client" {
		t.Fatalf("decorations: %+v", ws)
	}
	spaces, _ := a.Workspaces(ctx)
	if len(spaces) != 1 || spaces[0].Windows != 3 {
		t.Fatalf("workspaces %+v", spaces)
	}
	if err := a.MoveResize(ctx, "10", compositor.Rect{X: 10, Y: 20, W: 640, H: 480}); err != nil {
		t.Fatal(err)
	}
	if err := a.Spawn(ctx, []string{"foot", "--title", "it's"}); err == nil {
		t.Fatal("quote accepted")
	}
	if err := a.Spawn(ctx, []string{"foot", "--title", "term"}); err != nil {
		t.Fatal(err)
	}
	style := compositor.Style{BorderWidth: 2, Gaps: 8, FocusColor: "#4a525c", InactiveColor: "#343b44",
		Title: compositor.TitleStyle{Font: "Inter SemiBold", Size: 10, Align: "center", PadX: 12, PadY: 6,
			FocusedBg: "#22272e", FocusedText: "#ece7e1", InactiveBg: "#181c21", InactiveTxt: "#a0a6ae", UrgentBg: "#e0605a", UrgentText: "#ffffff"}}
	if err := a.ApplyStyle(ctx, style); err != nil {
		t.Fatal(err)
	}
	// Maximized: no frame; a new width does not bring it back; restored,
	// the frame comes back at the current width. A CSD window is skipped.
	if err := a.SetFrame(ctx, "10", false); err != nil {
		t.Fatal(err)
	}
	if err := a.SetFrame(ctx, "13", false); err != nil {
		t.Fatal(err)
	}
	style.BorderWidth = 1
	if err := a.ApplyStyle(ctx, style); err != nil {
		t.Fatal(err)
	}
	if err := a.SetFrame(ctx, "10", true); err != nil {
		t.Fatal(err)
	}
	if err := a.Minimize(ctx, "10"); err != nil {
		t.Fatal(err)
	}
	if err := a.Focus(ctx, "10; exec rm"); err == nil {
		t.Fatal("bad id accepted")
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	joined := strings.Join(f.cmds, "\n")
	if !strings.Contains(joined, "[con_id=10] floating enable, resize set width 640 px height 480 px, move absolute position 10 px 20 px") {
		t.Errorf("move command: %s", joined)
	}
	if !strings.Contains(joined, `exec 'foot' '--title' 'term'`) {
		t.Errorf("exec quoting: %s", joined)
	}
	for _, want := range []string{
		"client.focused #4a525c #22272e #ece7e1 #4a525c #4a525c",
		"client.unfocused #343b44 #181c21 #a0a6ae #343b44 #343b44",
		"font pango:Inter SemiBold 10", "title_align center", "titlebar_padding 12 6",
		"default_border normal 2", "default_floating_border normal 2",
		"[con_id=10] border normal 2", "[con_id=11] border normal 2", "[con_id=10] move scratchpad",
	} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
	for _, want := range []string{"titlebar_border_thickness 0", "gaps inner all set 8", "gaps outer all set 0",
		"[con_id=10] border none", "[con_id=11] border normal 1", "[con_id=10] border normal 1"} {
		if !strings.Contains(joined, want) {
			t.Errorf("missing %q in:\n%s", want, joined)
		}
	}
	if i, j := strings.Index(joined, "[con_id=10] border none"), strings.LastIndex(joined, "[con_id=10] border normal 1"); j < i {
		t.Errorf("frame order: %s", joined)
	}
	if strings.Count(joined, "[con_id=10] border normal 1") != 1 {
		t.Errorf("the style change reframed a maximized window:\n%s", joined)
	}
	// SwayFX-only commands never reach plain sway (they would fail).
	for _, bad := range []string{"layer_effects", "smart_corner_radius", "titlebar_separator", "shadow_offset"} {
		if strings.Contains(joined, bad) {
			t.Errorf("%q sent to plain sway", bad)
		}
	}
	// Windows that draw their own decorations are never given a border.
	for _, bad := range []string{"[con_id=12] border", "[con_id=13] border", "[all] border"} {
		if strings.Contains(joined, bad) {
			t.Errorf("%q would turn off client-side decorations", bad)
		}
	}
	_ = json.Valid
}

// The panel's layer gets blur (only where it draws) with a GPU and
// nothing without; one effect per command, after a reset. The popovers'
// full-screen layers never get blur, shadows or corners.
func TestLayerEffects(t *testing.T) {
	on := strings.Join(layerEffects(true), "\n")
	for _, want := range []string{`layer_effects "basalt-panel" reset`, `layer_effects "basalt-panel" "blur enable"`,
		`layer_effects "basalt-panel" "blur_ignore_transparent enable"`} {
		if !strings.Contains(on, want) {
			t.Errorf("missing %q in:\n%s", want, on)
		}
	}
	if strings.Contains(on, "shadows") || strings.Contains(on, "corner_radius") || strings.Contains(on, "basalt-drawer") {
		t.Errorf("layer effects beyond the panel's blur:\n%s", on)
	}
	off := strings.Join(layerEffects(false), "\n")
	if strings.Contains(off, "enable") || !strings.Contains(off, `layer_effects "basalt-panel" reset`) {
		t.Errorf("without a GPU: %s", off)
	}
}
