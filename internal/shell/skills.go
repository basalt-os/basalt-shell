package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/basalt-os/basalt-shell/internal/docs"
	"github.com/basalt-os/basalt-shell/internal/i18n"
	"github.com/basalt-os/basalt-shell/internal/models"
	"github.com/basalt-os/basalt-shell/internal/skills"
	"github.com/basalt-os/basalt-shell/internal/voice"
	"github.com/basalt-os/basalt-shell/internal/voiceprefs"
)

// The read-only skills add two typed actions, both confirmed by the
// person like every other action: grant.add (a consent sheet: let the
// assistant read a folder, a mailbox or a site, for a limited time) and
// file.open (open a found file with its default application). Neither is
// ever planned from content: grant.add comes from the person's own
// request or from a skill that needs a scope for the person's request;
// file.open takes a path from the last search results, inside an active
// folder grant.
func init() {
	Actions = append(Actions,
		&ActionDef{
			Name: "grant.add", Title: "Let the assistant read something",
			Description: "Give the read-only skills a scope for a limited time: a folder (and below), a mailbox, or a web site. Read only: the skills search, read and summarize; they never change, send, submit or delete.",
			Params: []Param{
				{Name: "kind", Type: "string", Required: true, Enum: []string{skills.GrantFolder, skills.GrantMailbox, skills.GrantSite}, Description: "folder, mailbox or site"},
				{Name: "targets", Type: "array", Required: true, Description: "folder paths, a mail account name, or a site host"},
				{Name: "duration", Type: "string", Description: "how long: 30s to 7d (default 1h)"},
			},
			plan: planGrant,
		},
		&ActionDef{
			Name: "file.open", Title: "Open a file",
			Description: "Open a file found by the files skill with its default application. Only files inside a folder the person granted.",
			Params:      []Param{{Name: "path", Type: "string", Required: true, Description: "absolute path of a search result"}},
			plan:        planFileOpen,
		},
	)
}

func planGrant(ctx context.Context, p *planner, a map[string]any) (step, error) {
	e := p.c.Skills
	if e == nil {
		return step{}, errors.New("the read-only skills are not available")
	}
	kind := argStr(a, "kind")
	var targets []string
	switch v := a["targets"].(type) {
	case []any:
		for _, x := range v {
			targets = append(targets, strings.TrimSpace(fmt.Sprint(x)))
		}
	case []string:
		targets = v
	case string:
		targets = []string{v}
	}
	if len(targets) == 0 || len(targets) > 8 {
		return step{}, errors.New("give 1 to 8 targets")
	}
	d, err := skills.ParseDuration(argStr(a, "duration"))
	if err != nil {
		return step{}, err
	}
	var labels []string
	switch kind {
	case skills.GrantFolder:
		for i, t := range targets {
			t = filepath.Clean(t)
			targets[i] = t
			if !filepath.IsAbs(t) || docs.Denied(e.Home, t) {
				return step{}, fmt.Errorf("%s cannot be granted", t)
			}
			if st, err := os.Lstat(t); err != nil || !st.IsDir() {
				return step{}, fmt.Errorf("%s is not a folder", t)
			}
			rel, _ := filepath.Rel(e.Home, t)
			if strings.HasPrefix(rel, "..") {
				return step{}, fmt.Errorf("%s is outside your home folder", t)
			}
			labels = append(labels, "~/"+strings.TrimPrefix(rel, "."))
		}
	case skills.GrantMailbox, skills.GrantSite:
		labels = targets
	default:
		return step{}, fmt.Errorf("kind must be folder, mailbox or site")
	}
	var summary string
	switch kind {
	case skills.GrantFolder:
		summary = i18n.G("Let the assistant read and search the files in %s (and below; hidden files, keys and browser data stay closed) for %s",
			strings.Join(labels, ", "), skills.Human(d))
	case skills.GrantMailbox:
		summary = i18n.G("Let the assistant read and summarize the mailbox %s (nothing is sent, deleted or marked as read) for %s",
			strings.Join(labels, ", "), skills.Human(d))
	default:
		summary = i18n.G("Let the assistant read pages of %s in a separate browser profile (no forms are submitted; other sites stay blocked) for %s",
			strings.Join(labels, ", "), skills.Human(d))
	}
	return step{Summary: summary, run: func(ctx context.Context) (any, error) {
		gs, err := e.ApplyGrant(kind, targets, d, "ui")
		if err != nil {
			return nil, err
		}
		for _, g := range gs {
			_, _ = p.c.Audit.Append("apply", "ui", "grant: "+g.Kind+" "+g.Label+" until "+g.Expires.Format(time.RFC3339),
				map[string]any{"grant": g})
		}
		if kind == skills.GrantMailbox {
			// The senders' names, for the speech recognition.
			go func() {
				ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
				defer cancel()
				_ = e.RefreshSenders(ctx)
			}()
		}
		p.c.Broadcast("grants", e.Store.Active(""))
		return gs, nil
	}}, nil
}

