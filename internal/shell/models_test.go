package shell

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/basalt-os/basalt-shell/internal/models"
	"github.com/basalt-os/basalt-shell/internal/voice"
	"github.com/basalt-os/basalt-shell/internal/voiceprefs"
)

func TestSpeechModelFor(t *testing.T) {
	sys := voice.System{STTModel: "/m/ggml-base.en.bin"}
	stt := func(names ...string) voice.Models {
		var m voice.Models
		for _, n := range names {
			m.STT = append(m.STT, voice.ModelInfo{Name: n, Multilingual: !voice.EnglishOnly(n), Allowed: true})
		}
		return m
	}
	cases := []struct {
		eff  voiceprefs.Effective
		ms   voice.Models
		want string
	}{
		{voiceprefs.Effective{SpeechLang: "en-US"}, stt(), "ggml-base.en"},
		{voiceprefs.Effective{SpeechLang: "en"}, stt("ggml-base.en"), "ggml-base.en"},
		// Another language, nothing multilingual installed: the default
		// multilingual model, which the card offers to download.
		{voiceprefs.Effective{SpeechLang: "pt-BR"}, stt("ggml-base.en"), "ggml-base-q5_1"},
		{voiceprefs.Effective{SpeechLang: "auto"}, stt("ggml-base.en"), "ggml-base-q5_1"},
		{voiceprefs.Effective{SpeechLang: "pt-BR"}, stt("ggml-base.en", "ggml-small-q5_1"), "ggml-small-q5_1"},
		// The person's own choice is kept, whatever it is.
		{voiceprefs.Effective{SpeechLang: "pt-BR", SpeechModel: "ggml-small-q5_1"}, stt(), "ggml-small-q5_1"},
	}
	for _, c := range cases {
		if got := speechModelFor(c.eff, sys, c.ms); got != c.want {
			t.Errorf("%+v: %q, want %q", c.eff, got, c.want)
		}
	}
	// A multilingual system default serves every language.
	if got := speechModelFor(voiceprefs.Effective{SpeechLang: "pt-BR"}, voice.System{STTModel: "/m/ggml-small.bin"}, stt()); got != "ggml-small" {
		t.Errorf("multilingual default: %q", got)
	}
	for model, want := range map[string]string{"ggml-base.en": "english", "ggml-base-q5_1": "multilingual", "ggml-small-q5_1": "ggml-small-q5_1"} {
		if got := voiceTarget(model); got != want {
			t.Errorf("target of %s: %s", model, got)
		}
	}
}

// TestVoicePressStartsVoiceService: the socket is missing (the unit did
// not run yet); push to talk starts the service and opens the microphone,
// with no error on the card.
func TestVoicePressStartsVoiceService(t *testing.T) {
	c, _, dir := newCore(t)
	voiceLab(t, dir)
	c.Voice = &voice.Client{Path: filepath.Join(dir, "voice.sock")}
	c.ScreenLocked = func() bool { return false }
	started := 0
	c.StartVoice = func(ctx context.Context) error {
		started++
		_, _ = startFakeVoice(t, dir)
		return nil
	}
	if err := c.VoicePress(context.Background(), false); err != nil {
		t.Fatalf("press: %v", err)
	}
	if st := c.VoiceStatus(); st.State != "listening" || st.Error != "" || !st.Enabled {
		t.Errorf("card: %+v", st)
	}
	if started != 1 {
		t.Errorf("started %d times", started)
	}
	found := false
	for _, r := range c.Audit.Tail(20) {
		found = found || strings.Contains(r.Text, "voice service started for push to talk")
	}
	if !found {
		t.Error("no audit record of the start")
	}
}

// TestVoicePressStartFails: when the service cannot start, the card says
// so in plain words, never the socket error.
func TestVoicePressStartFails(t *testing.T) {
	c, _, dir := newCore(t)
	voiceLab(t, dir)
	c.Voice = &voice.Client{Path: filepath.Join(dir, "voice.sock")}
	c.ScreenLocked = func() bool { return false }
	c.StartVoice = func(ctx context.Context) error { return os.ErrNotExist }
	err := c.VoicePress(context.Background(), false)
	if err == nil {
		t.Fatal("press without a service succeeded")
	}
	st := c.VoiceStatus()
	if st.State != "error" || strings.Contains(st.Error, "dial unix") || !strings.Contains(st.Error, "could not start") {
		t.Errorf("card: %+v", st)
	}
	// The panel's button stays: the unit is installed.
	VoiceUnitPath = filepath.Join(dir, "basalt-voice.service")
	defer func() { VoiceUnitPath = "/usr/lib/systemd/user/basalt-voice.service" }()
	_ = os.WriteFile(VoiceUnitPath, []byte("[Unit]\n"), 0o644)
	if !c.VoiceStatus().Enabled {
		t.Error("voice shown as not installed while its unit is")
	}
}

// fakeModels plays the download tools, polkit, the request program and
// the download service for the shell's tests.
type fakeModels struct {
	mu      sync.Mutex
	calls   []string
	present map[string]bool
	prog    map[string]models.Progress
}

