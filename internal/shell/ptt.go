package shell

import (
	"context"
	"time"

	"github.com/basalt-os/basalt-shell/internal/compositor"
	"github.com/basalt-os/basalt-shell/internal/voice"
	"github.com/basalt-os/basalt-shell/internal/voiceprefs"
)

// Push to talk has two ways of working, chosen per person in Settings,
// Voice and assistant (push_to_talk in voice-and-assistant.conf):
//
//   - hold (the default): the microphone is open while Super+V (or the
//     panel's microphone button) is held; releasing it sends the words.
//   - toggle, "press to start and stop": a press opens the microphone, the
//     next press closes it and sends the words. Escape (or the card's
//     Cancel) drops them. It also ends by itself at the hold limit
//     (BASALT_VOICE_MAX_HOLD) and, when the person chose it (the default
//     is 2 s), after a silence once speech was heard.
//
// Every compositor sends the same events: a press when the key goes down
// and a release when it goes up. niri has no key-release bindings, so its
// press says that no release will follow ("latch"), and that utterance
// works as toggle whatever the setting is: one code path for both.
//
// The decisions are a pure function (pttDecide) over the event, whether
// the microphone is open and the session that opened it; the Core keeps
// the state and does what it says.

// pttEvent is what happened.
type pttEvent string

const (
	pttPress   pttEvent = "press"   // the key or the button went down
	pttRelease pttEvent = "release" // it went up (never on niri)
	pttCancel  pttEvent = "cancel"  // Escape or the card's Cancel
	pttTick    pttEvent = "tick"    // time passed (limit and silence)
)

// pttAction is what to do.
type pttAction string

const (
	pttNone      pttAction = ""
	pttStart     pttAction = "start"     // open the microphone
	pttStop      pttAction = "stop"      // close it and send the words
	pttDropWords pttAction = "cancel"    // close it and drop the words
	pttIgnore    pttAction = "debounced" // a second press too soon after the first
)

// pttDebounce: in toggle mode a press this soon after the one that
// opened the microphone is ignored, so a long press (key repeat, or a
// duplicated event) does not start and stop at once. It is longer than
// the usual key repeat delay (600 ms).
const pttDebounce = 700 * time.Millisecond

// pttSession is how the open microphone was opened.
type pttSession struct {
	Mode     string        // voiceprefs.PushHold or voiceprefs.PushToggle
	Since    time.Time     // when the microphone opened
	MaxHold  time.Duration // the hold limit
	AutoStop time.Duration // toggle: silence that ends it (0: never)
}

// pttSignal is what the voice service measured (toggle only).
type pttSignal struct {
	Heard   bool          // speech-like sound was heard
	Silence time.Duration // audio time since it stopped
}

// pttModeFor is the mode of a new utterance: the person's setting, but
// toggle when no release will follow the press (niri).
func pttModeFor(setting string, latch bool) string {
	if latch || setting == voiceprefs.PushToggle {
		return voiceprefs.PushToggle
	}
	return voiceprefs.PushHold
}

// pttDecide says what an event means. listening is whether the
// microphone is open now; s is the session that opened it (ignored when
// it is not open).
func pttDecide(ev pttEvent, listening bool, s pttSession, now time.Time, sig pttSignal) (pttAction, string) {
	if !listening {
		if ev == pttPress {
			return pttStart, ""
		}
		// A release without an open microphone (refused at the lock
		// screen, a stray key-up, or the up of the press that stopped a
		// toggle utterance), a cancel or a tick: nothing to close.
		return pttNone, ""
	}
	switch ev {
	case pttCancel:
		return pttDropWords, "cancel"
	case pttPress:
		if s.Mode != voiceprefs.PushToggle {
			// Holding: a second down (key repeat) changes nothing.
			return pttNone, ""
		}
		if now.Sub(s.Since) < pttDebounce {
			return pttIgnore, ""
		}
		return pttStop, "key"
	case pttRelease:
		if s.Mode == voiceprefs.PushToggle {
			// The up of the press that started it: keep listening.
			return pttNone, ""
		}
		return pttStop, "release"
	case pttTick:
		if s.Mode != voiceprefs.PushToggle {
			// Holding: the voice service cuts the audio at the limit and
			// the release sends it, as before.
			return pttNone, ""
		}
		if s.MaxHold > 0 && now.Sub(s.Since) >= s.MaxHold {
			return pttStop, "limit"
		}
		if s.AutoStop > 0 && sig.Heard && sig.Silence >= s.AutoStop {
			return pttStop, "silence"
		}
	}
	return pttNone, ""
}

// pttTickEvery is how often a toggle utterance checks the limit and the
// silence (Core.PTTTick replaces it in tests).
const pttTickEvery = 200 * time.Millisecond

// voiceListening reports whether the microphone is open and the session
// that opened it.
func (c *Core) voiceListening() (bool, pttSession, uint64) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.voice.State == "listening", c.voiceSession, c.voiceGen
}

// takeListening ends the utterance gen if it is still the open one, so
// only one of the key, the limit, the silence and Escape ends it. It
// returns the generation of the turn that answers it.
func (c *Core) takeListening(gen uint64) (uint64, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.voice.State != "listening" || c.voiceGen != gen {
		return 0, false
	}
	c.voiceGen++
	return c.voiceGen, true
}

// stopListening closes the microphone and sends the words (the end of
// an utterance for any reason but a cancel).
func (c *Core) stopListening(gen uint64, reason string) {
	turn, ok := c.takeListening(gen)
	if !ok {
		return
	}
	c.voiceKeys(false)
	release := time.Now()
	if reason != "release" && reason != "key" {
		_, _ = c.Audit.Append("voice", "daemon", "microphone closed by itself ("+reason+")", map[string]any{"reason": reason})
	}
	c.setVoiceTurn(turn, VoiceState{State: "transcribing"})
	go c.voiceTurnFor(release, turn)
}

// voiceKeys turns the compositor's voice key mode on or off (sway:
// Escape cancels while a toggle utterance listens). It reports whether
// the compositor did it. Turning it off is always sent (cheap), so a mode
// left on by a daemon that stopped is cleared by the next utterance.
func (c *Core) voiceKeys(on bool) bool {
	vk, ok := c.Comp.(compositor.VoiceKeys)
	if !ok {
		return false
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	return vk.SetVoiceKeys(ctx, on) == nil
}

// watchToggle ends a toggle utterance at the hold limit or after the
// silence the person chose. It runs while that utterance listens.
func (c *Core) watchToggle(gen uint64) {
	every := c.PTTTick
	if every <= 0 {
		every = pttTickEvery
	}
	t := time.NewTicker(every)
	defer t.Stop()
	for range t.C {
		listening, s, cur := c.voiceListening()
		if !listening || cur != gen {
			return
		}
		var sig pttSignal
		if s.AutoStop > 0 && c.Voice != nil {
			ctx, cancel := context.WithTimeout(context.Background(), time.Second)
			rep, err := c.Voice.Do(ctx, voice.Request{Op: "status"})
			cancel()
			if err == nil && rep.Status != nil {
				sig = pttSignal{Heard: rep.Status.Heard, Silence: time.Duration(rep.Status.SilenceMS) * time.Millisecond}
			}
		}
		if act, why := pttDecide(pttTick, true, s, time.Now(), sig); act == pttStop {
			c.stopListening(gen, why)
			return
		}
	}
}