func planFileOpen(ctx context.Context, p *planner, a map[string]any) (step, error) {
	e := p.c.Skills
	if e == nil {
		return step{}, errors.New("the read-only skills are not available")
	}
	path := filepath.Clean(argStr(a, "path"))
	if !filepath.IsAbs(path) {
		return step{}, errors.New("path must be absolute")
	}
	if _, ok := e.Store.FolderFor(path); !ok {
		return step{}, fmt.Errorf("%s is not inside a folder you granted", path)
	}
	if docs.Denied(e.Home, path) {
		return step{}, fmt.Errorf("%s cannot be opened by the assistant", path)
	}
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() {
		return step{}, fmt.Errorf("%s is not a regular file", path)
	}
	return step{Summary: i18n.G("Open %s with its default application", filepath.Base(path)), run: func(ctx context.Context) (any, error) {
		return nil, p.c.Launch(ctx, "open", []string{"xdg-open", path})
	}}, nil
}

// skillAsk answers a request with a read-only skill; ok is false when
// the request is not for a skill.
func (c *Core) skillAsk(ctx context.Context, text string) (AskResult, bool) {
	if c.Skills == nil {
		return AskResult{}, false
	}
	a, ok := c.Skills.Handle(ctx, text)
	if !ok {
		return AskResult{}, false
	}
	if a.NeedModel {
		// The answer is shown without a summary; the model card offers
		// the assistant's local model (nothing is downloaded before the
		// person chooses Download).
		c.offerLocalModel(ctx, text)
	}
	res := AskResult{Kind: "skill", Request: text, Backend: "skill", Skill: &a}
	if a.Error != "" && a.NeedGrant == nil && a.Grant == nil && a.Open == "" && a.Act == nil {
		res.Kind, res.Error = "error", a.Error
		return res, true
	}
	var calls []Call
	switch {
	case a.NeedGrant != nil:
		calls = []Call{{Action: "grant.add", Args: map[string]any{"kind": a.NeedGrant.Kind, "targets": anySlice(a.NeedGrant.Targets), "duration": a.NeedGrant.Duration}}}
		res.Retry = text
	case a.Grant != nil:
		calls = []Call{{Action: "grant.add", Args: map[string]any{"kind": a.Grant.Kind, "targets": anySlice(a.Grant.Targets), "duration": a.Grant.Duration}}}
	case a.Open != "":
		calls = []Call{{Action: "file.open", Args: map[string]any{"path": a.Open}}}
	case a.Act != nil:
		calls = []Call{{Action: a.Act.Action, Args: a.Act.Args}}
	}
	if calls != nil {
		pr, err := c.Propose(ctx, Meta{Origin: "commandbar", Actor: "commandbar", Request: text, Explain: a.Text, Backend: "skill"}, calls)
		if err != nil {
			res.Kind, res.Error = "error", err.Error()
			return res, true
		}
		cp := pr.public()
		res.Kind, res.Proposal = "proposal", &cp
	}
	return res, true
}

func anySlice(s []string) []any {
	out := make([]any, len(s))
	for i, v := range s {
		out[i] = v
	}
	return out
}

// ------------------------------------------------------------ voice

