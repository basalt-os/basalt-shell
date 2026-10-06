package shell

import (
	"context"
	"errors"
	"os"
	"os/exec"
	"os/user"
	"time"

	"github.com/basalt-os/basalt-shell/internal/i18n"
	"github.com/basalt-os/basalt-shell/internal/intent"
	"github.com/basalt-os/basalt-shell/internal/models"
	"github.com/basalt-os/basalt-shell/internal/voice"
	"github.com/basalt-os/basalt-shell/internal/voiceprefs"
)

// Zero setup (Basalt OS rule: every feature works out of the box; asking
// the person to run a command is a bug).
//
//   - The voice service is a user unit enabled for every user by preset
//     and started with the session; when its socket is missing anyway
//     (a session started before the package was installed, a crash, a
//     unit stopped by hand) push to talk starts it, and the card says so
//     only if that fails.
//   - Push to talk without a speech model for the person's language
//     offers the download on the voice card (Download, Not now), with the
//     size and where it comes from; nothing is downloaded before the
//     person chooses Download. English gets ggml-base.en, any other
//     language the multilingual ggml-base-q5_1, both with the Silero
//     voice activity detector.
//   - A skill that needs the assistant's local model when none is
//     downloaded offers it the same way (once per session after Not now);
//     after the download the model service is turned on and the command
//     bar and the skills use it, without logging out.
//   - Settings, Voice and assistant, downloads and removes models with
//     one click.
//
// The download itself is the system's (internal/models: polkit, the
// confined basalt-models service, pinned URLs and SHA-256); the person's
// consent is recorded in the activity log and in basalt-ledger.

// DefaultMultilingualModel is the speech model a language other than
// English gets when the person chose none (fast, and it heard short
// requests best in the lab).
const DefaultMultilingualModel = "ggml-base-q5_1"

// Paths of Basalt OS (variables for tests).
var (
	VoiceUnitPath = "/usr/lib/systemd/user/basalt-voice.service"
	AssistantConf = "/etc/basalt/assistant.conf"
)

// speechModelFor is the speech model push to talk uses: the person's
// choice; else the system's default when it understands the speech
// language; else an installed multilingual model the administrator
// allows (ggml-base-q5_1 first); else ggml-base-q5_1, which the voice
// card offers to download.
func speechModelFor(eff voiceprefs.Effective, sys voice.System, ms voice.Models) string {
	if eff.SpeechModel != "" {
		return eff.SpeechModel
	}
	def := voice.ModelName(sys.STTModel)
	english := eff.SpeechLang != "auto" && i18n.Base(eff.SpeechLang) == "en"
	if english || !voice.EnglishOnly(def) {
		return def
	}
	if p := multilingualPick(ms); p != "" {
		return p
	}
	return DefaultMultilingualModel
}

// voiceTarget is what a download of a speech model asks for: the english
// and multilingual sets of basalt-voice-fetch, or the model by name (the
// detector comes with it).
func voiceTarget(model string) string {
	switch model {
	case "ggml-base.en":
		return "english"
	case DefaultMultilingualModel:
		return "multilingual"
	}
	return model
}

// voiceReady reports whether the speech model and the voice activity
// detector are installed.
func voiceReady(model string, sys voice.System, ms voice.Models) bool {
	found := false
	for _, m := range ms.STT {
		if m.Name == model {
			found = true
		}
	}
	if !found {
		return false
	}
	if sys.VADModel != "" {
		if st, err := os.Stat(sys.VADModel); err != nil || !st.Mode().IsRegular() {
			return false
		}
	}
	return true
}

// voiceInstalled reports whether the voice service is installed (its
// socket may still be missing: push to talk starts it).
func (c *Core) voiceInstalled() bool {
	if c.Voice == nil {
		return false
	}
	if c.Voice.Available() {
		return true
	}
	_, err := os.Stat(VoiceUnitPath)
	return err == nil
}

// ensureVoice starts the voice service when its socket is missing and
// waits a few seconds for it.
func (c *Core) ensureVoice(ctx context.Context) error {
	if c.Voice == nil {
		return errors.New("no voice client")
	}
	if c.Voice.Available() {
		return nil
	}
	start := c.StartVoice
	if start == nil {
		start = startVoiceUnit
	}
	if err := start(ctx); err != nil {
		return err
	}
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		if c.Voice.Available() {
			_, _ = c.Audit.Append("apply", "daemon", "voice service started for push to talk", nil)
			return nil
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(100 * time.Millisecond):
		}
	}
	return errors.New("the voice service did not start in time")
}

// startVoiceUnit starts basalt-voice.service in the person's systemd.
func startVoiceUnit(ctx context.Context) error {
	ctx, cancel := context.WithTimeout(ctx, 10*time.Second)
	defer cancel()
	out, err := exec.CommandContext(ctx, "systemctl", "--user", "start", "basalt-voice.service").CombinedOutput()
	if err != nil {
		return errors.New(string(out))
	}
	return nil
}

