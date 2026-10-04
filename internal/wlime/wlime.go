// Package wlime makes the shell daemon the session's input method
// (zwp_input_method_v2, the protocol behind text-input-v3), with a
// minimal Wayland client of its own (no third-party code, no cgo).
//
// Dictation uses it: when the person speaks while a text field has focus,
// the words go into that field as an input method's text, not as
// synthetic key presses. The compositor tells the input method when a
// text field is focused and what kind of field it is (a password, a PIN,
// a terminal), so the shell knows where the words would go before the
// microphone opens, and refuses to dictate into secret fields.
//
// The dictated text is first shown in the field as pre-edit text (the
// underlined, uncommitted text input methods use), and committed only
// when the person confirms in the shell; declining clears it.
//
// Only one input method can be bound on a seat. When another one (IBus,
// Fcitx) is running, the compositor answers "unavailable" and dictation
// is off; spoken requests still go to the assistant.
package wlime

import (
	"encoding/binary"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"sync"
	"time"
)

// Content purposes of text-input-v3 (zwp_text_input_v3.content_purpose).
const (
	PurposeNormal   = 0
	PurposePassword = 8
	PurposePin      = 9
	PurposeTerminal = 13
)

// Content hints (bit field).
const (
	HintHiddenText    = 0x40
	HintSensitiveData = 0x80
)

// State is what the compositor said about the focused text field.
type State struct {
	// Active: a text field has focus and accepts text from the input method.
	Active bool `json:"active"`
	// Purpose and Hint describe the field (text-input-v3 values).
	Purpose uint32 `json:"purpose"`
	Hint    uint32 `json:"hint"`
	// Gen changes every time a field is activated: text confirmed for one
	// field is never committed into another.
	Gen uint64 `json:"gen"`
	// Available is false when another input method owns the seat.
	Available bool `json:"available"`
}

// Sensitive reports a field for secrets (password, PIN, hidden or
// sensitive text): the shell never dictates into it.
func (s State) Sensitive() bool {
	return s.Purpose == PurposePassword || s.Purpose == PurposePin || s.Hint&(HintHiddenText|HintSensitiveData) != 0
}

// IM is one connection holding the seat's input method.
type IM struct {
	conn *net.UnixConn

	mu      sync.Mutex
	next    uint32
	im      uint32
	done    uint32 // "done" events received: the serial of commit
	pending State
	pendAct int // 1 activate, -1 deactivate, 0 none since the last done
	cur     State
	gen     uint64
	err     error
	closed  bool
	// OnChange is called (without the lock) after every applied state.
	OnChange func(State)
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

type global struct {
	name    uint32
	iface   string
	version uint32
}

// Open connects to the Wayland socket and becomes the seat's input method.
func Open(path string) (*IM, error) {
	c, err := net.DialUnix("unix", nil, &net.UnixAddr{Name: path, Net: "unix"})
	if err != nil {
		return nil, err
	}
	m := &IM{conn: c, next: 2}
	fail := func(err error) (*IM, error) {
		c.Close()
		return nil, err
	}
	reg := m.newID()
	m.send(1, 1, u32(reg)) // wl_display.get_registry
	cb := m.newID()
	m.send(1, 0, u32(cb)) // wl_display.sync
	globals := map[string]global{}
	_ = c.SetReadDeadline(time.Now().Add(5 * time.Second))
	for done := false; !done; {
		obj, op, body, err := m.read()
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
	mgr, ok2 := globals["zwp_input_method_manager_v2"]
	if !ok1 || !ok2 {
		return fail(errors.New("the compositor has no zwp_input_method_manager_v2"))
	}
	seatID := m.bind(reg, seat, 1)
	mgrID := m.bind(reg, mgr, 1)
	m.im = m.newID()
	m.send(mgrID, 0, cat(u32(seatID), u32(m.im))) // get_input_method
	m.cur.Available = true
	m.pending.Available = true
	if err := m.Err(); err != nil {
		return fail(err)
	}
	go m.loop()
	return m, nil
}

// State returns the focused field's state.
func (m *IM) State() State {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.cur
}

// Err reports a protocol error or a closed connection.
func (m *IM) Err() error {
	m.mu.Lock()
	defer m.mu.Unlock()
	return m.err
}

// Preedit shows text in the focused field as uncommitted pre-edit text,
// for the field generation gen only.
func (m *IM) Preedit(gen uint64, text string) error {
	if err := m.check(gen); err != nil {
		return err
	}
	n := int32(len(text))
	m.send(m.im, 1, cat(wstr(text), u32(uint32(n)), u32(uint32(n)))) // set_preedit_string
	m.commit()
	return m.Err()
}

// Commit types text into the focused field (replacing the pre-edit
// text), for the field generation gen only.
func (m *IM) Commit(gen uint64, text string) error {
	if err := m.check(gen); err != nil {
		return err
	}
	m.send(m.im, 1, cat(wstr(""), u32(0), u32(0))) // set_preedit_string("")
	m.send(m.im, 0, wstr(text))                    // commit_string
	m.commit()
	return m.Err()
}

// Clear removes the pre-edit text (the person declined), if the field is
// still the same.
func (m *IM) Clear(gen uint64) {
	if m.check(gen) != nil {
		return
	}
	m.send(m.im, 1, cat(wstr(""), u32(0), u32(0)))
	m.commit()
}

// ErrFieldChanged: the text field the words were meant for lost focus.
var ErrFieldChanged = errors.New("the text field lost focus")

func (m *IM) check(gen uint64) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return m.err
	}
	if !m.cur.Active || m.cur.Gen != gen {
		return ErrFieldChanged
	}
	if m.cur.Sensitive() {
		return errors.New("the focused field is for a secret")
	}
	return nil
}

