// Package sway is the sway / SwayFX backend: the i3-compatible IPC
// (binary header "i3-ipc" + little-endian length and type, JSON payload)
// on the socket named by $SWAYSOCK.
package sway

import (
	"bufio"
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Message types (sway-ipc(7)).
const (
	msgRunCommand    = 0
	msgGetWorkspaces = 1
	msgSubscribe     = 2
	msgGetOutputs    = 3
	msgGetTree       = 4
	msgGetVersion    = 7
)

var magic = []byte("i3-ipc")

// conn is one IPC connection. Requests are serialized: the protocol has
// no request ids, replies come in order.
type conn struct {
	mu sync.Mutex
	c  net.Conn
	r  *bufio.Reader
}

func dial(path string) (*conn, error) {
	c, err := net.DialTimeout("unix", path, 2*time.Second)
	if err != nil {
		return nil, err
	}
	return &conn{c: c, r: bufio.NewReader(c)}, nil
}

func (c *conn) close() error { return c.c.Close() }

func (c *conn) write(typ uint32, payload []byte) error {
	hdr := make([]byte, 14)
	copy(hdr, magic)
	binary.LittleEndian.PutUint32(hdr[6:], uint32(len(payload)))
	binary.LittleEndian.PutUint32(hdr[10:], typ)
	if _, err := c.c.Write(append(hdr, payload...)); err != nil {
		return err
	}
	return nil
}

func (c *conn) read() (uint32, []byte, error) {
	hdr := make([]byte, 14)
	if _, err := io.ReadFull(c.r, hdr); err != nil {
		return 0, nil, err
	}
	if string(hdr[:6]) != string(magic) {
		return 0, nil, errors.New("sway ipc: bad magic")
	}
	n := binary.LittleEndian.Uint32(hdr[6:])
	typ := binary.LittleEndian.Uint32(hdr[10:])
	if n > 64<<20 {
		return 0, nil, fmt.Errorf("sway ipc: reply too large (%d bytes)", n)
	}
	buf := make([]byte, n)
	if _, err := io.ReadFull(c.r, buf); err != nil {
		return 0, nil, err
	}
	return typ, buf, nil
}

// request sends one message and decodes the reply into out.
func (c *conn) request(typ uint32, payload []byte, out any) error {
	c.mu.Lock()
	defer c.mu.Unlock()
	_ = c.c.SetDeadline(time.Now().Add(5 * time.Second))
	defer func() { _ = c.c.SetDeadline(time.Time{}) }()
	if err := c.write(typ, payload); err != nil {
		return err
	}
	for {
		rt, body, err := c.read()
		if err != nil {
			return err
		}
		if rt&0x80000000 != 0 {
			continue // an event on a request connection: ignore
		}
		if rt != typ {
			return fmt.Errorf("sway ipc: reply type %d for request %d", rt, typ)
		}
		if out == nil {
			return nil
		}
		return json.Unmarshal(body, out)
	}
}

// commandResult is one entry of a RUN_COMMAND reply.
type commandResult struct {
	Success bool   `json:"success"`
	Error   string `json:"error"`
}
