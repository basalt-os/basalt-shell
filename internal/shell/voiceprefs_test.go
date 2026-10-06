package shell

import (
	"bufio"
	"context"
	"encoding/json"
	"net"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/basalt-os/basalt-shell/internal/i18n"
	"github.com/basalt-os/basalt-shell/internal/intent"
	"github.com/basalt-os/basalt-shell/internal/voice"
	"github.com/basalt-os/basalt-shell/internal/voiceprefs"
)

// voiceLab sets up a system voice configuration with an English-only
// default model, a multilingual one and an English voice, and a policy
// file without remote models.
func voiceLab(t *testing.T, dir string) {
	t.Helper()
	models := filepath.Join(dir, "models")
	_ = os.MkdirAll(models, 0o755)
	for _, n := range []string{"ggml-base.en.bin", "ggml-small-q5_1.bin", "ggml-silero-v5.1.2.bin", "en_US-ljspeech-medium.onnx", "en_US-ljspeech-medium.onnx.json"} {
		_ = os.WriteFile(filepath.Join(models, n), []byte("x"), 0o644)
	}
	conf := filepath.Join(dir, "voice.conf")
	// The shipped voice.conf sets no speech language (the session's is
	// used); the tests set the session to English.
	t.Setenv("LANG", "en_US.UTF-8")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	_ = os.WriteFile(conf, []byte("BASALT_VOICE_STT_MODEL="+filepath.Join(models, "ggml-base.en.bin")+"\nBASALT_VOICE_TTS_MODEL="+
		filepath.Join(models, "en_US-ljspeech-medium.onnx")+"\nBASALT_VOICE_VAD_MODEL="+filepath.Join(models, "ggml-silero-v5.1.2.bin")+"\n"), 0o644)
	oldConf, oldPol := SystemVoiceConf, voiceprefs.PolicyPath
	SystemVoiceConf, voiceprefs.PolicyPath = conf, filepath.Join(dir, "desktop-models.conf")
	t.Cleanup(func() { SystemVoiceConf, voiceprefs.PolicyPath = oldConf, oldPol; i18n.Load("en") })
}

func TestVoiceSettingsPolicy(t *testing.T) {
	c, _, dir := newCore(t)
	voiceLab(t, dir)
	local := &intent.Model{Endpoint: "unix:/run/basalt-llm/llm.sock"}
	c.SetLocalModel(local)
	ctx := context.Background()

	// Portuguese with an English-only model the person chose: refused,
	// with the reason.
	err := c.SetVoiceSettings(ctx, voiceprefs.Prefs{SpeechLang: "pt-BR", SpeechModel: "ggml-base.en"})
	if err == nil || !strings.Contains(err.Error(), "English only") {
		t.Fatalf("pt-BR on ggml-base.en: %v", err)
	}
	if _, err := os.Stat(c.prefsPath()); err == nil {
		t.Fatal("a refused setting was written")
	}
	// A model that is not installed, a remote model without a policy.
	if err := c.SetVoiceSettings(ctx, voiceprefs.Prefs{SpeechLang: "pt-BR", SpeechModel: "ggml-large"}); err == nil {
		t.Error("missing model accepted")
	}
	if err := c.SetVoiceSettings(ctx, voiceprefs.Prefs{Model: "example", AllowRemote: true}); err == nil {
		t.Error("remote model accepted without the administrator's policy")
	}
	// A voice of another language for Portuguese answers.
	if err := c.SetVoiceSettings(ctx, voiceprefs.Prefs{Voices: map[string]string{"pt": "en_US-ljspeech-medium"}}); err == nil {
		t.Error("an English voice accepted for Portuguese")
	}
	// Portuguese without a model of the person's: saved; push to talk uses
	// an installed multilingual model (or offers to download one).
	if err := c.SetVoiceSettings(ctx, voiceprefs.Prefs{SpeechLang: "pt-BR"}); err != nil {
		t.Fatalf("pt-BR without a chosen model: %v", err)
	}
	if st := c.VoiceSettings(ctx); st["speech_model_in_use"] != "ggml-small-q5_1" || st["speech_model_ready"] != true {
		t.Errorf("model in use for pt-BR: %v %v", st["speech_model_in_use"], st["speech_model_ready"])
	}
	// The owner's case: an English desktop, voice and answers in pt-BR.
	if err := c.SetVoiceSettings(ctx, voiceprefs.Prefs{SpeechLang: "pt-BR", SpeechModel: "ggml-small-q5_1"}); err != nil {
		t.Fatal(err)
	}
	eff := c.refreshPrefs()
	if eff.SpeechLang != "pt-BR" || eff.AnswerLang != "pt-BR" || eff.SpeechModel != "ggml-small-q5_1" {
		t.Errorf("effective: %+v", eff)
	}
	if i18n.Lang() != "pt_BR" {
		t.Errorf("the daemon's answer language is %q", i18n.Lang())
	}
	if c.translator() != local {
		t.Error("the local model must stay the person's model")
	}
	// No Portuguese voice: the answer is shown only.
	st := c.VoiceSettings(ctx)
	if st["voice"] != "" {
		t.Errorf("a voice for pt-BR: %v", st["voice"])
	}
	// The record of the change is in the activity log.
	found := false
	for _, r := range c.Audit.Tail(10) {
		if strings.Contains(r.Text, "voice and assistant settings changed") {
			found = true
		}
	}
	if !found {
		t.Error("no audit record")
	}

	// A hand edit outside the policy: not used, and the page says why.
	_ = os.WriteFile(c.prefsPath(), []byte("[speech]\nlanguage = pt-BR\nmodel = ggml-small-q5_1\n[model]\nchoice = example\nallow_remote = yes\n"), 0o644)
	future := time.Now().Add(2e9)
	_ = os.Chtimes(c.prefsPath(), future, future)
	c.refreshPrefs()
	st = c.VoiceSettings(ctx)
	if probs, _ := st["problems"].([]string); len(probs) == 0 || c.translator() != local {
		t.Errorf("remote choice outside the policy: %v", st["problems"])
	}
}

