// Package greetd speaks greetd's IPC protocol (greetd-ipc(7)): JSON
// messages, each preceded by its length as a 32-bit integer in the host's
// byte order, over the Unix socket named by GREETD_SOCK. Requests are
// create_session, post_auth_message_response, start_session and
// cancel_session; every request gets exactly one response: success, error
// (error_type auth_error or error) or auth_message (a PAM question or
// notice: visible, secret, info, error).
//
// The login screen itself talks to greetd through Quickshell's own client;
// this package is the reference the greeter is tested against: a client
// (Client) and a fake greetd (Fake, fake.go) that answers like greetd 0.10,
// including its quirks, so the greeter's handling of them is covered by
// tests that need neither PAM nor a display.
package greetd

import (
	"encoding/binary"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"time"
)

// MaxMessage bounds a message's length; greetd's own messages are small.
const MaxMessage = 1 << 20

// Request is one message from the greeter to greetd.
type Request struct {
	Type     string   `json:"type"`
	Username string   `json:"username,omitempty"`
	Response *string  `json:"response,omitempty"`
	Cmd      []string `json:"cmd,omitempty"`
	Env      []string `json:"env,omitempty"`
}

// Response is one message from greetd to the greeter.
type Response struct {
	Type            string `json:"type"`
	ErrorType       string `json:"error_type,omitempty"`
	Description     string `json:"description,omitempty"`
	AuthMessageType string `json:"auth_message_type,omitempty"`
	AuthMessage     string `json:"auth_message,omitempty"`
}

// Request and response types.
const (
	CreateSession           = "create_session"
	PostAuthMessageResponse = "post_auth_message_response"
	StartSession            = "start_session"
	CancelSession           = "cancel_session"

	Success     = "success"
	Error       = "error"
	AuthMessage = "auth_message"

	ErrAuth  = "auth_error"
	ErrOther = "error"

	Visible  = "visible"
	Secret   = "secret"
	Info     = "info"
	ErrorMsg = "error"
)

// Write sends one message: its length, then its JSON.
func Write(w io.Writer, v any) error {
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	if len(b) > MaxMessage {
		return fmt.Errorf("greetd: message of %d bytes is too long", len(b))
	}
	buf := make([]byte, 4+len(b))
	binary.NativeEndian.PutUint32(buf, uint32(len(b)))
	copy(buf[4:], b)
	_, err = w.Write(buf)
	return err
}

// Read receives one message into v.
func Read(r io.Reader, v any) error {
	var hdr [4]byte
	if _, err := io.ReadFull(r, hdr[:]); err != nil {
		return err
	}
	n := binary.NativeEndian.Uint32(hdr[:])
	if n > MaxMessage {
		return fmt.Errorf("greetd: message of %d bytes is too long", n)
	}
	b := make([]byte, n)
	if _, err := io.ReadFull(r, b); err != nil {
		return err
	}
	return json.Unmarshal(b, v)
}

// Client is a connection to greetd.
type Client struct {
	conn net.Conn
}

// Dial connects to greetd's socket (GREETD_SOCK).
func Dial(path string) (*Client, error) {
	c, err := net.DialTimeout("unix", path, 5*time.Second)
	if err != nil {
		return nil, err
	}
	return &Client{conn: c}, nil
}

// Close closes the connection.
func (c *Client) Close() error { return c.conn.Close() }

func (c *Client) do(req Request) (Response, error) {
	var resp Response
	if err := Write(c.conn, req); err != nil {
		return resp, err
	}
	err := Read(c.conn, &resp)
	return resp, err
}

// CreateSession starts a login conversation for a user.
func (c *Client) CreateSession(user string) (Response, error) {
	return c.do(Request{Type: CreateSession, Username: user})
}

// Respond answers greetd's current auth message. Info and error notices
// are acknowledged with no answer (Respond(nil)).
func (c *Client) Respond(answer *string) (Response, error) {
	return c.do(Request{Type: PostAuthMessageResponse, Response: answer})
}

// StartSession asks greetd to start the authenticated session.
func (c *Client) StartSession(cmd, env []string) (Response, error) {
	return c.do(Request{Type: StartSession, Cmd: cmd, Env: env})
}

// Cancel ends the conversation being configured.
func (c *Client) Cancel() (Response, error) {
	return c.do(Request{Type: CancelSession})
}

// ErrUnexpected is returned by Login for a response it did not expect.
var ErrUnexpected = errors.New("greetd: unexpected response")

// Login runs a whole password login: create the session, answer the
// password question (and acknowledge notices), start the session. The
// returned response is the last one (success, or the error).
func (c *Client) Login(user, password string, cmd, env []string) (Response, error) {
	r, err := c.CreateSession(user)
	for err == nil && r.Type == AuthMessage {
		switch r.AuthMessageType {
		case Secret, Visible:
			p := password
			r, err = c.Respond(&p)
		default:
			r, err = c.Respond(nil)
		}
	}
	if err != nil || r.Type != Success {
		return r, err
	}
	return c.StartSession(cmd, env)
}
