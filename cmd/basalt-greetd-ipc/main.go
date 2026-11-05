// Command basalt-greetd-ipc is the login screen's connection to greetd.
//
// The greeter UI (Quickshell) starts it as a child process and talks to it
// in lines: each line on stdin is one greetd request as JSON
// ({"type":"create_session","username":"ana"}), and each answer from greetd
// comes back as one JSON line on stdout, in order. The program frames the
// messages for greetd's socket (GREETD_SOCK: a 32-bit length in the host's
// byte order, then the JSON) and reads every answer whole.
//
// Why not Quickshell's own greetd client: the one Fedora ships reads one
// length and one body per readyRead signal. greetd writes the length and
// the body with two writes, so a read can see the length alone: the
// client then takes an empty body, drops the conversation without a
// signal ("Received unexpected greetd response"), and reads the next
// body's first four bytes as a length. The login screen waited forever
// (release audit 2026-11, B6). Reading whole messages here makes the
// framing the kernel's business, not the UI's event loop.
//
// The password is in a post_auth_message_response line: it goes to greetd
// and nowhere else; this program never prints or logs a request. When
// stdin closes, the connection to greetd is closed, which ends any
// conversation that was open. A broken connection is reported as one line
// {"type":"ipc_error","description":"..."} and exit status 1.
package main

import (
	"bufio"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"os"
	"time"

	"github.com/basalt-os/basalt-shell/internal/greetd"
)

func main() {
	os.Exit(run(os.Getenv("GREETD_SOCK"), os.Stdin, os.Stdout, 5*time.Second))
}

// run serves the line protocol until in ends (status 0) or greetd's
// connection fails (status 1).
func run(sock string, in io.Reader, out io.Writer, dial time.Duration) int {
	enc := json.NewEncoder(out)
	fail := func(format string, a ...any) int {
		_ = enc.Encode(map[string]string{"type": "ipc_error", "description": fmt.Sprintf(format, a...)})
		return 1
	}
	if sock == "" {
		return fail("GREETD_SOCK is not set")
	}
	conn, err := net.DialTimeout("unix", sock, dial)
	if err != nil {
		return fail("cannot connect to greetd: %v", err)
	}
	defer conn.Close()
	// Tell the UI the connection is up (it may queue requests until then).
	_ = enc.Encode(map[string]string{"type": "ipc_ready"})
	sc := bufio.NewScanner(in)
	sc.Buffer(make([]byte, 64*1024), greetd.MaxMessage)
	for sc.Scan() {
		line := sc.Bytes()
		if len(line) == 0 {
			continue
		}
		var req greetd.Request
		if err := json.Unmarshal(line, &req); err != nil {
			// Never echo the line: it may hold a password.
			return fail("a request that is not JSON")
		}
		if !known(req.Type) {
			return fail("unknown request type %q", req.Type)
		}
		if err := greetd.Write(conn, req); err != nil {
			return fail("cannot write to greetd: %v", err)
		}
		var resp greetd.Response
		if err := greetd.Read(conn, &resp); err != nil {
			return fail("cannot read greetd's answer: %v", err)
		}
		if err := enc.Encode(resp); err != nil {
			return 1
		}
	}
	return 0
}

func known(t string) bool {
	switch t {
	case greetd.CreateSession, greetd.PostAuthMessageResponse, greetd.StartSession, greetd.CancelSession:
		return true
	}
	return false
}
