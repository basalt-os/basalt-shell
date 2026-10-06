package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"sync"
	"time"

	"github.com/basalt-os/basalt-shell/internal/i18n"
	"github.com/basalt-os/basalt-shell/internal/intent"
	"github.com/basalt-os/basalt-shell/internal/voice"
	"github.com/basalt-os/basalt-shell/internal/voiceprefs"
)

// The person's voice and assistant settings (internal/voiceprefs): their
// speech language and model, their answer language and voice, and their
// model choice within the administrator's policy. They are independent of
// the desktop's language: an English desktop can be spoken to, and
// answer, in Brazilian Portuguese.
//
// The daemon is the only reader of the person's file (the voice service
// and the skill workers get the values with each request) and its only
// writer, when the person changes them in Settings (voice.settings.set,
// shell UI only). A file edited by hand is picked up at the next request
// (its modification time is checked); a choice outside the policy is not
// used and the settings page says why.

// SystemVoiceConf is the administrator's voice settings and policy.
var SystemVoiceConf = "/etc/basalt/voice.conf"

type prefsState struct {
	mu       sync.Mutex
	loaded   bool
	mod      time.Time
	prefs    voiceprefs.Prefs
	eff      voiceprefs.Effective
	problems []string
	system   voice.System
	policy   voiceprefs.Policy
	// local is the system's model settings (the [translator] section);
	// the person's choice replaces it for them only.
	local    *intent.Model
	localSet bool
	model    *intent.Model
}

// SetLocalModel sets the system's language model (nil: none). The
// person's choice is applied on top of it.
func (c *Core) SetLocalModel(m *intent.Model) {
	c.prefs.mu.Lock()
	c.prefs.local, c.prefs.localSet, c.prefs.loaded = m, true, false
	c.prefs.mu.Unlock()
	c.Translator = m
	c.refreshPrefs()
}

func (c *Core) prefsPath() string { return voiceprefs.Path(c.ConfigDir) }

// refreshPrefs reads the settings when the file changed (or on the first
// call) and applies them: the daemon's answer language, the model of the
// command bar and of the skills. It returns the settings in force.
func (c *Core) refreshPrefs() voiceprefs.Effective {
	ps := &c.prefs
	ps.mu.Lock()
	var mod time.Time
	if st, err := os.Stat(c.prefsPath()); err == nil {
		mod = st.ModTime()
	}
	if ps.loaded && mod.Equal(ps.mod) {
		e := ps.eff
		ps.mu.Unlock()
		return e
	}
	if !ps.localSet {
		ps.local, ps.localSet = c.Translator, true
	}
	p, err := voiceprefs.Load(c.prefsPath(), homeOf(c.ConfigDir))
	ps.problems = nil
	if err != nil {
		ps.problems = append(ps.problems, i18n.G("Your voice settings could not be read: %s", err.Error()))
	}
	if err := p.Validate(); err != nil {
		ps.problems = append(ps.problems, i18n.G("Your voice settings have a mistake (%s); the defaults are used.", err.Error()))
		p = voiceprefs.Prefs{Voices: map[string]string{}}
	}
	ps.system = voice.SystemFromFile(SystemVoiceConf)
	ps.policy = voiceprefs.LoadPolicy(voiceprefs.PolicyPath)
	ps.prefs, ps.mod, ps.loaded = p, mod, true
	ps.eff = voiceprefs.Resolve(p, ps.system.Language, i18n.SessionTag())
	m, err := voiceprefs.ChooseModel(ps.policy, p, ps.local)
	if err != nil {
		// Outside the policy: the local model, and the reason on the page.
		ps.problems = append(ps.problems, err.Error())
		ps.eff.Model = "local"
	}
	ps.model = m
	eff := ps.eff
	ps.mu.Unlock()

	// What the assistant says is in the answer language.
	i18n.Load(eff.AnswerLang)
	c.mu.Lock()
	c.Translator = m
	c.mu.Unlock()
	if c.Skills != nil {
		c.Skills.SetModel(m, eff.AnswerLang)
	}
	return eff
}

// translator is the language model of the person's command bar.
func (c *Core) translator() *intent.Model {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.Translator
}

func homeOf(configDir string) string {
	if h, err := os.UserHomeDir(); err == nil {
		return h
	}
	return configDir
}

// voiceModels asks the voice service which models are installed and
// allowed (it is the one that enforces the policy); without it, the same
// listing is made here from the system settings.
func (c *Core) voiceModels(ctx context.Context) voice.Models {
	if c.Voice != nil && c.Voice.Available() {
		ctx, cancel := context.WithTimeout(ctx, 3*time.Second)
		defer cancel()
		if rep, err := c.Voice.Do(ctx, voice.Request{Op: "models"}); err == nil && rep.Models != nil {
			return *rep.Models
		}
	}
	c.prefs.mu.Lock()
	sys := c.prefs.system
	c.prefs.mu.Unlock()
	return sys.List()
}