// VoiceState is what the UI shows while the person talks.
type VoiceState struct {
	// idle, listening, transcribing, thinking, speaking, dictation, error;
	// offer (no speech model yet: Offer asks to download it) and download
	// (Download is the download the person agreed to).
	State   string           `json:"state"`
	Text    string           `json:"text,omitempty"`
	Error   string           `json:"error,omitempty"`
	Since   time.Time        `json:"since"`
	Timing  map[string]int64 `json:"timing,omitempty"`
	Enabled bool             `json:"enabled"`
	// Mode is where the words go: "dictation" (into Target's text field)
	// or "assistant"; Note says why a focused field gets no dictation.
	Mode   string `json:"mode,omitempty"`
	Target string `json:"target,omitempty"`
	Note   string `json:"note,omitempty"`
	// Proposal is the dictation waiting for Insert or Discard.
	Proposal string `json:"proposal,omitempty"`
	// Lang is the speech language in force (auto or a tag) and Answer
	// the answer language, shown on the card.
	Lang   string `json:"lang,omitempty"`
	Answer string `json:"answer_lang,omitempty"`
	// LangName is the speech language's own name ("" for auto).
	LangName string `json:"lang_name,omitempty"`
	// While listening: PushToTalk is hold or toggle (how this utterance
	// ends), EscKey whether the compositor binds Escape to cancel it
	// (sway; elsewhere the card offers Cancel), MaxHoldS the hold limit
	// and AutoStopMS the silence that ends a toggle utterance (0: never).
	PushToTalk string `json:"push_to_talk,omitempty"`
	EscKey     bool   `json:"esc_key,omitempty"`
	MaxHoldS   int    `json:"max_hold_s,omitempty"`
	AutoStopMS int64  `json:"auto_stop_ms,omitempty"`
	// Offer and Download: the speech model download (zero setup).
	Offer    *models.Offer `json:"offer,omitempty"`
	Download *models.Job   `json:"download,omitempty"`
}

func (c *Core) setVoice(st VoiceState) { c.setVoiceTurn(0, st) }

// setVoiceTurn sets the card for the utterance turn (0: whatever is
// going on). A turn that a newer press superseded changes nothing: the
// answer to the last utterance must not close the card of the next one
// (found in the lab: a press while the previous answer was being worked
// on showed "listening", then the old turn set the card to idle and the
// next press opened the microphone again instead of stopping).
func (c *Core) setVoiceTurn(turn uint64, st VoiceState) bool {
	st.Since = time.Now().UTC()
	st.Enabled = c.voiceInstalled()
	if st.Lang == "" {
		c.prefs.mu.Lock()
		st.Lang, st.Answer = c.prefs.eff.SpeechLang, c.prefs.eff.AnswerLang
		c.prefs.mu.Unlock()
	}
	if st.Lang != "" && st.Lang != "auto" {
		st.LangName = nativeName(st.Lang)
	}
	c.mu.Lock()
	if turn != 0 && c.voiceGen != turn {
		c.mu.Unlock()
		return false
	}
	c.voice = st
	c.mu.Unlock()
	c.Broadcast("voice", st)
	return true
}

// speaker is a second connection to the voice service, for "speak"
// only: a speak waits for the first sentence to be synthesized (seconds
// for a cold Piper), and on the shared connection a press made meanwhile
// waited for it before the microphone opened (found in the lab). The
// voice service serves each connection on its own, and its "listen"
// stops the answer being spoken.
func (c *Core) speaker() *voice.Client {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.speakClient == nil || c.speakClient.Path != c.Voice.Path {
		c.speakClient = &voice.Client{Path: c.Voice.Path}
	}
	return c.speakClient
}

// voiceCurrent is the utterance generation now (see ptt.go).
func (c *Core) voiceCurrent() uint64 {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.voiceGen
}

// VoiceStatus returns the current voice state.
func (c *Core) VoiceStatus() VoiceState {
	c.mu.Lock()
	defer c.mu.Unlock()
	st := c.voice
	st.Enabled = c.voiceInstalled()
	if st.State == "" {
		st.State = "idle"
	}
	return st
}

// VoicePress is the key or the panel button going down, with a release
// to follow (sway, the panel button). Only the shell UI may call it.
func (c *Core) VoicePress(ctx context.Context, commandBar bool) error {
	return c.VoiceKeyDown(ctx, commandBar, false)
}