func TestVoicePressRefusesEnglishOnlyModel(t *testing.T) {
	c, _, dir := newCore(t)
	voiceLab(t, dir)
	_, cl := startFakeVoice(t, dir)
	c.Voice = cl
	c.ScreenLocked = func() bool { return false }
	_ = os.MkdirAll(filepath.Dir(c.prefsPath()), 0o700)
	// Written by hand: Portuguese with an English-only model.
	_ = os.WriteFile(c.prefsPath(), []byte("[speech]\nlanguage = pt-BR\nmodel = ggml-base.en\n"), 0o644)
	err := c.VoicePress(context.Background(), false)
	if err == nil || !strings.Contains(err.Error(), "ggml-base.en") {
		t.Fatalf("press: %v", err)
	}
	if st := c.VoiceStatus(); st.State != "error" || st.Lang != "pt-BR" {
		t.Errorf("card: %+v", st)
	}
}

func TestAssistantPrefixPortuguese(t *testing.T) {
	for in, want := range map[string]string{
		"Assistente, encontre o PDF do banco": "encontre o PDF do banco",
		"assistant find the PDF":              "find the PDF",
		"ok assistente: deixe mais escuro":    "deixe mais escuro",
		"Assistente \"Abra o resultado 2\".":  "Abra o resultado 2",
	} {
		got, ok := AssistantPrefix(in)
		if !ok || got != want {
			t.Errorf("%q: %q %v", in, got, ok)
		}
	}
	if _, ok := AssistantPrefix("assistentes sociais chegaram"); ok {
		t.Error("a word that only starts with assistente")
	}
}

// fakeVoice is a voice service on a Unix socket that records the
// operations it gets and hears the same words at every "stop".
type fakeVoice struct {
	mu   sync.Mutex
	ops  []string
	text string
}

func (f *fakeVoice) take() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	o := f.ops
	f.ops = nil
	return o
}

