package main

import (
	"bufio"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/basalt-os/basalt-shell/internal/greetd"
)

// lines runs the helper on the given request lines against greetd at
// sock and returns its output lines.
func lines(t *testing.T, sock string, reqs ...string) ([]map[string]any, int) {
	t.Helper()
	pr, pw := io.Pipe()
	or, ow := io.Pipe()
	done := make(chan int, 1)
	go func() { done <- run(sock, pr, ow, time.Second); ow.Close() }()
	var out []map[string]any
	sc := bufio.NewScanner(or)
	read := func() bool {
		if !sc.Scan() {
			return false
		}
		var m map[string]any
		if err := json.Unmarshal(sc.Bytes(), &m); err != nil {
			t.Fatalf("not JSON: %q", sc.Text())
		}
		out = append(out, m)
		return true
	}
	if !read() {
		t.Fatal("no first line")
	}
	if out[0]["type"] == "ipc_error" {
		pw.Close()
		return out, <-done
	}
	for _, r := range reqs {
		if _, err := io.WriteString(pw, r+"\n"); err != nil {
			t.Fatal(err)
		}
		if !read() {
			break
		}
	}
	pw.Close()
	for read() {
	}
	return out, <-done
}

// A whole login through the helper, with greetd's split writes: every
// answer arrives whole and in order, and the session is started.
func TestLoginSplitWrites(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "greetd.sock")
	f := &greetd.Fake{Users: map[string]string{"ana": "pw"}, Split: true}
	if err := f.Listen(sock); err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out, rc := lines(t, sock,
		`{"type":"create_session","username":"ana"}`,
		`{"type":"post_auth_message_response","response":"pw"}`,
		`{"type":"start_session","cmd":["basalt-session","sway"],"env":["LANG=pt_BR.UTF-8"]}`)
	if rc != 0 {
		t.Fatalf("status %d: %v", rc, out)
	}
	want := []string{"ipc_ready", "auth_message", "success", "success"}
	if len(out) != len(want) {
		t.Fatalf("got %v", out)
	}
	for i, w := range want {
		if out[i]["type"] != w {
			t.Errorf("line %d: %v, want %s", i, out[i], w)
		}
	}
	s := f.Sessions()
	if len(s) != 1 || s[0].User != "ana" || strings.Join(s[0].Env, " ") != "LANG=pt_BR.UTF-8" {
		t.Errorf("sessions: %+v", s)
	}
	for _, m := range out {
		b, _ := json.Marshal(m)
		if strings.Contains(string(b), `"pw"`) {
			t.Errorf("the password came back: %s", b)
		}
	}
}

// A refused password, the cancel after it and a new conversation.
func TestRefusedThenRetry(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "greetd.sock")
	f := &greetd.Fake{Users: map[string]string{"ana": "pw"}, Split: true}
	if err := f.Listen(sock); err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out, rc := lines(t, sock,
		`{"type":"create_session","username":"ana"}`,
		`{"type":"post_auth_message_response","response":"wrong"}`,
		`{"type":"cancel_session"}`,
		`{"type":"create_session","username":"ana"}`,
		`{"type":"post_auth_message_response","response":"pw"}`)
	if rc != 0 {
		t.Fatalf("status %d", rc)
	}
	if len(out) != 6 || out[2]["error_type"] != "auth_error" || out[4]["type"] != "auth_message" || out[5]["type"] != "success" {
		t.Fatalf("got %v", out)
	}
}

// Problems are one ipc_error line and status 1; a bad line is never echoed.
func TestErrors(t *testing.T) {
	out, rc := lines(t, "")
	if rc != 1 || out[0]["type"] != "ipc_error" {
		t.Errorf("no socket: %v %d", out, rc)
	}
	out, rc = lines(t, filepath.Join(t.TempDir(), "none.sock"))
	if rc != 1 || out[0]["type"] != "ipc_error" {
		t.Errorf("missing socket: %v %d", out, rc)
	}
	sock := filepath.Join(t.TempDir(), "greetd.sock")
	f := &greetd.Fake{Users: map[string]string{"ana": "pw"}}
	if err := f.Listen(sock); err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	out, rc = lines(t, sock, `{"type":"post_auth_message_response","response":"secret-pw`)
	if rc != 1 || out[len(out)-1]["type"] != "ipc_error" {
		t.Fatalf("bad line: %v %d", out, rc)
	}
	if b, _ := json.Marshal(out); strings.Contains(string(b), "secret-pw") {
		t.Errorf("the line was echoed: %s", b)
	}
	out, rc = lines(t, sock, `{"type":"shell","cmd":["sh"]}`)
	if rc != 1 || out[len(out)-1]["type"] != "ipc_error" {
		t.Errorf("unknown type: %v %d", out, rc)
	}
}