// VoiceKeyDown is the key or the panel button going down. latch says no
// release will follow (niri has no key-release bindings): the utterance
// then works as "press to start and stop" whatever the setting is. With
// the microphone closed it opens it; with it open, in "press to start
// and stop", it closes it and sends the words (ptt.go).
func (c *Core) VoiceKeyDown(ctx context.Context, commandBar, latch bool) error {
	if c.Voice == nil {
		return errors.New(i18n.G("The voice service is not running."))
	}
	if listening, s, gen := c.voiceListening(); listening {
		if act, why := pttDecide(pttPress, true, s, time.Now(), pttSignal{}); act == pttStop {
			c.stopListening(gen, why)
		}
		return nil
	}
	if c.ScreenLocked != nil && c.ScreenLocked() {
		_, _ = c.Audit.Append("refuse", "ui", "microphone refused: the screen is locked", nil)
		err := errors.New(i18n.G("The screen is locked. Unlock it to talk."))
		c.setVoice(VoiceState{State: "error", Error: err.Error()})
		return err
	}
	// Zero setup: the voice service starts when its socket is missing.
	if err := c.ensureVoice(ctx); err != nil {
		_, _ = c.Audit.Append("fail", "daemon", "voice service did not start: "+err.Error(), nil)
		msg := i18n.G("The voice service could not start. Try again in a moment; if it keeps failing, log out and in again.")
		c.setVoice(VoiceState{State: "error", Error: msg})
		return errors.New(msg)
	}
	// The person's speech language and model; an English-only model for
	// another language is refused before the microphone opens.
	eff := c.refreshPrefs()
	ms := c.voiceModels(ctx)
	c.prefs.mu.Lock()
	sys := c.prefs.system
	c.prefs.mu.Unlock()
	model := speechModelFor(eff, sys, ms)
	if !voiceReady(model, sys, ms) {
		// Not downloaded yet: the card offers it, in the person's
		// language, with its size; nothing is downloaded before Download.
		return c.offerVoice(ctx, eff, model, sys)
	}
	if _, err := voice.WhisperLanguage(eff.SpeechLang, model); err != nil {
		msg := voiceError(err, model)
		_, _ = c.Audit.Append("refuse", "ui", "microphone not opened: "+err.Error(), map[string]any{"speech_language": eff.SpeechLang, "speech_model": model})
		c.setVoice(VoiceState{State: "error", Error: msg})
		return errors.New(msg)
	}
	// A dictation still waiting for Insert is dropped by a new press.
	for _, pr := range c.Pending() {
		if pr.Origin == "voice" {
			_, _ = c.Decide(ctx, pr.ID, false, "ui")
		}
	}
	rt := c.routeVoice()
	if commandBar {
		// The command bar is open: the words are a request to the assistant.
		rt = voiceRoute{Mode: "assistant"}
	}
	// Hold or "press to start and stop" (ptt.go), fixed for this utterance.
	sess := pttSession{Mode: pttModeFor(eff.PushToTalk, latch), Since: time.Now(), MaxHold: sys.MaxHold}
	toggle := sess.Mode == voiceprefs.PushToggle
	if toggle {
		sess.AutoStop = time.Duration(eff.AutoStopMS) * time.Millisecond
	}
	c.mu.Lock()
	c.voicePress = sess.Since
	c.voiceRoute = rt
	c.voiceModel = model
	c.voiceSession = sess
	c.voiceGen++
	gen := c.voiceGen
	c.mu.Unlock()
	if _, err := c.Voice.Do(ctx, voice.Request{Op: "listen"}); err != nil {
		c.setVoice(VoiceState{State: "error", Error: err.Error()})
		return err
	}
	esc := false
	if toggle {
		// sway: Escape cancels while the microphone is open.
		esc = c.voiceKeys(true)
	}
	c.setVoice(VoiceState{State: "listening", Mode: rt.Mode, Target: rt.Target, Note: rt.Note,
		PushToTalk: sess.Mode, EscKey: esc, MaxHoldS: int(sess.MaxHold / time.Second), AutoStopMS: sess.AutoStop.Milliseconds()})
	if toggle {
		go c.watchToggle(gen)
	}
	if c.Skills != nil {
		// Sender names for the next utterances (at most every ten minutes,
		// only while a mailbox is granted).
		go func() {
			ctx, cancel := context.WithTimeout(context.Background(), time.Minute)
			defer cancel()
			_ = c.Skills.RefreshSenders(ctx)
		}()
	}
	_, _ = c.Audit.Append("voice", "ui", "microphone opened (push to talk, "+rt.Mode+")", map[string]any{"mode": rt.Mode, "target": rt.Target, "push_to_talk": sess.Mode})
	return nil
}

