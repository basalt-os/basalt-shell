package shell

import (
	"context"
	"strings"
	"testing"
	"time"

	"github.com/basalt-os/basalt-shell/internal/compositor/fake"
	"github.com/basalt-os/basalt-shell/internal/voiceprefs"
)

// TestPTTDecide is the state machine itself: hold, toggle, cancel, the
// hold limit, the silence and the debounce of a long press.
func TestPTTDecide(t *testing.T) {
	t0 := time.Date(2026, 10, 6, 12, 0, 0, 0, time.UTC)
	hold := pttSession{Mode: voiceprefs.PushHold, Since: t0, MaxHold: 30 * time.Second}
	toggle := pttSession{Mode: voiceprefs.PushToggle, Since: t0, MaxHold: 30 * time.Second, AutoStop: 2 * time.Second}
	never := toggle
	never.AutoStop = 0
	at := func(d time.Duration) time.Time { return t0.Add(d) }
	sec := time.Second
	cases := []struct {
		name      string
		ev        pttEvent
		listening bool
		s         pttSession
		now       time.Time
		sig       pttSignal
		act       pttAction
		why       string
	}{
		// Closed microphone: only a press does something.
		{"idle press", pttPress, false, pttSession{}, at(0), pttSignal{}, pttStart, ""},
		{"idle release (stray key-up)", pttRelease, false, pttSession{}, at(0), pttSignal{}, pttNone, ""},
		{"idle cancel", pttCancel, false, pttSession{}, at(0), pttSignal{}, pttNone, ""},
		{"idle tick", pttTick, false, pttSession{}, at(0), pttSignal{}, pttNone, ""},
		// Hold: the release sends; a repeated down changes nothing; the
		// voice service cuts the audio at the limit and the release sends.
		{"hold release", pttRelease, true, hold, at(3 * sec), pttSignal{}, pttStop, "release"},
		{"hold repeat", pttPress, true, hold, at(2 * sec), pttSignal{}, pttNone, ""},
		{"hold limit", pttTick, true, hold, at(31 * sec), pttSignal{}, pttNone, ""},
		{"hold silence", pttTick, true, hold, at(5 * sec), pttSignal{Heard: true, Silence: 3 * sec}, pttNone, ""},
		{"hold cancel", pttCancel, true, hold, at(1 * sec), pttSignal{}, pttDropWords, "cancel"},
		// Toggle: the up of the starting press keeps listening; the next
		// press sends; a press too soon (long press, key repeat) is ignored.
		{"toggle release", pttRelease, true, toggle, at(300 * time.Millisecond), pttSignal{}, pttNone, ""},
		{"toggle long press", pttPress, true, toggle, at(600 * time.Millisecond), pttSignal{}, pttIgnore, ""},
		{"toggle second press", pttPress, true, toggle, at(4 * sec), pttSignal{}, pttStop, "key"},
		{"toggle cancel", pttCancel, true, toggle, at(4 * sec), pttSignal{}, pttDropWords, "cancel"},
		// The limit ends it, even while the person speaks.
		{"toggle limit", pttTick, true, toggle, at(30 * sec), pttSignal{Heard: true}, pttStop, "limit"},
		{"toggle under limit", pttTick, true, never, at(29 * sec), pttSignal{}, pttNone, ""},
		// Silence ends it only after speech was heard, and only when
		// chosen (0: never).
		{"toggle silence", pttTick, true, toggle, at(6 * sec), pttSignal{Heard: true, Silence: 2 * sec}, pttStop, "silence"},
		{"toggle short pause", pttTick, true, toggle, at(6 * sec), pttSignal{Heard: true, Silence: 1500 * time.Millisecond}, pttNone, ""},
		{"toggle nothing said yet", pttTick, true, toggle, at(8 * sec), pttSignal{Heard: false, Silence: 0}, pttNone, ""},
		{"toggle silence off", pttTick, true, never, at(8 * sec), pttSignal{Heard: true, Silence: 5 * sec}, pttNone, ""},
	}
	for _, c := range cases {
		act, why := pttDecide(c.ev, c.listening, c.s, c.now, c.sig)
		if act != c.act || why != c.why {
			t.Errorf("%s: %q %q, want %q %q", c.name, act, why, c.act, c.why)
		}
	}
	// The mode of a new utterance: the setting, or toggle when no
	// release will follow (niri).
	for _, m := range []struct {
		setting string
		latch   bool
		want    string
	}{{"", false, "hold"}, {"hold", false, "hold"}, {"toggle", false, "toggle"}, {"", true, "toggle"}, {"hold", true, "toggle"}} {
		if got := pttModeFor(m.setting, m.latch); got != m.want {
			t.Errorf("pttModeFor(%q, %v) = %q", m.setting, m.latch, got)
		}
	}
}

