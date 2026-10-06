package greetd

import (
	"bytes"
	"path/filepath"
	"reflect"
	"testing"
)

func fake(t *testing.T) (*Fake, *Client) {
	t.Helper()
	f := &Fake{Users: map[string]string{"ana": "right horse", "basalt": "staple"}}
	sock := filepath.Join(t.TempDir(), "greetd.sock")
	if err := f.Listen(sock); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	c, err := Dial(sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = c.Close() })
	return f, c
}

func ptr(s string) *string { return &s }

// The wire format: a 32-bit length in host order, then the JSON.
func TestCodecRoundTrip(t *testing.T) {
	var buf bytes.Buffer
	in := Request{Type: StartSession, Cmd: []string{"basalt-session", "sway"}, Env: []string{"XDG_SESSION_TYPE=wayland"}}
	if err := Write(&buf, in); err != nil {
		t.Fatal(err)
	}
	if n := buf.Len(); n < 5 {
		t.Fatalf("short message: %d bytes", n)
	}
	var out Request
	if err := Read(&buf, &out); err != nil {
		t.Fatal(err)
	}
	if !reflect.DeepEqual(in, out) {
		t.Fatalf("%+v != %+v", in, out)
	}
	// An answer is sent even when empty; an acknowledgement has none.
	b, _ := jsonOf(Request{Type: PostAuthMessageResponse, Response: ptr("")})
	if !bytes.Contains(b, []byte(`"response":""`)) {
		t.Errorf("empty answer dropped: %s", b)
	}
	b, _ = jsonOf(Request{Type: PostAuthMessageResponse})
	if bytes.Contains(b, []byte(`"response":`)) {
		t.Errorf("acknowledgement carries a response: %s", b)
	}
}

func jsonOf(v any) ([]byte, error) {
	var buf bytes.Buffer
	err := Write(&buf, v)
	return buf.Bytes()[4:], err
}

func TestReadRejectsHugeMessage(t *testing.T) {
	buf := bytes.NewBuffer([]byte{0xff, 0xff, 0xff, 0x7f})
	var r Response
	if err := Read(buf, &r); err == nil {
		t.Fatal("a 2 GB message was accepted")
	}
}

// The right password: the session the greeter asked for is started.
func TestLogin(t *testing.T) {
	f, c := fake(t)
	r, err := c.Login("ana", "right horse", []string{"basalt-session", "niri"}, []string{"XDG_SESSION_DESKTOP=niri"})
	if err != nil || r.Type != Success {
		t.Fatalf("%+v %v", r, err)
	}
	s := f.Sessions()
	if len(s) != 1 || s[0].User != "ana" || !reflect.DeepEqual(s[0].Cmd, []string{"basalt-session", "niri"}) {
		t.Fatalf("started %+v", s)
	}
}

// A wrong password, the way greetd 0.10 and Quickshell's client go
// through it: auth_error, then Quickshell's cancel is answered with an
// error the greeter must not show (Login.qml: expectCancelReply), then a
// new conversation works.
func TestWrongPasswordThenRight(t *testing.T) {
	f, c := fake(t)
	r, _ := c.CreateSession("basalt")
	if r.Type != AuthMessage || r.AuthMessageType != Secret {
		t.Fatalf("question %+v", r)
	}
	r, _ = c.Respond(ptr("wrong"))
	if r.Type != Error || r.ErrorType != ErrAuth {
		t.Fatalf("wrong password: %+v", r)
	}
	// Without a cancel, greetd keeps the dead conversation.
	r, _ = c.CreateSession("basalt")
	if r.Type != Error || r.Description != "a session is already being configured" {
		t.Fatalf("create before cancel: %+v", r)
	}
	r, _ = c.Cancel()
	if r.Type != Error || r.ErrorType != ErrOther {
		t.Fatalf("cancel after a refused password: %+v (greetd 0.10 answers with an error)", r)
	}
	r, err := c.Login("basalt", "staple", []string{"basalt-session", "sway"}, nil)
	if err != nil || r.Type != Success {
		t.Fatalf("second try: %+v %v", r, err)
	}
	if f.Failures() != 1 || len(f.Sessions()) != 1 {
		t.Fatalf("failures %d sessions %d", f.Failures(), len(f.Sessions()))
	}
}

// pam_faillock style: a notice before the failure, acknowledged with no
// answer; a locked account says so.
func TestNoticeAndLocked(t *testing.T) {
	f, c := fake(t)
	f.Notice = "There were 2 failed login attempts."
	f.Locked = map[string]bool{"ana": true}
	r, _ := c.CreateSession("basalt")
	r, _ = c.Respond(ptr("nope"))
	if r.Type != AuthMessage || r.AuthMessageType != Info || r.AuthMessage != f.Notice {
		t.Fatalf("notice %+v", r)
	}
	r, _ = c.Respond(nil)
	if r.Type != Error || r.ErrorType != ErrAuth {
		t.Fatalf("after the notice %+v", r)
	}
	_, _ = c.Cancel()
	r, _ = c.CreateSession("ana")
	r, _ = c.Respond(ptr("right horse"))
	if r.Type != AuthMessage || r.AuthMessageType != ErrorMsg {
		t.Fatalf("locked: %+v", r)
	}
	r, _ = c.Respond(nil)
	if r.ErrorType != ErrAuth {
		t.Fatalf("locked, then %+v", r)
	}
}

// start_session only after authentication.
func TestStartNeedsAuthentication(t *testing.T) {
	f, c := fake(t)
	r, _ := c.StartSession([]string{"sh"}, nil)
	if r.Type != Error {
		t.Fatalf("started without a login: %+v", r)
	}
	_, _ = c.CreateSession("ana")
	r, _ = c.StartSession([]string{"sh"}, nil)
	if r.Type != Error || len(f.Sessions()) != 0 {
		t.Fatalf("started before the password: %+v", r)
	}
}
