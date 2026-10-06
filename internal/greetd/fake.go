package greetd

import (
	"errors"
	"net"
	"os"
	"sync"
)

// Fake is a greetd stand-in for tests and the lab: users with passwords,
// optional PAM notices, and greetd 0.10's answers, quirks included:
//
//   - a refused password is an auth_error, after an optional notice (an
//     info or error auth_message, as pam_faillock sends) that the greeter
//     acknowledges first;
//   - cancel_session right after a refused password is answered with an
//     error ("unable to send message: ..."): greetd's PAM worker is gone,
//     and greetd still tries to tell it to cancel. Quickshell's client
//     sends exactly that cancel after every auth_error;
//   - create_session while a conversation is open, a refused one included,
//     is an error ("a session is already being configured").
//
// Passwords are compared and dropped; the fake records which users logged
// in and the session commands, never an answer.
type Fake struct {
	// Users maps user names to passwords.
	Users map[string]string
	// Notice, when set, is sent as an auth_message of type NoticeType
	// (info or error) before every auth_error, like pam_faillock.
	Notice     string
	NoticeType string
	// Locked users get the notice "The account is locked" and an auth_error.
	Locked map[string]bool

	mu       sync.Mutex
	started  []Started
	failures int
	ln       net.Listener
}

// Started is one start_session the fake accepted.
type Started struct {
	User string
	Cmd  []string
	Env  []string
}

// conversation is the state of one client's login conversation.
type conversation struct {
	user        string
	state       string // "", "asked", "notice", "authenticated", "dead"
	pendingFail bool
}

// Listen serves on a Unix socket at path (removed first).
func (f *Fake) Listen(path string) error {
	_ = os.Remove(path)
	ln, err := net.Listen("unix", path)
	if err != nil {
		return err
	}
	f.ln = ln
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return nil
}

// Close stops listening.
func (f *Fake) Close() error {
	if f.ln == nil {
		return nil
	}
	return f.ln.Close()
}

// Sessions returns the sessions started so far.
func (f *Fake) Sessions() []Started {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]Started(nil), f.started...)
}

// Failures returns how many answers were refused.
func (f *Fake) Failures() int {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.failures
}

func (f *Fake) serve(c net.Conn) {
	defer c.Close()
	var conv *conversation
	for {
		var req Request
		if err := Read(c, &req); err != nil {
			return
		}
		resp := f.handle(&conv, req)
		// The answer is not kept.
		req.Response = nil
		if err := Write(c, resp); err != nil {
			return
		}
	}
}

func errResp(kind, desc string) Response {
	return Response{Type: Error, ErrorType: kind, Description: desc}
}

func (f *Fake) handle(conv **conversation, req Request) Response {
	switch req.Type {
	case CreateSession:
		// Also after a refused password: greetd keeps the dead
		// conversation until it is cancelled.
		if *conv != nil {
			return errResp(ErrOther, "a session is already being configured")
		}
		*conv = &conversation{user: req.Username, state: "asked"}
		return Response{Type: AuthMessage, AuthMessageType: Secret, AuthMessage: "Password: "}
	case PostAuthMessageResponse:
		cv := *conv
		if cv == nil || cv.state == "dead" {
			return errResp(ErrOther, "no session under configuration")
		}
		switch cv.state {
		case "notice":
			// The notice acknowledged: now the failure.
			cv.state = "dead"
			return errResp(ErrAuth, "pam_authenticate: AUTH_ERR")
		case "asked":
			if req.Response == nil {
				cv.state = "dead"
				return errResp(ErrAuth, "pam_authenticate: AUTH_ERR")
			}
			want, ok := f.Users[cv.user]
			ok = ok && want == *req.Response && !f.Locked[cv.user]
			if ok {
				cv.state = "authenticated"
				return Response{Type: Success}
			}
			f.mu.Lock()
			f.failures++
			f.mu.Unlock()
			if f.Locked[cv.user] {
				cv.state = "notice"
				return Response{Type: AuthMessage, AuthMessageType: ErrorMsg, AuthMessage: "The account is locked due to 3 failed logins."}
			}
			if f.Notice != "" {
				cv.state = "notice"
				t := f.NoticeType
				if t == "" {
					t = Info
				}
				return Response{Type: AuthMessage, AuthMessageType: t, AuthMessage: f.Notice}
			}
			cv.state = "dead"
			return errResp(ErrAuth, "pam_authenticate: AUTH_ERR")
		default:
			return errResp(ErrOther, "session has no pending questions")
		}
	case CancelSession:
		cv := *conv
		*conv = nil
		if cv != nil && cv.state == "dead" {
			// greetd 0.10: the worker exited after the failure.
			return errResp(ErrOther, "unable to send message: Connection refused (os error 111)")
		}
		return Response{Type: Success}
	case StartSession:
		cv := *conv
		if cv == nil || cv.state != "authenticated" {
			return errResp(ErrOther, "session not ready")
		}
		if len(req.Cmd) == 0 {
			return errResp(ErrOther, "no command")
		}
		f.mu.Lock()
		f.started = append(f.started, Started{User: cv.user, Cmd: req.Cmd, Env: req.Env})
		f.mu.Unlock()
		*conv = nil
		return Response{Type: Success}
	}
	return errResp(ErrOther, errors.New("unknown request").Error())
}