// VoiceRelease closes the microphone, transcribes, answers the request
// like the command bar and speaks the short answer. It returns at once;
// the UI follows the "voice" and "voice-result" events.
func (c *Core) VoiceRelease(ctx context.Context) error {
	if c.Voice == nil {
		return errors.New("the voice service is not running")
	}
	// In hold mode the release sends the words; in "press to start and
	// stop" it is the up of a press and changes nothing; without an
	// open microphone (refused at the lock screen, a stray key-up) there
	// is nothing to close.
	listening, s, gen := c.voiceListening()
	if act, why := pttDecide(pttRelease, listening, s, time.Now(), pttSignal{}); act == pttStop {
		c.stopListening(gen, why)
	}
	return nil
}

func (c *Core) voiceTurn(release time.Time) { c.voiceTurnFor(release, c.voiceCurrent()) }

// voiceTurnFor answers the utterance that ended at release; turn is the
// generation it left (a newer press supersedes it: its card and its
// spoken answer are then dropped, the answer is still shown).
func (c *Core) voiceTurnFor(release time.Time, turn uint64) {
	ctx, cancel := context.WithTimeout(context.Background(), 4*time.Minute)
	defer cancel()
	timing := map[string]int64{}
	c.mu.Lock()
	rt, pressModel := c.voiceRoute, c.voiceModel
	c.mu.Unlock()
	eff := c.refreshPrefs()
	// The model chosen when the key went down (the person's, or the one
	// that understands their language).
	rep, err := c.Voice.Do(ctx, voice.Request{Op: "stop", Prompt: c.speechPrompt(rt.Mode == "dictation"), Dictation: rt.Mode == "dictation",
		Lang: eff.SpeechLang, Model: pressModel})
	_, _ = c.Audit.Append("voice", "ui", "microphone closed", nil)
	if err != nil || rep.Transcript == nil {
		msg := i18n.G("Speech to text failed.")
		if err != nil {
			model := pressModel
			if model == "" {
				model = i18n.G("the default model")
			}
			msg = voiceError(err, model)
		}
		c.setVoiceTurn(turn, VoiceState{State: "error", Error: msg})
		return
	}
	tr := rep.Transcript
	timing["held"] = tr.AudioMS
	timing["stt"] = tr.STTMS
	timing["release_to_text"] = time.Since(release).Milliseconds()
	if !tr.Speech || strings.TrimSpace(tr.Text) == "" {
		// Nothing understood (silence, noise, or a key let go at once): a
		// friendly note on the card, not an error.
		c.setVoiceTurn(turn, VoiceState{State: "idle", Note: i18n.G("I did not catch that. Try again."), Mode: rt.Mode, Timing: timing})
		_, _ = c.Audit.Append("voice", "voice", "no speech", map[string]any{"timing": timing, "level": tr.Level})
		return
	}
	request := tr.Text
	if rt.Mode == "dictation" {
		if rest, ok := AssistantPrefix(tr.Text); ok && rest != "" {
			// "Assistant, ...": a request, although a field has focus.
			request = rest
			rt.Mode = "assistant"
		}
	}
	if rt.Mode == "dictation" {
		pr, err := c.dictate(ctx, rt, tr.Text)
		timing["release_to_preview"] = time.Since(release).Milliseconds()
		if err != nil {
			c.setVoiceTurn(turn, VoiceState{State: "error", Error: err.Error(), Mode: rt.Mode, Target: rt.Target, Timing: timing})
			return
		}
		_, _ = c.Audit.Append("voice", "voice", "dictation shown for confirmation", map[string]any{"timing": timing, "stt_model": tr.Model,
			"target": rt.Target, "chars": len([]rune(tr.Text))})
		c.setVoiceTurn(turn, VoiceState{State: "dictation", Text: tr.Text, Mode: rt.Mode, Target: rt.Target, Proposal: pr.ID, Timing: timing})
		go func() {
			// The card goes back to idle when the dictation is decided.
			_, _ = c.Wait(context.Background(), pr.ID)
			if st := c.VoiceStatus(); st.Proposal == pr.ID {
				c.setVoiceTurn(turn, VoiceState{State: "idle", Text: tr.Text, Mode: rt.Mode, Target: rt.Target})
			}
		}()
		return
	}
	c.setVoiceTurn(turn, VoiceState{State: "thinking", Text: request, Mode: rt.Mode, Target: rt.Target, Note: rt.Note, Timing: timing})
	t := time.Now()
	res := c.Ask(ctx, request)
	timing["answer"] = time.Since(t).Milliseconds()
	c.Broadcast("voice-result", map[string]any{"request": request, "result": res})
	speech := spokenAnswer(res)
	timing["release_to_answer"] = time.Since(release).Milliseconds()
	var sp *voice.Spoken
	// The settings in force now (the request may have taken a while, or
	// changed them). With spoken answers off nothing is synthesized: no
	// voice is looked up, no "speak" reaches the voice service, and the
	// card says nothing about speaking.
	eff = c.refreshPrefs()
	// The answer is spoken in the answer language with a voice for it
	// (the person's, else an installed one), never by a voice of another
	// language. Without one the answer is shown only, and the card says
	// so once per language and session.
	note := ""
	voiceName := ""
	if speech != "" && eff.Spoken {
		models := c.voiceModels(ctx)
		c.prefs.mu.Lock()
		chosen := c.prefs.prefs.Voices
		c.prefs.mu.Unlock()
		v, ok := voiceFor(eff, chosen, models)
		if !ok {
			c.mu.Lock()
			if c.voiceLangNoticed == nil {
				c.voiceLangNoticed = map[string]bool{}
			}
			first := !c.voiceLangNoticed[eff.AnswerLang]
			c.voiceLangNoticed[eff.AnswerLang] = true
			c.mu.Unlock()
			if first {
				note = i18n.G("Answers are shown, not spoken: no voice for %s is installed.", nativeName(eff.AnswerLang))
			}
			speech = ""
		}
		voiceName = v
	} else {
		speech = ""
	}
	if speech != "" && c.voiceCurrent() != turn {
		// The person already pressed again: do not speak over them.
		speech = ""
	}
	if speech != "" {
		c.setVoiceTurn(turn, VoiceState{State: "speaking", Text: tr.Text, Timing: timing})
		r, err := c.speaker().Do(ctx, voice.Request{Op: "speak", Text: speech, Voice: voiceName})
		if err == nil && r.Spoken != nil {
			sp = r.Spoken
			timing["tts_first_audio"] = sp.FirstAudioMS
			timing["release_to_first_audio"] = time.Since(release).Milliseconds()
		}
	}
	_, _ = c.Audit.Append("voice", "voice", tr.Text, map[string]any{"timing": timing, "stt_model": tr.Model, "stt_language": tr.Lang,
		"answer_language": eff.AnswerLang, "kind": res.Kind, "spoken": speech, "level": tr.Level, "tts": sp})
	c.setVoiceTurn(turn, VoiceState{State: "idle", Text: tr.Text, Timing: timing, Note: note})
}