// pttCore is a core with the fake voice service and an English model.
func pttCore(t *testing.T, prefs voiceprefs.Prefs) (*Core, *fakeVoice, *fake.Adapter) {
	t.Helper()
	c, fk, dir := newCore(t)
	voiceLab(t, dir)
	fv, cl := startFakeVoice(t, dir)
	c.Voice = cl
	c.ScreenLocked = func() bool { return false }
	if err := c.SetVoiceSettings(context.Background(), prefs); err != nil {
		t.Fatal(err)
	}
	fv.text = "make it darker"
	fv.take()
	return c, fv, fk
}

// waitOp waits until the voice service got op.
func waitOp(t *testing.T, fv *fakeVoice, op string) []string {
	t.Helper()
	var all []string
	deadline := time.Now().Add(3 * time.Second)
	for time.Now().Before(deadline) {
		all = append(all, fv.take()...)
		if hasOp(all, op) {
			return all
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatalf("no %q (got %v)", op, all)
	return nil
}

// backdate moves the open utterance's start into the past.
func backdate(c *Core, d time.Duration) {
	c.mu.Lock()
	c.voiceSession.Since = c.voiceSession.Since.Add(-d)
	c.mu.Unlock()
}

func voiceKeyCalls(fk *fake.Adapter) string {
	var out []string
	for _, call := range fk.Calls {
		if strings.HasPrefix(call, "voicekeys") {
			out = append(out, call)
		}
	}
	return strings.Join(out, ",")
}

func TestPushToTalkHold(t *testing.T) {
	c, fv, fk := pttCore(t, voiceprefs.Prefs{})
	ctx := context.Background()
	if err := c.VoicePress(ctx, false); err != nil {
		t.Fatal(err)
	}
	st := c.VoiceStatus()
	if st.State != "listening" || st.PushToTalk != "hold" || st.EscKey {
		t.Fatalf("card: %+v", st)
	}
	// Key repeat while held: nothing.
	_ = c.VoicePress(ctx, false)
	if ops := fv.take(); hasOp(ops, "stop") {
		t.Fatalf("a repeated down stopped: %v", ops)
	}
	if err := c.VoiceRelease(ctx); err != nil {
		t.Fatal(err)
	}
	waitOp(t, fv, "stop")
	// Hold never switches sway's key mode.
	if k := voiceKeyCalls(fk); strings.Contains(k, "true") {
		t.Errorf("hold changed the key mode: %s", k)
	}
}

func TestPushToTalkToggle(t *testing.T) {
	c, fv, fk := pttCore(t, voiceprefs.Prefs{PushToTalk: "toggle", AutoStopSilence: "0"})
	ctx := context.Background()
	if err := c.VoicePress(ctx, false); err != nil {
		t.Fatal(err)
	}
	st := c.VoiceStatus()
	if st.State != "listening" || st.PushToTalk != "toggle" || !st.EscKey || st.AutoStopMS != 0 || st.MaxHoldS != 30 {
		t.Fatalf("card: %+v", st)
	}
	// The up of the press, and a second down right away (long press):
	// still listening.
	_ = c.VoiceRelease(ctx)
	_ = c.VoicePress(ctx, false)
	if ops := fv.take(); hasOp(ops, "stop") || c.VoiceStatus().State != "listening" {
		t.Fatalf("stopped by the long press: %v %s", ops, c.VoiceStatus().State)
	}
	// The next press, later: the words are sent.
	backdate(c, 2*time.Second)
	_ = c.VoicePress(ctx, false)
	waitOp(t, fv, "stop")
	// Its up changes nothing (the utterance is already being answered).
	_ = c.VoiceRelease(ctx)
	if k := voiceKeyCalls(fk); !strings.HasPrefix(k, "voicekeys true,voicekeys false") {
		t.Errorf("key mode: %s", k)
	}
}

// TestPushToTalkLatch: niri's press says no release will follow, so a
// hold setting still works there (one code path).
func TestPushToTalkLatch(t *testing.T) {
	c, fv, _ := pttCore(t, voiceprefs.Prefs{PushToTalk: "hold"})
	ctx := context.Background()
	if err := c.VoiceKeyDown(ctx, false, true); err != nil {
		t.Fatal(err)
	}
	if st := c.VoiceStatus(); st.PushToTalk != "toggle" || st.AutoStopMS != 2000 {
		t.Fatalf("card: %+v", st)
	}
	backdate(c, time.Second)
	_ = c.VoiceKeyDown(ctx, false, true)
	waitOp(t, fv, "stop")
}

func TestPushToTalkCancel(t *testing.T) {
	c, fv, fk := pttCore(t, voiceprefs.Prefs{PushToTalk: "toggle"})
	ctx := context.Background()
	_ = c.VoicePress(ctx, false)
	c.VoiceCancel(ctx)
	ops := fv.take()
	if !hasOp(ops, "cancel") || hasOp(ops, "stop") {
		t.Fatalf("cancel sent %v", ops)
	}
	if st := c.VoiceStatus(); st.State != "idle" {
		t.Fatalf("card: %+v", st)
	}
	// A late press of the key after the cancel starts a new utterance.
	_ = c.VoicePress(ctx, false)
	if c.VoiceStatus().State != "listening" {
		t.Fatal("no new utterance after a cancel")
	}
	c.VoiceCancel(ctx)
	if k := voiceKeyCalls(fk); !strings.HasSuffix(k, "voicekeys false") {
		t.Errorf("key mode left on: %s", k)
	}
}

func TestPushToTalkLimitAndSilence(t *testing.T) {
	ctx := context.Background()

	// The hold limit ends a toggle utterance and sends the words.
	c, fv, _ := pttCore(t, voiceprefs.Prefs{PushToTalk: "toggle", AutoStopSilence: "0"})
	c.PTTTick = 10 * time.Millisecond
	_ = c.VoicePress(ctx, false)
	time.Sleep(50 * time.Millisecond)
	if c.VoiceStatus().State != "listening" {
		t.Fatal("ended before the limit")
	}
	backdate(c, 31*time.Second)
	waitOp(t, fv, "stop")
	var said bool
	for _, r := range c.Audit.Tail(20) {
		said = said || strings.Contains(r.Text, "closed by itself (limit)")
	}
	if !said {
		t.Error("the limit is not in the activity log")
	}

	// Silence after speech ends it; before speech it does not.
	c, fv, _ = pttCore(t, voiceprefs.Prefs{PushToTalk: "toggle", AutoStopSilence: "1.5s"})
	c.PTTTick = 10 * time.Millisecond
	fv.mu.Lock()
	fv.heard, fv.silenceMS = false, 0
	fv.mu.Unlock()
	_ = c.VoicePress(ctx, false)
	time.Sleep(60 * time.Millisecond)
	if ops := fv.take(); hasOp(ops, "stop") {
		t.Fatalf("stopped before any speech: %v", ops)
	}
	fv.mu.Lock()
	fv.heard, fv.silenceMS = true, 1000
	fv.mu.Unlock()
	time.Sleep(60 * time.Millisecond)
	if ops := fv.take(); hasOp(ops, "stop") {
		t.Fatalf("stopped on a short pause: %v", ops)
	}
	fv.mu.Lock()
	fv.silenceMS = 1600
	fv.mu.Unlock()
	waitOp(t, fv, "stop")
	// The key pressed right after: a new utterance, not a second stop.
	time.Sleep(20 * time.Millisecond)
	if st := c.VoiceStatus().State; st == "listening" {
		t.Fatalf("still listening after the silence")
	}
}

// TestPushToTalkOldTurn: the answer to the last utterance, arriving
// while the person already speaks again, neither closes the new card nor
// speaks over them (found in the lab).
func TestPushToTalkOldTurn(t *testing.T) {
	c, fv, _ := pttCore(t, voiceprefs.Prefs{PushToTalk: "toggle", AutoStopSilence: "0", AnswerLang: "en-US"})
	ctx := context.Background()
	// A first utterance, stopped: its turn is the old one.
	_ = c.VoicePress(ctx, false)
	listening, _, gen := c.voiceListening()
	old, ok := c.takeListening(gen)
	if !listening || !ok || old == 0 {
		t.Fatal("no first utterance")
	}
	c.setVoiceTurn(old, VoiceState{State: "thinking"})
	if err := c.VoicePress(ctx, false); err != nil {
		t.Fatal(err)
	}
	fv.take()
	c.voiceTurnFor(time.Now(), old)
	if st := c.VoiceStatus(); st.State != "listening" {
		t.Fatalf("the old turn changed the card: %+v", st)
	}
	if ops := fv.take(); hasOp(ops, "speak") || !hasOp(ops, "stop") {
		t.Fatalf("the old turn sent %v", ops)
	}
	// The second press still stops the new utterance.
	backdate(c, time.Second)
	_ = c.VoicePress(ctx, false)
	waitOp(t, fv, "stop")
}