func startFakeVoice(t *testing.T, dir string) (*fakeVoice, *voice.Client) {
	t.Helper()
	sock := filepath.Join(dir, "voice.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { l.Close() })
	f := &fakeVoice{}
	models := voice.Models{
		STT:      []voice.ModelInfo{{Name: "ggml-base.en", Kind: "stt", Lang: "en", Allowed: true, Default: true}},
		Voices:   []voice.ModelInfo{{Name: "en_US-ljspeech-medium", Kind: "tts", Lang: "en-US", Allowed: true, Default: true}},
		Language: "en", DefaultSTT: "ggml-base.en", DefaultVoice: "en_US-ljspeech-medium", TTS: true,
	}
	go func() {
		for {
			conn, err := l.Accept()
			if err != nil {
				return
			}
			go func(conn net.Conn) {
				defer conn.Close()
				r := bufio.NewReader(conn)
				w := json.NewEncoder(conn)
				for {
					line, err := r.ReadBytes('\n')
					if err != nil {
						return
					}
					var req voice.Request
					_ = json.Unmarshal(line, &req)
					f.mu.Lock()
					f.ops = append(f.ops, req.Op)
					text := f.text
					f.mu.Unlock()
					rep := voice.Reply{ID: req.ID, OK: true}
					switch req.Op {
					case "stop":
						rep.Transcript = &voice.Transcript{Text: text, Speech: true, Model: "ggml-base.en", Lang: "en"}
					case "models":
						m := models
						rep.Models = &m
					case "speak":
						rep.Spoken = &voice.Spoken{Voice: req.Voice, Sentences: 1}
					}
					_ = w.Encode(rep)
				}
			}(conn)
		}
	}()
	return f, &voice.Client{Path: sock}
}

func hasOp(ops []string, op string) bool {
	for _, o := range ops {
		if o == op {
			return true
		}
	}
	return false
}

// TestSpokenAnswersOff: "stop speaking answers" is a confirmed change of
// the person's own setting; with it off push to talk still answers on
// the screen, nothing is synthesized and the card says nothing about
// speaking; "speak answers" turns it back on.
func TestSpokenAnswersOff(t *testing.T) {
	c, _, dir := newCore(t)
	voiceLab(t, dir)
	fv, cl := startFakeVoice(t, dir)
	c.Voice = cl
	c.ScreenLocked = func() bool { return false }
	ctx := context.Background()
	// English answers, so the English voice speaks them while on.
	if err := c.SetVoiceSettings(ctx, voiceprefs.Prefs{AnswerLang: "en-US"}); err != nil {
		t.Fatal(err)
	}

	// While on, a spoken request's answer is spoken.
	fv.text = "make it darker"
	fv.take()
	c.voiceTurn(time.Now())
	if ops := fv.take(); !hasOp(ops, "speak") {
		t.Fatalf("spoken answers on: no speak (%v)", ops)
	}

	// The request: a proposal the person confirms, like other settings.
	res := c.Ask(ctx, "stop speaking answers")
	if res.Kind != "proposal" || res.Proposal == nil || len(res.Proposal.Calls) != 1 || res.Proposal.Calls[0].Action != "voice.answers.set" {
		t.Fatalf("ask: %+v", res)
	}
	if b, _ := os.ReadFile(c.prefsPath()); strings.Contains(string(b), "spoken = no") {
		t.Fatal("changed before the confirmation")
	}
	if _, err := c.Decide(ctx, res.Proposal.ID, true, "ui"); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(c.prefsPath())
	if !strings.Contains(string(b), "spoken = no") || !strings.Contains(string(b), "language = en-US") {
		t.Fatalf("settings file:\n%s", b)
	}
	// The voice service stopped speaking and ended its synthesizer.
	if ops := fv.take(); !hasOp(ops, "hush") || !hasOp(ops, "unload") {
		t.Errorf("turning off sent %v", ops)
	}

	// Push to talk with spoken answers off: answered on the screen, no
	// voice looked up, nothing synthesized, no word about speaking.
	events, stop := c.Subscribe()
	defer stop()
	c.voiceTurn(time.Now())
	results := 0
	for len(events) > 0 {
		if ev := <-events; ev.Event == "voice-result" {
			results++
		}
	}
	ops := fv.take()
	if hasOp(ops, "speak") || hasOp(ops, "models") {
		t.Errorf("spoken answers off, the voice service got %v", ops)
	}
	if results != 1 {
		t.Errorf("answer shown %d times", results)
	}
	st := c.VoiceStatus()
	if st.State != "idle" || st.Error != "" {
		t.Errorf("card: %+v", st)
	}
	if card, _ := json.Marshal(st); strings.Contains(strings.ToLower(string(card)), "spoken") || strings.Contains(strings.ToLower(string(card)), "speak") {
		t.Errorf("the card mentions speaking: %s", card)
	}

	// "Fale as respostas": back on after the confirmation.
	res = c.Ask(ctx, "fale as respostas")
	if res.Kind != "proposal" || res.Proposal == nil {
		t.Fatalf("ask: %+v", res)
	}
	if _, err := c.Decide(ctx, res.Proposal.ID, true, "ui"); err != nil {
		t.Fatal(err)
	}
	if !c.refreshPrefs().Spoken {
		t.Fatal("still off")
	}
	fv.take()
	c.voiceTurn(time.Now())
	if ops := fv.take(); !hasOp(ops, "speak") {
		t.Errorf("spoken answers on again: no speak (%v)", ops)
	}
}

// TestSpokenAnswersPersonOnly: an agent cannot propose the change.
func TestSpokenAnswersPersonOnly(t *testing.T) {
	c, _, _ := newCore(t)
	_, err := c.Propose(context.Background(), Meta{Origin: "agent", Actor: "agent"}, []Call{{Action: "voice.answers.set", Args: map[string]any{"spoken": "off"}}})
	if err == nil {
		t.Fatal("an agent proposed a change of the person's voice settings")
	}
}

// TestMultilingualPick: a language other than English switches to the
// base quantized multilingual model first, then the small one.
func TestMultilingualPick(t *testing.T) {
	stt := func(names ...string) voice.Models {
		var m voice.Models
		for _, n := range names {
			m.STT = append(m.STT, voice.ModelInfo{Name: n, Kind: "stt", Multilingual: !voice.EnglishOnly(n), Allowed: true})
		}
		return m
	}
	for want, m := range map[string]voice.Models{
		"ggml-base-q5_1":           stt("ggml-base.en", "ggml-small-q5_1", "ggml-base-q5_1"),
		"ggml-small-q5_1":          stt("ggml-base.en", "ggml-large-v3-turbo-q5_0", "ggml-small-q5_1"),
		"ggml-large-v3-turbo-q5_0": stt("ggml-base.en", "ggml-large-v3-turbo-q5_0"),
		"":                         stt("ggml-base.en", "ggml-small.en"),
	} {
		if got := multilingualPick(m); got != want {
			t.Errorf("%+v: %q, want %q", m.STT, got, want)
		}
	}
	// One the administrator does not allow is skipped.
	m := stt("ggml-base-q5_1", "ggml-small-q5_1")
	m.STT[0].Allowed = false
	if got := multilingualPick(m); got != "ggml-small-q5_1" {
		t.Errorf("not allowed base: %q", got)
	}
}