func (f *fakeModels) exec(ctx context.Context, argv []string) ([]byte, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, strings.Join(argv, " "))
	st := func(n string) string {
		if f.present[n] {
			return "present"
		}
		return "absent"
	}
	switch {
	case argv[0] == models.VoiceFetch && argv[1] == "--plan" && argv[2] == "multilingual":
		return []byte("ggml-base-q5_1 59707625 MIT huggingface.co " + st("ggml-base-q5_1") + "\nggml-silero-v5.1.2 885098 MIT huggingface.co present\n"), 0, nil
	case argv[0] == models.VoiceFetch && argv[1] == "--plan" && argv[2] == "english":
		return []byte("ggml-base.en 147964211 MIT huggingface.co " + st("ggml-base.en") + "\nggml-silero-v5.1.2 885098 MIT huggingface.co present\n"), 0, nil
	case argv[0] == models.LLMFetch && argv[1] == "--plan" && argv[2] == "recommended":
		return []byte("qwen3-1.7b-q8_0 1834426016 Apache-2.0 huggingface.co absent\n"), 0, nil
	case argv[0] == "pkexec" && argv[2] == "download":
		id := argv[3] + "-" + argv[4]
		f.prog[id] = models.Progress{State: models.StateDone, Bytes: 100, Total: 100, Time: time.Now()}
		return []byte("started " + id), 0, nil
	}
	return nil, 2, nil
}

func newFakeModels(t *testing.T, dir string) (*models.Manager, *fakeModels) {
	t.Helper()
	old := []string{models.VoiceFetch, models.LLMFetch, models.Request, models.RequestAdmin, models.PolicyPath, models.LLMModelDir}
	t.Cleanup(func() {
		models.VoiceFetch, models.LLMFetch, models.Request, models.RequestAdmin, models.PolicyPath, models.LLMModelDir =
			old[0], old[1], old[2], old[3], old[4], old[5]
	})
	md := filepath.Join(dir, "mtools")
	_ = os.MkdirAll(md, 0o755)
	for _, p := range []*string{&models.VoiceFetch, &models.LLMFetch, &models.Request, &models.RequestAdmin} {
		*p = filepath.Join(md, filepath.Base(*p))
		_ = os.WriteFile(*p, []byte("#!/bin/sh\n"), 0o755)
	}
	models.PolicyPath = filepath.Join(md, "models.conf")
	models.LLMModelDir = filepath.Join(md, "llm")
	_ = os.MkdirAll(models.LLMModelDir, 0o755)
	f := &fakeModels{present: map[string]bool{}, prog: map[string]models.Progress{}}
	m := &models.Manager{Exec: f.exec, Pkexec: "pkexec", Poll: 5 * time.Millisecond, RetryEvery: 10 * time.Millisecond, Stale: time.Second,
		LoadPolicy: func() models.Policy { return models.LoadPolicy(models.PolicyPath) }, IsAdmin: func(string) bool { return false },
		Online: func() bool { return true }, ServiceRunning: func(string) bool { return true },
		ReadProgress: func(id string) (models.Progress, bool) {
			f.mu.Lock()
			defer f.mu.Unlock()
			p, ok := f.prog[id]
			return p, ok
		}}
	return m, f
}

// TestVoicePressOffersSpeechModel: a Portuguese session, no multilingual
// speech model: push to talk offers ggml-base-q5_1 (in the person's
// language, with its size and where it comes from) and downloads nothing
// before Download; after the download push to talk works.
func TestVoicePressOffersSpeechModel(t *testing.T) {
	c, _, dir := newCore(t)
	voiceLab(t, dir)
	t.Setenv("LANG", "pt_BR.UTF-8")
	fv, cl := startFakeVoice(t, dir) // lists only ggml-base.en
	c.Voice = cl
	c.ScreenLocked = func() bool { return false }
	m, fm := newFakeModels(t, dir)
	c.Models = m
	c.WireModels()
	events, stop := c.Subscribe()
	defer stop()
	ctx := context.Background()

	if err := c.VoicePress(ctx, false); err != nil {
		t.Fatalf("press: %v", err)
	}
	st := c.VoiceStatus()
	if st.State != "offer" || st.Offer == nil || st.Offer.Target != "multilingual" || st.Offer.Lang != "pt-BR" ||
		st.Offer.Bytes != 59707625 || st.Offer.Host != "huggingface.co" || st.Offer.Ask != models.AskPerson || st.LangName != "Português (Brasil)" {
		t.Fatalf("card: %+v offer %+v", st, st.Offer)
	}
	if hasOp(fv.take(), "listen") {
		t.Error("the microphone opened without a speech model")
	}
	// Releasing the key does nothing; nothing was downloaded.
	if err := c.VoiceRelease(ctx); err != nil || c.VoiceStatus().State != "offer" {
		t.Errorf("release: %v %+v", err, c.VoiceStatus())
	}
	for _, call := range fm.calls {
		if strings.HasPrefix(call, "pkexec") {
			t.Fatal("download before consent")
		}
	}
	// Download.
	j, err := c.AcceptOffer(st.Offer.ID)
	if err != nil || j.ID != "voice-multilingual" {
		t.Fatalf("accept: %+v %v", j, err)
	}
	deadline := time.Now().Add(5 * time.Second)
	var notified bool
	for time.Now().Before(deadline) && !notified {
		select {
		case ev := <-events:
			if ev.Event == "notify" {
				d, _ := ev.Data.(map[string]any)
				notified = strings.Contains(d["summary"].(string), "Voice is ready")
			}
		case <-time.After(50 * time.Millisecond):
		}
	}
	if !notified {
		t.Fatal("no notification when voice was ready")
	}
	if st := c.VoiceStatus(); st.State != "idle" || !strings.Contains(st.Note, "Voice is ready") {
		t.Errorf("card after the download: %+v", st)
	}
	var agreed bool
	for _, r := range c.Audit.Tail(30) {
		agreed = agreed || strings.Contains(r.Text, "model download agreed: voice multilingual")
	}
	if !agreed {
		t.Error("the consent is not in the activity log")
	}
	found := false
	for _, call := range fm.calls {
		found = found || call == "pkexec "+models.Request+" download voice multilingual"
	}
	if !found {
		t.Errorf("request: %v", fm.calls)
	}
}