// offerVoice shows the speech model download on the voice card (or the
// download already running).
func (c *Core) offerVoice(ctx context.Context, eff voiceprefs.Effective, model string, sys voice.System) error {
	target := voiceTarget(model)
	lang := eff.SpeechLang
	langName := nativeName(lang)
	if lang == "auto" {
		langName = i18n.G("automatic language detection")
	}
	if c.Models != nil {
		if j, ok := c.Models.Job(models.Voice + "-" + target); ok && !j.Final() {
			c.setVoice(VoiceState{State: "download", Download: &j})
			return nil
		}
	}
	if c.Models == nil || !c.Models.Available(models.Voice) {
		msg := i18n.G("No speech model for %s is installed, and this computer cannot download one. Ask your administrator.", langName)
		c.setVoice(VoiceState{State: "error", Error: msg})
		return errors.New(msg)
	}
	if err := sys.Allowed(model, 0); err != nil {
		msg := voiceError(err, model)
		c.setVoice(VoiceState{State: "error", Error: msg})
		return errors.New(msg)
	}
	o, err := c.Models.NewOffer(ctx, models.Voice, target, "push-to-talk", lang)
	if err != nil || o == nil {
		msg := i18n.G("The speech model for %s cannot be downloaded here.", langName)
		if err != nil {
			_, _ = c.Audit.Append("refuse", "daemon", "speech model offer failed: "+err.Error(), map[string]any{"model": model})
		}
		c.setVoice(VoiceState{State: "error", Error: msg})
		return errors.New(msg)
	}
	_, _ = c.Audit.Append("propose", "ui", "push to talk: the speech model is not downloaded; download offered",
		map[string]any{"model": model, "target": target, "bytes": o.Bytes, "language": lang})
	c.setVoice(VoiceState{State: "offer", Offer: o, LangName: langName})
	return nil
}

// offerLocalModel offers the assistant's local model when a skill needed
// it and none is downloaded (at most once per session after Not now).
func (c *Core) offerLocalModel(ctx context.Context, request string) {
	if c.Models == nil || !c.Models.Available(models.LLM) || models.HasLLMModel() {
		return
	}
	c.mu.Lock()
	declined := c.llmDeclined
	c.mu.Unlock()
	if declined {
		return
	}
	if j, ok := c.Models.Job(models.LLM + "-recommended"); ok && !j.Final() {
		return
	}
	for _, o := range c.Models.Snapshot().Offers {
		if o.Kind == models.LLM {
			return
		}
	}
	eff := c.refreshPrefs()
	o, err := c.Models.NewOffer(ctx, models.LLM, "recommended", "skill", eff.AnswerLang)
	if err != nil || o == nil {
		return
	}
	_, _ = c.Audit.Append("propose", "ui", "a skill needs the assistant's local model; download offered",
		map[string]any{"bytes": o.Bytes, "files": o.Files, "request_chars": len([]rune(request))})
}

// consent records the person's consent to a download.
func (c *Core) consent(kind, target, purpose string, bytes int64, files []models.File) {
	data := map[string]any{"kind": kind, "what": target, "purpose": purpose, "bytes": bytes}
	if u, err := user.Current(); err == nil {
		data["user"] = u.Username
	}
	var names []string
	for _, f := range files {
		if !f.Present {
			names = append(names, f.Name)
		}
	}
	if len(names) > 0 {
		data["models"] = names
	}
	_, _ = c.Audit.Append("apply", "ui", "model download agreed: "+kind+" "+target, data)
	c.Ledger.Append("model.download.consent", "allowed", "", data)
}

// AcceptOffer starts the download of an offer: the person chose
// Download. Only the shell UI calls it.
func (c *Core) AcceptOffer(id string) (models.Job, error) {
	if c.Models == nil {
		return models.Job{}, errors.New("model downloads are not available")
	}
	o, ok := c.Models.Offer(id)
	if !ok {
		return models.Job{}, errors.New(i18n.G("This download offer is no longer open."))
	}
	if o.Ask == models.AskNone {
		return models.Job{}, errors.New(i18n.G("The administrator turned model downloads off on this computer."))
	}
	j, err := c.Models.Accept(id)
	if err != nil {
		_, _ = c.Audit.Append("refuse", "ui", "model download not started: "+err.Error(), map[string]any{"kind": o.Kind, "what": o.Target})
		return j, err
	}
	c.consent(o.Kind, o.Target, o.Purpose, o.Bytes, o.Files)
	if o.Kind == models.Voice {
		c.setVoice(VoiceState{State: "download", Download: &j})
	}
	return j, nil
}

// StartDownload starts a download from Settings, where the person chose
// Download next to the model and its size.
func (c *Core) StartDownload(ctx context.Context, kind, target string) (models.Job, error) {
	if c.Models == nil {
		return models.Job{}, errors.New("model downloads are not available")
	}
	p, err := c.Models.Plan(ctx, kind, target)
	if err != nil {
		return models.Job{}, err
	}
	j, err := c.Models.Start(kind, target, "settings", "", p.Missing())
	if err != nil {
		return j, err
	}
	c.consent(kind, target, "settings", p.Missing(), p.Files)
	return j, nil
}