// VoiceSettings is the "Voice and assistant" page's data.
func (c *Core) VoiceSettings(ctx context.Context) map[string]any {
	eff := c.refreshPrefs()
	models := c.voiceModels(ctx)
	ps := &c.prefs
	ps.mu.Lock()
	defer ps.mu.Unlock()
	var remotes []map[string]string
	for _, r := range ps.policy.Remotes {
		remotes = append(remotes, map[string]string{"name": r.Name, "label": r.Label})
	}
	voiceName, _ := voiceFor(eff, ps.prefs.Voices, models)
	return map[string]any{
		"prefs": ps.prefs, "effective": eff, "problems": ps.problems,
		"models": models, "languages": i18n.Languages, "session": i18n.SessionTag(),
		"policy":   map[string]any{"allow_remote": ps.policy.AllowRemote, "remotes": remotes},
		"local":    ps.local != nil,
		"voice":    voiceName,
		"path":     c.prefsPath(),
		"language": i18n.EnglishName(eff.AnswerLang),
	}
}

// SetVoiceSettings checks and saves the person's settings. Only the
// shell UI calls it (the Settings window); every value is checked against
// the administrator's policy first, and refused with a message when it
// is outside it.
func (c *Core) SetVoiceSettings(ctx context.Context, p voiceprefs.Prefs) error {
	if p.Voices == nil {
		p.Voices = map[string]string{}
	}
	if err := p.Validate(); err != nil {
		return err
	}
	c.refreshPrefs()
	c.prefs.mu.Lock()
	pol, sysLang := c.prefs.policy, c.prefs.system.Language
	c.prefs.mu.Unlock()
	if err := pol.Check(p); err != nil {
		return err
	}
	models := c.voiceModels(ctx)
	model := models.DefaultSTT
	if p.SpeechModel != "" {
		var found *voice.ModelInfo
		for i := range models.STT {
			if models.STT[i].Name == p.SpeechModel {
				found = &models.STT[i]
			}
		}
		switch {
		case found == nil:
			return errors.New(i18n.G("The speech model %s is not installed.", p.SpeechModel))
		case !found.Allowed:
			return errors.New(i18n.G("Your administrator does not allow the speech model %s.", p.SpeechModel))
		}
		model = p.SpeechModel
	}
	lang := p.SpeechLang
	if lang == "" {
		lang = sysLang
	}
	if _, err := voice.WhisperLanguage(lang, model); voice.ErrorCode(err) == voice.CodeEnglishOnly {
		return errors.New(i18n.G("The speech model %s understands English only. Choose a multilingual model for %s.", model, i18n.EnglishName(lang)))
	}
	for l, v := range p.Voices {
		var found *voice.ModelInfo
		for i := range models.Voices {
			if models.Voices[i].Name == v {
				found = &models.Voices[i]
			}
		}
		switch {
		case found == nil:
			return errors.New(i18n.G("The voice %s is not installed.", v))
		case !found.Allowed:
			return errors.New(i18n.G("Your administrator does not allow the voice %s.", v))
		case i18n.Base(found.Lang) != i18n.Base(l):
			return errors.New(i18n.G("The voice %s does not speak that language.", v))
		}
	}
	if err := voiceprefs.Save(c.prefsPath(), p); err != nil {
		return err
	}
	_, _ = c.Audit.Append("apply", "ui", "voice and assistant settings changed", map[string]any{
		"speech_language": p.SpeechLang, "speech_model": p.SpeechModel, "answer_language": p.AnswerLang,
		"spoken": p.Spoken, "voices": p.Voices, "model": p.Model, "allow_remote": p.AllowRemote})
	c.prefs.mu.Lock()
	c.prefs.loaded = false
	c.prefs.mu.Unlock()
	c.refreshPrefs()
	c.Broadcast("voice-settings", c.VoiceSettings(ctx))
	st := c.VoiceStatus()
	if st.State == "idle" {
		c.setVoice(VoiceState{State: "idle"})
	}
	return nil
}

// voiceFor picks the voice for the answer language: the person's choice
// for it, else the system's default voice if it speaks that language,
// else any allowed installed voice of the language. "" when there is
// none: the answer is shown and not spoken.
func voiceFor(eff voiceprefs.Effective, chosen map[string]string, models voice.Models) (string, bool) {
	if !models.TTS {
		return "", false
	}
	base := i18n.Base(eff.AnswerLang)
	ok := func(name string) bool {
		for _, v := range models.Voices {
			if v.Name == name && v.Allowed && i18n.Base(v.Lang) == base {
				return true
			}
		}
		return false
	}
	if v := chosen[base]; v != "" && ok(v) {
		return v, true
	}
	if ok(models.DefaultVoice) {
		return models.DefaultVoice, true
	}
	for _, v := range models.Voices {
		if v.Allowed && i18n.Base(v.Lang) == base {
			return v.Name, true
		}
	}
	return "", false
}

// voiceError says a voice service refusal in the person's language.
func voiceError(err error, model string) string {
	switch voice.ErrorCode(err) {
	case voice.CodeEnglishOnly:
		return i18n.G("The speech model %s understands English only. Choose a multilingual model or English as your speech language in Settings, Voice and assistant.", model)
	case voice.CodeNotAllowed:
		return i18n.G("Your administrator does not allow the speech model %s. Choose another one in Settings, Voice and assistant.", model)
	case voice.CodeNoModel:
		return i18n.G("The speech model %s is not installed. Choose another one in Settings, Voice and assistant.", model)
	case voice.CodeBadLanguage:
		return i18n.G("Your speech language setting is not a language. Choose one in Settings, Voice and assistant.")
	}
	return fmt.Sprint(err)
}