// TestVoiceOfferNotNow: Not now closes the card; nothing downloads.
func TestVoiceOfferNotNow(t *testing.T) {
	c, _, dir := newCore(t)
	voiceLab(t, dir)
	t.Setenv("LANG", "pt_BR.UTF-8")
	_, cl := startFakeVoice(t, dir)
	c.Voice = cl
	c.ScreenLocked = func() bool { return false }
	m, fm := newFakeModels(t, dir)
	c.Models = m
	c.WireModels()
	if err := c.VoicePress(context.Background(), false); err != nil {
		t.Fatal(err)
	}
	st := c.VoiceStatus()
	c.DismissOffer(st.Offer.ID)
	if st := c.VoiceStatus(); st.State != "idle" || st.Offer != nil {
		t.Errorf("after Not now: %+v", st)
	}
	if len(m.Snapshot().Offers) != 0 {
		t.Error("offer still open")
	}
	for _, call := range fm.calls {
		if strings.HasPrefix(call, "pkexec") {
			t.Fatal("downloaded after Not now")
		}
	}
	// Downloads turned off by the administrator: the offer says so and
	// Download is refused.
	_ = os.WriteFile(models.PolicyPath, []byte("downloads = nobody\n"), 0o644)
	_ = c.VoicePress(context.Background(), false)
	st = c.VoiceStatus()
	if st.Offer == nil || st.Offer.Ask != models.AskNone {
		t.Fatalf("offer with downloads off: %+v", st)
	}
	if _, err := c.AcceptOffer(st.Offer.ID); err == nil {
		t.Error("download accepted although downloads are off")
	}
}

// TestLocalModelOffer: a skill without a model offers the assistant's
// local model once; Not now holds for the session; a downloaded model
// is not offered; after the download the local model is used at once.
func TestLocalModelOffer(t *testing.T) {
	c, _, dir := newCore(t)
	m, fm := newFakeModels(t, dir)
	c.Models = m
	c.WireModels()
	ctx := context.Background()
	c.offerLocalModel(ctx, "summarize news.example.org")
	offers := m.Snapshot().Offers
	if len(offers) != 1 || offers[0].Kind != models.LLM || offers[0].Target != "recommended" || offers[0].Bytes != 1834426016 || offers[0].MB != 1835 {
		t.Fatalf("offers: %+v", offers)
	}
	c.offerLocalModel(ctx, "again")
	if len(m.Snapshot().Offers) != 1 {
		t.Error("offered twice")
	}
	c.DismissOffer(offers[0].ID)
	c.offerLocalModel(ctx, "after not now")
	if len(m.Snapshot().Offers) != 0 {
		t.Error("offered again after Not now")
	}
	for _, call := range fm.calls {
		if strings.HasPrefix(call, "pkexec") {
			t.Fatal("downloaded without consent")
		}
	}
	// A model on disk: no offer.
	c.mu.Lock()
	c.llmDeclined = false
	c.mu.Unlock()
	_ = os.WriteFile(filepath.Join(models.LLMModelDir, "qwen3-1.7b-q8_0.gguf"), []byte("x"), 0o644)
	c.offerLocalModel(ctx, "with a model")
	if len(m.Snapshot().Offers) != 0 {
		t.Error("offered although a model is downloaded")
	}

	// After the download the assistant's settings are read again.
	old := AssistantConf
	AssistantConf = filepath.Join(dir, "assistant.conf")
	defer func() { AssistantConf = old }()
	_ = os.WriteFile(AssistantConf, []byte("[translator]\nenabled = yes\n"), 0o644)
	if c.translator() != nil {
		t.Fatal("a model before the download")
	}
	c.downloadDone(models.Job{ID: "llm-recommended", Kind: models.LLM, Target: "recommended", State: models.StateDone})
	if c.translator() == nil || !c.translator().Local() {
		t.Error("the local model is not used after its download")
	}
}