// DismissOffer closes an offer (Not now).
func (c *Core) DismissOffer(id string) {
	if c.Models == nil {
		return
	}
	o, ok := c.Models.Offer(id)
	c.Models.Dismiss(id)
	if !ok {
		return
	}
	_, _ = c.Audit.Append("decline", "ui", "model download declined (not now): "+o.Kind+" "+o.Target, nil)
	if o.Kind == models.LLM {
		c.mu.Lock()
		c.llmDeclined = true
		c.mu.Unlock()
	}
	if st := c.VoiceStatus(); st.State == "offer" && st.Offer != nil && st.Offer.ID == id {
		c.setVoice(VoiceState{State: "idle"})
	}
}

// CloseDownload clears an ended download from the card, or stops a
// download waiting for the network.
func (c *Core) CloseDownload(id string) {
	if c.Models == nil {
		return
	}
	c.Models.Cancel(id)
	if st := c.VoiceStatus(); st.State == "download" && st.Download != nil && st.Download.ID == id {
		c.setVoice(VoiceState{State: "idle"})
	}
}

// WireModels connects the download manager to the UI and the voice card.
func (c *Core) WireModels() {
	if c.Models == nil {
		return
	}
	c.Models.OnChange = func() {
		st := c.Models.Snapshot()
		c.Broadcast("models", st)
		// The voice card follows the download it shows.
		cur := c.VoiceStatus()
		if cur.State != "download" || cur.Download == nil {
			return
		}
		for _, j := range st.Jobs {
			if j.ID == cur.Download.ID && j != *cur.Download {
				jj := j
				c.setVoice(VoiceState{State: "download", Download: &jj})
			}
		}
	}
	c.Models.OnDone = c.downloadDone
}

// downloadDone tells the person how a download ended.
func (c *Core) downloadDone(j models.Job) {
	data := map[string]any{"kind": j.Kind, "what": j.Target, "state": j.State, "error": j.Error, "bytes": j.Total, "attempts": j.Attempts}
	switch j.State {
	case models.StateDone:
		_, _ = c.Audit.Append("apply", "daemon", "model downloaded and verified: "+j.Kind+" "+j.Target, data)
	case models.StateFailed:
		_, _ = c.Audit.Append("fail", "daemon", "model download failed: "+j.Kind+" "+j.Target+" ("+j.Error+")", data)
	}
	cur := c.VoiceStatus()
	onCard := cur.State == "download" && cur.Download != nil && cur.Download.ID == j.ID
	switch {
	case j.Kind == models.Voice && j.State == models.StateDone:
		c.Broadcast("notify", map[string]any{"summary": i18n.G("Voice is ready"),
			"body": i18n.G("Hold Super+V, or the microphone button in the panel, and speak."), "urgency": "normal",
			"app": i18n.G("Voice and assistant"), "page": "voice"})
		if onCard {
			c.setVoice(VoiceState{State: "idle", Note: i18n.G("Voice is ready. Hold Super+V and speak.")})
		}
		c.Broadcast("voice-settings", c.VoiceSettings(context.Background()))
	case j.Kind == models.Voice && j.State == models.StateCancelled:
		if onCard {
			c.setVoice(VoiceState{State: "idle"})
		}
	case j.Kind == models.Voice:
		if onCard {
			jj := j
			c.setVoice(VoiceState{State: "download", Download: &jj})
		}
	case j.Kind == models.LLM && j.State == models.StateDone:
		c.reloadLocalModel()
		c.Broadcast("notify", map[string]any{"summary": i18n.G("The assistant's local model is ready"),
			"body": i18n.G("It runs on this computer. Ask again: summaries and requests in your own words now use it."), "urgency": "normal",
			"app": i18n.G("Voice and assistant"), "page": "voice"})
	}
}

// reloadLocalModel reads the assistant's model settings again (the
// download turned the local model on) and applies them to the command
// bar and the skills.
func (c *Core) reloadLocalModel() {
	m := intent.FromAssistantConfig(AssistantConf)
	if m != nil && c.Assistant != nil {
		m.Helper = c.Assistant.TranslateHelper()
	}
	c.SetLocalModel(m)
	c.Broadcast("translator", map[string]any{"available": m != nil})
	c.Broadcast("voice-settings", c.VoiceSettings(context.Background()))
}

// ModelsList is the Settings page's data: the models of the manifests,
// what is downloaded, the downloads and the policy.
func (c *Core) ModelsList(ctx context.Context) map[string]any {
	if c.Models == nil {
		return map[string]any{"available": false}
	}
	cat := c.Models.List(ctx)
	st := c.Models.Snapshot()
	return map[string]any{"available": true, "voice": c.Models.Available(models.Voice), "llm": c.Models.Available(models.LLM),
		"catalog": cat, "state": st, "llm_installed": models.HasLLMModel()}
}
