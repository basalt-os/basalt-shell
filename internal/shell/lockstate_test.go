package shell

import (
	"context"
	"strings"
	"testing"

	"github.com/basalt-os/basalt-shell/internal/voiceprefs"
)

// While the shell's lock screen holds the session (the UI says so through
// ui.state), agents cannot type, click or take screenshots, and push to
// talk is refused. The flag only takes power away: it never unlocks
// anything (the daemon has no unlock operation; PAM decides in the UI).
func TestLockedRefusesAgentsAndVoice(t *testing.T) {
	c, _, _ := newCore(t)
	ctx := context.Background()
	c.ScreenLocked = func() bool { return false }
	var typed []string
	c.TypeText = func(_ context.Context, s string) error { typed = append(typed, s); return nil }
	m := agentMeta(4343)
	pr, err := c.Propose(ctx, m, []Call{{Action: "agent.control", Args: map[string]any{"reason": "fill a form", "minutes": 2}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Decide(ctx, pr.ID, true, "ui"); err != nil {
		t.Fatal(err)
	}

	c.SetUILocked(true)
	if !c.Locked() {
		t.Fatal("not locked after the UI said so")
	}
	if _, err := c.Input(ctx, m, InputRequest{Kind: "type", Text: "hunter2"}); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("input at the lock screen: %v", err)
	}
	if len(typed) != 0 {
		t.Fatalf("typed at the lock screen: %v", typed)
	}
	if _, err := c.Capture(ctx, m, map[string]any{}, 0); err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("screenshot at the lock screen: %v", err)
	}

	c.SetUILocked(false)
	if c.Locked() {
		t.Fatal("still locked")
	}
	if _, err := c.Input(ctx, m, InputRequest{Kind: "type", Text: "ok"}); err != nil {
		t.Fatal(err)
	}

	// A locker program (the swaylock fallback) counts too.
	c.ScreenLocked = func() bool { return true }
	if !c.Locked() {
		t.Fatal("swaylock not counted")
	}
}

func TestLockedRefusesPushToTalk(t *testing.T) {
	c, _, _ := pttCore(t, voiceprefs.Prefs{})
	c.SetUILocked(true)
	err := c.VoiceKeyDown(context.Background(), false, false)
	if err == nil || !strings.Contains(err.Error(), "locked") {
		t.Fatalf("microphone opened at the lock screen: %v", err)
	}
}

// ui.state is the shell UI's alone: an agent connection cannot mark the
// screen locked or unlocked.
func TestUIStateIsUIOnly(t *testing.T) {
	c, _, _ := newCore(t)
	c.ScreenLocked = func() bool { return false }
	ss := &session{s: &Server{Core: c}, role: RoleAgent}
	if _, err := ss.handle(context.Background(), Request{Op: "ui.state", Args: []byte(`{"locked":true}`)}); err == nil {
		t.Fatal("an agent set ui.state")
	}
	if c.Locked() {
		t.Fatal("an agent changed the lock state")
	}
}