func (m *IM) commit() {
	m.mu.Lock()
	serial := m.done
	m.mu.Unlock()
	m.send(m.im, 3, u32(serial)) // commit(serial)
}

// Close gives the input method up.
func (m *IM) Close() error {
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return nil
	}
	m.closed = true
	m.mu.Unlock()
	m.send(m.im, 6, nil) // destroy
	return m.conn.Close()
}

// Events of zwp_input_method_v2.
const (
	evActivate = iota
	evDeactivate
	evSurroundingText
	evTextChangeCause
	evContentType
	evDone
	evUnavailable
)

func (m *IM) loop() {
	for {
		obj, op, body, err := m.read()
		if err != nil {
			m.mu.Lock()
			if m.err == nil && !m.closed {
				m.err = fmt.Errorf("wayland connection closed: %v", err)
			}
			m.cur.Active = false
			m.mu.Unlock()
			return
		}
		if obj == 1 && op == 0 {
			m.mu.Lock()
			m.err = fmt.Errorf("wayland error: %s", displayError(body))
			m.mu.Unlock()
			continue
		}
		if obj != m.im {
			continue
		}
		var changed *State
		m.mu.Lock()
		switch op {
		case evActivate:
			// A new activation resets the pending state of the field.
			m.pendAct = 1
			m.pending = State{Available: true}
		case evDeactivate:
			m.pendAct = -1
		case evContentType:
			if len(body) >= 8 {
				m.pending.Hint = binary.LittleEndian.Uint32(body)
				m.pending.Purpose = binary.LittleEndian.Uint32(body[4:])
			}
		case evSurroundingText, evTextChangeCause:
			// The field's text is never read or kept.
		case evDone:
			m.done++
			switch m.pendAct {
			case 1:
				m.gen++
				m.cur = State{Active: true, Purpose: m.pending.Purpose, Hint: m.pending.Hint, Gen: m.gen, Available: true}
			case -1:
				m.cur = State{Gen: m.cur.Gen, Available: true}
			default:
				m.cur.Purpose, m.cur.Hint = m.pending.Purpose, m.pending.Hint
			}
			m.pendAct = 0
			st := m.cur
			changed = &st
		case evUnavailable:
			m.cur = State{Available: false}
			m.err = errors.New("another input method is running on this seat")
			st := m.cur
			changed = &st
		}
		cb := m.OnChange
		m.mu.Unlock()
		if changed != nil && cb != nil {
			cb(*changed)
		}
	}
}

// --- wire format (as in package wlvirt) ---

func (m *IM) newID() uint32 {
	m.mu.Lock()
	defer m.mu.Unlock()
	id := m.next
	m.next++
	return id
}

func (m *IM) bind(reg uint32, g global, version uint32) uint32 {
	if g.version < version {
		version = g.version
	}
	id := m.newID()
	m.send(reg, 0, cat(u32(g.name), wstr(g.iface), u32(version), u32(id)))
	return id
}

func (m *IM) send(obj uint32, op uint16, body []byte) {
	msg := make([]byte, 8+len(body))
	binary.LittleEndian.PutUint32(msg[0:], obj)
	binary.LittleEndian.PutUint32(msg[4:], uint32(len(msg))<<16|uint32(op))
	copy(msg[8:], body)
	m.mu.Lock()
	defer m.mu.Unlock()
	if m.err != nil {
		return
	}
	_ = m.conn.SetWriteDeadline(time.Now().Add(2 * time.Second))
	if _, err := m.conn.Write(msg); err != nil {
		m.err = err
	}
}

func (m *IM) read() (obj uint32, op uint16, body []byte, err error) {
	hdr := make([]byte, 8)
	if err = readFull(m.conn, hdr); err != nil {
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
	err = readFull(m.conn, body)
	return
}

func readFull(c *net.UnixConn, b []byte) error {
	n := 0
	for n < len(b) {
		k, err := c.Read(b[n:])
		n += k
		if err != nil {
			return err
		}
	}
	return nil
}

func u32(v uint32) []byte {
	b := make([]byte, 4)
	binary.LittleEndian.PutUint32(b, v)
	return b
}

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
