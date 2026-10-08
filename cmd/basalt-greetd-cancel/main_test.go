package main

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/basalt-os/basalt-shell/internal/greetd"
)

// The helper ends a pending conversation, so a new greeter's
// create_session succeeds, and it reports a missing socket instead of
// failing.
func TestCancel(t *testing.T) {
	sock := filepath.Join(t.TempDir(), "greetd.sock")
	f := &greetd.Fake{Users: map[string]string{"basalt": "pw"}}
	if err := f.Listen(sock); err != nil {
		t.Fatal(err)
	}
	defer f.Close()
	if m := cancel(sock, 2*time.Second); m != "" {
		t.Errorf("cancel on an idle greetd: %q", m)
	}
	if m := cancel("", time.Second); m == "" {
		t.Error("no socket must be reported")
	}
	if m := cancel(filepath.Join(t.TempDir(), "none.sock"), time.Second); m == "" {
		t.Error("a missing socket must be reported")
	}
}
