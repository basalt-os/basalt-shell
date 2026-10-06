package voiceprefs

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basalt-os/basalt-shell/internal/intent"
)

func TestResolvePrecedence(t *testing.T) {
	cases := []struct {
		name                   string
		p                      Prefs
		system, session        string
		speech, answer, source string
		spoken                 bool
	}{
		// Nothing set: the system's speech language; answers in the
		// session's language (not the system's).
		{"defaults", Prefs{}, "en", "en-US", "en", "en-US", "session", true},
		// The owner's case: an English desktop, voice and answers in pt-BR.
		{"pt-BR on English", Prefs{SpeechLang: "pt-BR"}, "en", "en-US", "pt-BR", "pt-BR", "speech", true},
		// An explicit answer language wins over the speech language.
		{"answers en, speech pt", Prefs{SpeechLang: "pt_BR", AnswerLang: "en-GB"}, "en", "pt-BR", "pt-BR", "en-GB", "settings", true},
		// Speech auto: answers follow the session.
		{"auto", Prefs{SpeechLang: "auto"}, "en", "pt_BR.UTF-8", "auto", "pt-BR", "session", true},
		// The system default is auto; the person's session is Spanish.
		{"system auto", Prefs{Spoken: "no"}, "auto", "es_ES", "auto", "es-ES", "session", false},
		{"no session", Prefs{}, "en", "", "en", "en", "session", true},
	}
	for _, c := range cases {
		e := Resolve(c.p, c.system, c.session)
		if e.SpeechLang != c.speech || e.AnswerLang != c.answer || e.AnswerSource != c.source || e.Spoken != c.spoken || e.Model != "local" {
			t.Errorf("%s: %+v", c.name, e)
		}
	}
}

func TestSaveLoad(t *testing.T) {
	dir := t.TempDir()
	path := Path(dir)
	p := Prefs{SpeechLang: "pt-BR", SpeechModel: "ggml-small-q5_1", AnswerLang: "pt-BR", Spoken: "no",
		Voices: map[string]string{"en": "en_US-ljspeech-medium"}, Model: "local"}
	if err := Save(path, p); err != nil {
		t.Fatal(err)
	}
	st, err := os.Stat(filepath.Dir(path))
	if err != nil || st.Mode().Perm() != 0o700 {
		t.Errorf("directory mode: %v %v", st.Mode(), err)
	}
	got, err := Load(path, dir)
	if err != nil {
		t.Fatal(err)
	}
	if got.SpeechLang != "pt-BR" || got.SpeechModel != "ggml-small-q5_1" || got.Spoken != "no" || got.Voices["en"] != "en_US-ljspeech-medium" || got.AllowRemote {
		t.Errorf("round trip: %+v", got)
	}
	// A missing file is the defaults, not an error.
	if p, err := Load(filepath.Join(dir, "none.conf"), dir); err != nil || p.SpeechLang != "" {
		t.Errorf("missing file: %+v %v", p, err)
	}
	// Bad values are refused before anything is written.
	for _, bad := range []Prefs{{SpeechLang: "português"}, {SpeechModel: "../../etc/passwd"}, {Spoken: "maybe"},
		{Voices: map[string]string{"pt": "a/b"}}, {Model: "x y"}} {
		if err := Save(path, bad); err == nil {
			t.Errorf("saved %+v", bad)
		}
	}
	// A hand-edited file with comments and a home-relative key path.
	_ = os.WriteFile(path, []byte("[speech]\nlanguage = pt-BR # mine\n[model]\nchoice = example\nallow_remote = yes\napi_key_file = ~/.config/basalt/k\n"), 0o644)
	got, _ = Load(path, "/home/ana")
	if got.SpeechLang != "pt-BR" || got.Model != "example" || !got.AllowRemote || got.APIKeyFile != "/home/ana/.config/basalt/k" {
		t.Errorf("hand edited: %+v", got)
	}
}

func TestPolicy(t *testing.T) {
	dir := t.TempDir()
	polPath := filepath.Join(dir, "desktop-models.conf")
	key := filepath.Join(dir, "k")
	_ = os.WriteFile(key, []byte("x"), 0o600)
	local := &intent.Model{Endpoint: "unix:/run/basalt-llm/llm.sock"}

	// No policy file: local only.
	pol := LoadPolicy(polPath)
	if pol.AllowRemote || len(pol.Remotes) != 0 {
		t.Errorf("default policy: %+v", pol)
	}
	m, err := ChooseModel(pol, Prefs{Model: "example", AllowRemote: true}, local)
	if m != local || err == nil {
		t.Errorf("remote without a policy: %v %v", m, err)
	}

	_ = os.WriteFile(polPath, []byte(`[policy]
allow_remote = no
[remote example]
label = Example
endpoint = https://api.example.com/v1
model = big-model
[remote plain]
endpoint = http://api.example.com/v1
model = m
`), 0o644)
	pol = LoadPolicy(polPath)
	if len(pol.Remotes) != 1 || pol.Remotes[0].Name != "example" {
		t.Fatalf("an http endpoint must be dropped: %+v", pol)
	}
	// The administrator does not allow remote models: the person cannot.
	if err := pol.Check(Prefs{Model: "example", AllowRemote: true}); err == nil || !strings.Contains(err.Error(), "administrator") {
		t.Errorf("admin refusal: %v", err)
	}
	pol.AllowRemote = true
	// The person has not opted in.
	if err := pol.Check(Prefs{Model: "example"}); err == nil {
		t.Error("remote without the person's opt-in")
	}
	// A name the administrator does not list.
	if err := pol.Check(Prefs{Model: "other", AllowRemote: true}); err == nil {
		t.Error("unlisted remote accepted")
	}
	// Both agree: the remote model, with the person's key.
	m, err = ChooseModel(pol, Prefs{Model: "example", AllowRemote: true, APIKeyFile: key}, local)
	if err != nil || m == local || m.Endpoint != "https://api.example.com/v1" || m.Model != "big-model" || m.APIKeyFile != key || m.Local() {
		t.Errorf("remote: %+v %v", m, err)
	}
	// A key file others can read is refused.
	_ = os.Chmod(key, 0o644)
	if err := pol.Check(Prefs{Model: "example", AllowRemote: true, APIKeyFile: key}); err == nil {
		t.Error("public key file accepted")
	}
	// Local is always allowed.
	if m, err := ChooseModel(pol, Prefs{Model: "local"}, local); m != local || err != nil {
		t.Error("local")
	}
}
