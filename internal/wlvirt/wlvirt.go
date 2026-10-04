// Package wlvirt is a minimal Wayland client (no third-party code, no
// cgo) that holds a virtual keyboard (zwp_virtual_keyboard_v1) and a
// virtual pointer (zwlr_virtual_pointer_v1) on the compositor's seat.
//
// The shell daemon uses it for the agents' last-resort input inside a
// control session: absolute pointer moves, buttons and scrolling on any
// wlroots compositor, and a keyboard that stays on the seat. On a
// headless session (no input devices at all) the seat has no keyboard or
// pointer capability, so clients never bind wl_keyboard or wl_pointer and
// short-lived tools (wtype) type into nothing; holding these devices for
// the whole session fixes that.
package wlvirt

import (
	"encoding/binary"
	"errors"
	"fmt"
	"math"
	"net"
	"os"
	"path/filepath"
	"sync"
	"syscall"
	"time"
)

// Device is one connection with a virtual keyboard and pointer.
type Device struct {
	conn *net.UnixConn

	mu     sync.Mutex
	next   uint32
	kb     uint32
	ptr    uint32
	err    error
	start  time.Time
	closed bool
}

// Socket returns the Wayland socket path of the environment.
func Socket() (string, error) {
	d := os.Getenv("WAYLAND_DISPLAY")
	if d == "" {
		return "", errors.New("WAYLAND_DISPLAY is not set")
	}
	if filepath.IsAbs(d) {
		return d, nil
	}
	rt := os.Getenv("XDG_RUNTIME_DIR")
	if rt == "" {
		return "", errors.New("XDG_RUNTIME_DIR is not set")
	}
	return filepath.Join(rt, d), nil
}

// keymap is compiled by the compositor (xkbcommon resolves the includes).
const keymap = `xkb_keymap {
	xkb_keycodes { include "evdev+aliases(qwerty)" };
	xkb_types { include "complete" };
	xkb_compat { include "complete" };
	xkb_symbols { include "pc+us+inet(evdev)" };
};
`

type global struct {
	name    uint32
	iface   string
	version uint32
}

// Open connects to the Wayland socket at path and creates the devices.
func Open(path string) (*Device, error) {
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	d := &Device{conn: c, next: 2, start: time.Now()}
	fail := func(err error) (*Device, error) {
		c.Close()
		return nil, err
	}
	reg := d.newID()
	d.send(1, 1, u32(reg), nil) // wl_display.get_registry
	cb := d.newID()
	d.send(1, 0, u32(cb), nil) // wl_display.sync
	globals := map[string]global{}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for done := false; !done; {
		obj, op, body, err := d.read()
		if err != nil {
			return fail(fmt.Errorf("wayland: %w", err))
		}
		switch {
		case obj == 1 && op == 0:
			return fail(fmt.Errorf("wayland error: %s", displayError(body)))
		case obj == reg && op == 0:
			name := binary.LittleEndian.Uint32(body)
			iface, rest := str(body[4:])
			if len(rest) >= 4 {
				if _, ok := globals[iface]; !ok {
					globals[iface] = global{name, iface, binary.LittleEndian.Uint32(rest)}
				}
			}
		case obj == cb && op == 0:
			done = true
		}
	}
	_ = c.SetReadDeadline(time.Time{})
	seat, ok1 := globals["wl_seat"]
	vkm, ok2 := globals["zwp_virtual_keyboard_manager_v1"]
	vpm, ok3 := globals["zwlr_virtual_pointer_manager_v1"]
	if !ok1 || !ok2 || !ok3 {
		return fail(errors.New("the compositor lacks wl_seat, zwp_virtual_keyboard_manager_v1 or zwlr_virtual_pointer_manager_v1"))
	}
	seatID := d.bind(reg, seat, 1)
	vkmID := d.bind(reg, vkm, 1)
	vpmID := d.bind(reg, vpm, 1)
	d.kb = d.newID()
	d.send(vkmID, 0, cat(u32(seatID), u32(d.kb)), nil) // create_virtual_keyboard
	d.ptr = d.newID()
	d.send(vpmID, 0, cat(u32(seatID), u32(d.ptr)), nil) // create_virtual_pointer
	fd, size, err := keymapFD()
	if err != nil {
		return fail(err)
	}
	d.send(d.kb, 0, cat(u32(1), u32(uint32(size))), syscall.UnixRights(fd)) // keymap: XKB_V1, fd, size
	syscall.Close(fd)
	if d.err != nil {
		return fail(d.err)
	}
	// Events (seat capabilities, errors) are read and kept until Close.
	go d.drain()
	// Let the compositor process the requests before the first input.
	time.Sleep(50 * time.Millisecond)
	return d, d.Err()
}

func (d *Device) drain() {
	for {
		obj, op, body, err := d.read()
		if err != nil {
			d.mu.Lock()
			if d.err == nil && !d.closed {
				d.err = fmt.Errorf("wayland connection closed: %v", err)
			}
			d.mu.Unlock()
			return
		}
		if obj == 1 && op == 0 {
			d.mu.Lock()
			d.err = fmt.Errorf("wayland error: %s", displayError(body))
			d.mu.Unlock()
		}
	}
}

// Err reports a protocol error or a closed connection.
func (d *Device) Err() error {
	d.mu.Lock()
	defer d.mu.Unlock()
	return d.err
}

// Close destroys the devices and the connection.
func (d *Device) Close() error {
	d.mu.Lock()
	if d.closed {
		d.mu.Unlock()
		return nil
	}
	d.closed = true
	d.mu.Unlock()
	d.send(d.kb, 3, nil, nil)  // zwp_virtual_keyboard_v1.destroy
	d.send(d.ptr, 8, nil, nil) // zwlr_virtual_pointer_v1.destroy
	return d.conn.Close()
}

