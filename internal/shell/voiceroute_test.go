package shell

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/basalt-os/basalt-shell/internal/i18n"
	"github.com/basalt-os/basalt-shell/internal/voiceprefs"
	"github.com/basalt-os/basalt-shell/internal/wlime"
)

// TestRouteFor: where the words go. Nothing focused, or a field that
// lost focus, is a request to the assistant (the owner's report: holding
// Super+V on an empty desktop must never end in an error).
func TestRouteFor(t *testing.T) {
	field := wlime.State{Active: true, Available: true, Gen: 7}
	left := wlime.State{Active: false, Available: true, Gen: 7} // deactivated after a field had focus
	secret := wlime.State{Active: true, Available: true, Gen: 8, Purpose: wlime.PurposePassword}
	cases := []struct {
		name       string
		haveIM     bool
		st         wlime.State
		app        string
		appFocused bool
		mode, note string
	}{
		{"no input method", false, wlime.State{}, "", false, "assistant", ""},
		{"empty desktop", true, wlime.State{Available: true}, "", false, "assistant", ""},
		{"focus left the field", true, left, "Mousepad", true, "assistant", ""},
		// A stale activation with no app window focused (the shell's own
		// surfaces): still the assistant.
		{"active but no app window", true, field, "", false, "assistant", ""},
		{"password field", true, secret, "KeePassXC", true, "assistant", "password"},
		{"text field", true, field, "Mousepad", true, "dictation", ""},
	}
	for _, c := range cases {
		rt := routeFor(c.haveIM, c.st, c.app, c.appFocused)
		if rt.Mode != c.mode || (c.note == "") != (rt.Note == "") {
			t.Errorf("%s: %+v", c.name, rt)
		}
		if rt.Mode == "dictation" && rt.Gen != c.st.Gen {
			t.Errorf("%s: field %d, want %d", c.name, rt.Gen, c.st.Gen)
		}
	}
}

// TestVoiceNothingHeard: an empty or unclear utterance is a friendly
// note in the person's language, not an error; the press on an empty
// desktop (no input method in the tests) goes to the assistant.
func TestVoiceNothingHeard(t *testing.T) {
	old := i18n.Dir
	i18n.Dir, _ = filepath.Abs("../../locale")
	t.Cleanup(func() { i18n.Dir = old; i18n.Load("en") })
	c, fv, _ := pttCore(t, voiceprefs.Prefs{AnswerLang: "pt-BR", SpeechLang: "en-US"})
	ctx := context.Background()
	if err := c.VoicePress(ctx, false); err != nil {
		t.Fatal(err)
	}
	if st := c.VoiceStatus(); st.State != "listening" || st.Mode != "assistant" || st.Error != "" {
		t.Fatalf("listening card: %+v", st)
	}
	fv.mu.Lock()
	fv.text = "  "
	fv.mu.Unlock()
	_ = c.VoiceRelease(ctx)
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) && c.VoiceStatus().State != "idle" {
		time.Sleep(10 * time.Millisecond)
	}
	st := c.VoiceStatus()
	if st.State != "idle" || st.Error != "" || st.Note != "Não entendi. Tente de novo." {
		t.Fatalf("card: %+v", st)
	}
}
