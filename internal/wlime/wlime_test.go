package wlime

import (
	"encoding/binary"
	"net"
	"path/filepath"
	"testing"
	"time"
)

// fakeCompositor speaks just enough Wayland: the registry with a seat and
// the input method manager, the sync callback, and then lets the test
// send input method events and read the client's requests.
type fakeCompositor struct {
	t    *testing.T
	conn net.Conn
}

func (f *fakeCompositor) send(obj uint32, op uint16, body []byte) {
	msg := make([]byte, 8+len(body))
	binary.LittleEndian.PutUint32(msg, obj)
	binary.LittleEndian.PutUint32(msg[4:], uint32(len(msg))<<16|uint32(op))
	copy(msg[8:], body)
	if _, err := f.conn.Write(msg); err != nil {
		f.t.Fatal(err)
	}
}

func (f *fakeCompositor) read() (uint32, uint16, []byte) {
	hdr := make([]byte, 8)
	_ = f.conn.SetReadDeadline(time.Now().Add(3 * time.Second))
	if _, err := ioFull(f.conn, hdr); err != nil {
		f.t.Fatal(err)
	}
	w := binary.LittleEndian.Uint32(hdr[4:])
	body := make([]byte, int(w>>16)-8)
	if _, err := ioFull(f.conn, body); err != nil {
		f.t.Fatal(err)
	}
	return binary.LittleEndian.Uint32(hdr), uint16(w & 0xffff), body
}

func ioFull(c net.Conn, b []byte) (int, error) {
	n := 0
	for n < len(b) {
		k, err := c.Read(b[n:])
		n += k
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func TestInputMethod(t *testing.T) {
	path := filepath.Join(t.TempDir(), "wayland-0")
	l, err := net.Listen("unix", path)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	fcCh := make(chan *fakeCompositor, 1)
	go func() {
		c, err := l.Accept()
		if err != nil {
			return
		}
		f := &fakeCompositor{t: t, conn: c}
		// get_registry (id 2) and sync (id 3).
		f.read()
		f.read()
		f.send(2, 0, cat(u32(1), wstr("wl_seat"), u32(7)))
		f.send(2, 0, cat(u32(2), wstr("zwp_input_method_manager_v2"), u32(1)))
		f.send(3, 0, u32(0))
		// bind seat (4), bind manager (5), get_input_method (6).
		f.read()
		f.read()
		f.read()
		fcCh <- f
	}()
	im, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	defer im.Close()
	f := <-fcCh
	changed := make(chan State, 8)
	im.mu.Lock()
	im.OnChange = func(s State) { changed <- s }
	im.mu.Unlock()
	wait := func() State {
		select {
		case s := <-changed:
			return s
		case <-time.After(3 * time.Second):
			t.Fatal("no state change")
		}
		return State{}
	}

	// A password field: active, sensitive, nothing may be typed.
	f.send(6, evActivate, nil)
	f.send(6, evContentType, cat(u32(0), u32(PurposePassword)))
	f.send(6, evDone, nil)
	st := wait()
	if !st.Active || !st.Sensitive() {
		t.Fatalf("password field: %+v", st)
	}
	if err := im.Commit(st.Gen, "secret"); err == nil {
		t.Error("typed into a password field")
	}

	// A normal field: pre-edit, then commit, both for this field only.
	f.send(6, evDeactivate, nil)
	f.send(6, evDone, nil)
	wait()
	f.send(6, evActivate, nil)
	f.send(6, evContentType, cat(u32(0), u32(PurposeNormal)))
	f.send(6, evDone, nil)
	st = wait()
	if !st.Active || st.Sensitive() || st.Gen != 2 {
		t.Fatalf("text field: %+v", st)
	}
	if err := im.Commit(st.Gen-1, "old field"); err != ErrFieldChanged {
		t.Errorf("commit into an older field: %v", err)
	}
	if err := im.Preedit(st.Gen, "hello"); err != nil {
		t.Fatal(err)
	}
	if obj, op, body := f.read(); obj != 6 || op != 1 {
		t.Errorf("preedit: %d %d", obj, op)
	} else if s, _ := str(body); s != "hello" {
		t.Errorf("preedit text %q", s)
	}
	if _, op, body := f.read(); op != 3 || binary.LittleEndian.Uint32(body) != 3 {
		t.Errorf("commit serial: op %d serial %d (want 3 done events)", op, binary.LittleEndian.Uint32(body))
	}
	if err := im.Commit(st.Gen, "hello"); err != nil {
		t.Fatal(err)
	}
	f.read() // pre-edit cleared
	if _, op, body := f.read(); op != 0 {
		t.Errorf("commit_string op %d", op)
	} else if s, _ := str(body); s != "hello" {
		t.Errorf("committed %q", s)
	}
}
