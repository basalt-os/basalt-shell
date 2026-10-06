package shell

import (
	"context"
	"os"
	"path/filepath"
	"strings"
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
	for _, n := range []string{"ggml-base.en.bin", "ggml-small-q5_1.bin", "en_US-ljspeech-medium.onnx", "en_US-ljspeech-medium.onnx.json"} {
		_ = os.WriteFile(filepath.Join(models, n), []byte("x"), 0o644)
	}
	conf := filepath.Join(dir, "voice.conf")
	_ = os.WriteFile(conf, []byte("BASALT_VOICE_STT_MODEL="+filepath.Join(models, "ggml-base.en.bin")+"\nBASALT_VOICE_TTS_MODEL="+
		filepath.Join(models, "en_US-ljspeech-medium.onnx")+"\n"), 0o644)
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

	// Portuguese with the English-only default model: refused, with the reason.
	err := c.SetVoiceSettings(ctx, voiceprefs.Prefs{SpeechLang: "pt-BR"})
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
	c.Voice = &voice.Client{Path: filepath.Join(dir, "none.sock")}
	c.ScreenLocked = func() bool { return false }
	_ = os.MkdirAll(filepath.Dir(c.prefsPath()), 0o700)
	// Written by hand: Portuguese with the English-only default model.
	_ = os.WriteFile(c.prefsPath(), []byte("[speech]\nlanguage = pt-BR\n"), 0o644)
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
