// Command basalt-greetd-cancel ends the login conversation greetd is
// holding, if any, so that the next greeter can start a new one.
//
// greeter-session runs it before it falls back to the text login. When the
// graphical login screen dies after it asked greetd for a person's session
// (the password question is pending), greetd keeps that conversation: it
// does not cancel it when the greeter's connection is reset, only when the
// connection ends cleanly. tuigreet, started right after with --remember,
// asks for the remembered person at once, greetd refuses because a session
// is already being configured, and the text login opened with "An error was
// received from greetd". A cancel_session from a new connection ends the
// stale conversation first.
//
// It talks to the socket named by GREETD_SOCK and never fails the caller:
// problems are printed and the exit status is 0.
package main

import (
	"fmt"
	"os"
	"time"

	"github.com/basalt-os/basalt-shell/internal/greetd"
)

func main() {
	if msg := cancel(os.Getenv("GREETD_SOCK"), 3*time.Second); msg != "" {
		fmt.Fprintln(os.Stderr, "basalt-greetd-cancel:", msg)
	}
}

// cancel sends cancel_session and says what happened when it is not the
// usual outcome ("" when greetd answered success).
func cancel(sock string, timeout time.Duration) string {
	if sock == "" {
		return "GREETD_SOCK is not set"
	}
	c, err := greetd.Dial(sock)
	if err != nil {
		return err.Error()
	}
	defer c.Close()
	done := make(chan string, 1)
	go func() {
		r, err := c.Cancel()
		switch {
		case err != nil:
			done <- err.Error()
		case r.Type == greetd.Success:
			done <- ""
		default:
			// No conversation to end: nothing was pending.
			done <- "greetd answered " + r.Type + ": " + r.Description
		}
	}()
	select {
	case m := <-done:
		return m
	case <-time.After(timeout):
		return "no answer from greetd"
	}
}