func (d *Device) now() uint32 { return uint32(time.Since(d.start).Milliseconds()) }

// MoveAbsolute puts the pointer at x, y of a layout of size w x h.
func (d *Device) MoveAbsolute(x, y, w, h int) error {
	if w <= 0 || h <= 0 || x < 0 || y < 0 || x > w || y > h {
		return fmt.Errorf("position %d,%d outside the screens (%dx%d)", x, y, w, h)
	}
	d.send(d.ptr, 1, cat(u32(d.now()), u32(uint32(x)), u32(uint32(y)), u32(uint32(w)), u32(uint32(h))), nil)
	d.send(d.ptr, 4, nil, nil) // frame
	return d.Err()
}

// Linux input button codes.
var Buttons = map[string]uint32{"left": 0x110, "right": 0x111, "middle": 0x112}

// Button presses or releases a button.
func (d *Device) Button(button string, pressed bool) error {
	code, ok := Buttons[button]
	if !ok {
		return fmt.Errorf("unknown button %q", button)
	}
	state := uint32(0)
	if pressed {
		state = 1
	}
	d.send(d.ptr, 2, cat(u32(d.now()), u32(code), u32(state)), nil)
	d.send(d.ptr, 4, nil, nil)
	return d.Err()
}

// Scroll scrolls by wheel steps (dy down, dx right).
func (d *Device) Scroll(dx, dy int) error {
	axis := func(a uint32, n int) {
		if n == 0 {
			return
		}
		d.send(d.ptr, 5, u32(0), nil) // axis_source wheel
		d.send(d.ptr, 7, cat(u32(d.now()), u32(a), fixed(float64(n)*15), u32(uint32(int32(n)))), nil)
		d.send(d.ptr, 4, nil, nil)
	}
	axis(0, dy)
	axis(1, dx)
	return d.Err()
}

// Key presses or releases an evdev key code with the virtual keyboard.
func (d *Device) Key(code uint32, pressed bool) error {
	state := uint32(0)
	if pressed {
		state = 1
	}
	d.send(d.kb, 1, cat(u32(d.now()), u32(code), u32(state)), nil)
	return d.Err()
}

// --- wire format ---

func (d *Device) newID() uint32 {
	id := d.next
	d.next++
	return id
}

func (d *Device) bind(reg uint32, g global, version uint32) uint32 {
	if g.version < version {
		version = g.version
	}
	id := d.newID()
	d.send(reg, 0, cat(u32(g.name), wstr(g.iface), u32(version), u32(id)), nil)
	return id
}

func (d *Device) send(obj uint32, op uint16, body []byte, oob []byte) {
	msg := make([]byte, 8+len(body))
	binary.LittleEndian.PutUint32(msg[0:], obj)
	binary.LittleEndian.PutUint32(msg[4:], uint32(len(msg))<<16|uint32(op))
	copy(msg[8:], body)
	d.mu.Lock()
	defer d.mu.Unlock()
	if d.err != nil {
		return
	}
	_ = d.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, _, err := d.conn.WriteMsgUnix(msg, oob, nil); err != nil {
		d.err = err
	}
}

func (d *Device) read() (obj uint32, op uint16, body []byte, err error) {
	hdr := make([]byte, 8)
	if _, err = ioReadFull(d.conn, hdr); err != nil {
		return
	}
	obj = binary.LittleEndian.Uint32(hdr)
	w := binary.LittleEndian.Uint32(hdr[4:])
	op = uint16(w & 0xffff)
	size := int(w >> 16)
	if size < 8 {
		return 0, 0, nil, errors.New("bad message size")
	}
	body = make([]byte, size-8)
	_, err = ioReadFull(d.conn, body)
	return
}

func ioReadFull(c *net.UnixConn, b []byte) (int, error) {
	n := 0
	for n < len(b) {
		m, err := c.Read(b[n:])
		n += m
		if err != nil {
			return n, err
		}
	}
	return n, nil
}

func u32(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}

func fixed(v float64) []byte { return u32(uint32(int32(math.Round(v * 256)))) }

func wstr(s string) []byte {
	n := len(s) + 1
	b := make([]byte, 4+(n+3)/4*4)
	binary.LittleEndian.PutUint32(b, uint32(n))
	copy(b[4:], s)
	return b
}

func str(b []byte) (string, []byte) {
	if len(b) < 4 {
		return "", nil
	}
	n := int(binary.LittleEndian.Uint32(b))
	pad := (n + 3) / 4 * 4
	if n == 0 || 4+pad > len(b) {
		return "", nil
	}
	return string(b[4 : 4+n-1]), b[4+pad:]
}

func cat(parts ...[]byte) []byte {
	var out []byte
	for _, p := range parts {
		out = append(out, p...)
	}
	return out
}

func displayError(body []byte) string {
	if len(body) < 8 {
		return "unknown"
	}
	msg, _ := str(body[8:])
	return fmt.Sprintf("object %d code %d: %s", binary.LittleEndian.Uint32(body), binary.LittleEndian.Uint32(body[4:]), msg)
}

// keymapFD returns a file with the keymap (NUL terminated), already
// unlinked, for the compositor to map.
func keymapFD() (int, int, error) {
	data := append([]byte(keymap), 0)
	dir := os.Getenv("XDG_RUNTIME_DIR")
	f, err := os.CreateTemp(dir, ".basalt-keymap-*")
	if err != nil {
		return -1, 0, err
	}
	defer f.Close()
	_ = os.Remove(f.Name())
	if _, err := f.Write(data); err != nil {
		return -1, 0, err
	}
	fd, err := syscall.Dup(int(f.Fd()))
	if err != nil {
		return -1, 0, err
	}
	return fd, len(data), nil
}