// nativeName is a language's own name for the person ("Português
// (Brasil)"), or its tag.
func nativeName(tag string) string {
	for _, l := range i18n.Languages {
		if l.Tag == i18n.Tag(tag) {
			return l.Native
		}
	}
	for _, l := range i18n.Languages {
		if i18n.Base(l.Tag) == i18n.Base(tag) {
			return l.Native
		}
	}
	return tag
}

// speechPrompt is the extra speech-recognition prompt of this utterance:
// the names the person may say (contacts, senders of the granted
// mailbox) and, for requests, the names of the installed themes (the lab
// heard "lichen" as "like and" in English and "lixem" in Portuguese
// without them).
func (c *Core) speechPrompt(dictation bool) string {
	var names []string
	if c.Skills != nil {
		names = c.Skills.SpeechNames(24)
	}
	// In the speech language: an English lead-in biased Portuguese
	// speech toward English words.
	c.prefs.mu.Lock()
	pt := i18n.Base(c.prefs.eff.SpeechLang) == "pt"
	c.prefs.mu.Unlock()
	hi, lead, themesLead := "Hi ", "Names: ", "Themes: "
	if pt {
		hi, lead, themesLead = "Oi ", "Nomes: ", "Temas: "
	}
	if dictation {
		if len(names) == 0 {
			return ""
		}
		// Free text: the names as the start of a message.
		return hi + strings.Join(names, ", ") + "."
	}
	var parts []string
	if len(names) > 0 {
		parts = append(parts, lead+strings.Join(names, ", ")+".")
	}
	if c.Themes != nil {
		var ts []string
		for _, t := range c.Themes.Themes() {
			ts = append(ts, t.Name)
		}
		if len(ts) > 0 && len(ts) <= 12 {
			parts = append(parts, themesLead+strings.Join(ts, ", ")+".")
		}
	}
	return strings.Join(parts, " ")
}

