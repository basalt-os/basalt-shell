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

	"github.com/openbasalt/basalt-shell/internal/compositor"
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
					reply = `{"id":1,"type":"root","nodes":[{"id":2,"type":"output","name":"Virtual-1","nodes":[{"id":5,"type":"workspace","name":"1","nodes":[{"id":10,"type":"con","name":"term","app_id":"foot","pid":42,"focused":true,"rect":{"x":0,"y":0,"width":800,"height":600},"nodes":[],"floating_nodes":[]}],"floating_nodes":[{"id":11,"type":"floating_con","name":"xeyes","app_id":null,"pid":43,"shell":"xwayland","window":4194307,"window_properties":{"class":"XEyes"},"rect":{"x":100,"y":100,"width":200,"height":200},"nodes":[]}]}]}]}`
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
	if err != nil || len(ws) != 2 {
		t.Fatalf("windows %v %+v", err, ws)
	}
	if ws[1].AppID != "XEyes" || !ws[1].XWayland || !ws[1].Floating || ws[0].Workspace != "5" {
		t.Fatalf("parse: %+v", ws)
	}
	spaces, _ := a.Workspaces(ctx)
	if len(spaces) != 1 || spaces[0].Windows != 2 {
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
	_ = json.Valid
}