// spokenAnswer is the short text read aloud for a result.
func spokenAnswer(r AskResult) string {
	switch {
	case r.Skill != nil && r.Skill.Speech != "":
		return r.Skill.Speech
	case r.Kind == "proposal" && r.Proposal != nil:
		return i18n.G("Please confirm on the screen: %s.", strings.TrimSuffix(r.Proposal.summary(), "."))
	case r.Kind == "system":
		return i18n.G("Here is what the system assistant found.")
	case r.Error != "":
		return r.Error
	}
	return ""
}

// WatchVoice tells the UI when the voice service appears or goes away.
func (c *Core) WatchVoice(ctx context.Context) {
	if c.Voice == nil {
		return
	}
	last := !c.Voice.Available()
	t := time.NewTicker(3 * time.Second)
	defer t.Stop()
	for {
		if up := c.Voice.Available(); up != last {
			last = up
			st := c.VoiceStatus()
			c.setVoice(VoiceState{State: st.State, Text: st.Text})
		}
		select {
		case <-ctx.Done():
			return
		case <-t.C:
		}
	}
}

// VoiceCancel drops the recording (the person pressed Escape or the
// card's Cancel): the microphone closes and nothing is transcribed.
func (c *Core) VoiceCancel(ctx context.Context) {
	if listening, s, gen := c.voiceListening(); listening {
		if act, _ := pttDecide(pttCancel, true, s, time.Now(), pttSignal{}); act == pttDropWords {
			if _, ok := c.takeListening(gen); ok {
				_, _ = c.Audit.Append("voice", "ui", "microphone closed, words dropped (cancel)", nil)
			}
		}
	}
	c.voiceKeys(false)
	if c.Voice != nil {
		_, _ = c.Voice.Do(ctx, voice.Request{Op: "cancel"})
		_, _ = c.Voice.Do(ctx, voice.Request{Op: "hush"})
	}
	c.setVoice(VoiceState{State: "idle"})
}

// GrantsChanged is wired to the store (broadcast and audit expiry).
func (c *Core) wireGrants() {
	if c.Skills == nil {
		return
	}
	c.Skills.Store.OnExpire = func(g skills.Grant) {
		_, _ = c.Audit.Append("expire", "grants", "grant ended: "+g.Kind+" "+g.Label, map[string]any{"grant": g})
	}
	c.Skills.Store.OnChange = func(gs []skills.Grant) { c.Broadcast("grants", gs) }
	// A revoked grant's rule in the approval gate goes with it.
	c.Skills.Store.OnRevoke = func(gs []skills.Grant) {
		for _, g := range gs {
			c.gateRemoveRules(c.GrantRules(g.ID))
		}
	}
	c.Skills.Audit = func(typ, text string, data map[string]any) { _, _ = c.Audit.Append(typ, "skill", text, data) }
	go func() {
		t := time.NewTicker(2 * time.Second)
		defer t.Stop()
		for range t.C {
			c.Skills.Store.Sweep()
		}
	}()
}
