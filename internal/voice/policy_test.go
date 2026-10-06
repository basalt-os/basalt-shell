package voice

import (
	"os"
	"path/filepath"
	"testing"
)

func TestWhisperLanguage(t *testing.T) {
	cases := []struct {
		lang, model, want, code string
	}{
		{"", "ggml-base.en", "en", ""},
		{"auto", "ggml-base.en-q5_1", "en", ""},
		{"auto", "ggml-small-q5_1", "auto", ""},
		{"en-US", "ggml-base.en", "en", ""},
		{"en", "ggml-small", "en", ""},
		{"pt-BR", "ggml-small-q5_1", "pt", ""},
		{"pt_BR.UTF-8", "/var/lib/basalt-voice/models/ggml-base.bin", "pt", ""},
		{"es", "ggml-large-v3-turbo-q5_0", "es", ""},
		// An English-only model refuses another language.
		{"pt-BR", "ggml-base.en", "", CodeEnglishOnly},
		{"pt-BR", "/var/lib/basalt-voice/models/ggml-small.en-q5_1.bin", "", CodeEnglishOnly},
		{"klingon!", "ggml-small", "", CodeBadLanguage},
	}
	for _, c := range cases {
		got, err := WhisperLanguage(c.lang, c.model)
		if got != c.want || ErrorCode(err) != c.code {
			t.Errorf("WhisperLanguage(%q, %q) = %q, %v; want %q, code %q", c.lang, c.model, got, err, c.want, c.code)
		}
	}
}

func TestEnglishOnly(t *testing.T) {
	for n, want := range map[string]bool{"ggml-base.en": true, "ggml-base.en-q5_1.bin": true, "ggml-small.en": true,
		"ggml-base": false, "ggml-small-q5_1": false, "ggml-large-v3-turbo-q5_0": false} {
		if EnglishOnly(n) != want {
			t.Errorf("EnglishOnly(%q) != %v", n, want)
		}
	}
}

func writeFile(t *testing.T, p string, size int) {
	t.Helper()
	if err := os.WriteFile(p, make([]byte, size), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestPolicy(t *testing.T) {
	dir := t.TempDir()
	for _, n := range []string{"ggml-base.en.bin", "ggml-small-q5_1.bin", "ggml-large-v3-turbo-q5_0.bin", "ggml-silero-v5.1.2.bin",
		"en_US-ljspeech-medium.onnx", "en_US-ljspeech-medium.onnx.json", "pt_BR-orphan-medium.onnx"} {
		writeFile(t, filepath.Join(dir, n), 1<<20)
	}
	writeFile(t, filepath.Join(dir, "ggml-large-v3-turbo-q5_0.bin"), 3<<20)
	env := map[string]string{
		"BASALT_VOICE_STT_MODEL":      filepath.Join(dir, "ggml-base.en.bin"),
		"BASALT_VOICE_TTS_MODEL":      filepath.Join(dir, "en_US-ljspeech-medium.onnx"),
		"BASALT_VOICE_ALLOWED_MODELS": "ggml-small-q5_1 ggml-large-v3-turbo-q5_0",
		"BASALT_VOICE_MAX_MODEL_MB":   "2",
	}
	s := SystemFromEnv(func(k string) string { return env[k] })
	if s.Language != "" {
		t.Errorf("default language %q (empty: the session's)", s.Language)
	}
	// The defaults are always allowed; an allowed model in size; refusals.
	if p, err := s.Find("", "stt"); err != nil || filepath.Base(p) != "ggml-base.en.bin" {
		t.Errorf("default stt: %q %v", p, err)
	}
	if _, err := s.Find("ggml-small-q5_1", "stt"); err != nil {
		t.Errorf("allowed model refused: %v", err)
	}
	if _, err := s.Find("ggml-large-v3-turbo-q5_0", "stt"); ErrorCode(err) != CodeNotAllowed {
		t.Errorf("model over the size limit: %v", err)
	}
	if _, err := s.Find("ggml-tiny", "stt"); ErrorCode(err) != CodeNoModel {
		t.Errorf("missing model: %v", err)
	}
	for _, bad := range []string{"../ggml-small-q5_1", "/etc/passwd", ".hidden"} {
		if _, err := s.Find(bad, "stt"); err == nil {
			t.Errorf("bad name %q accepted", bad)
		}
	}
	m := s.List()
	if len(m.STT) != 3 || len(m.Voices) != 1 {
		t.Fatalf("list: %+v", m)
	}
	for _, mi := range m.STT {
		switch mi.Name {
		case "ggml-base.en":
			if mi.Multilingual || !mi.Allowed || !mi.Default || mi.Lang != "en" {
				t.Errorf("base.en: %+v", mi)
			}
		case "ggml-small-q5_1":
			if !mi.Multilingual || !mi.Allowed {
				t.Errorf("small: %+v", mi)
			}
		case "ggml-large-v3-turbo-q5_0":
			if mi.Allowed || mi.Reason == "" {
				t.Errorf("large: %+v", mi)
			}
		}
	}
	if m.Voices[0].Lang != "en-US" {
		t.Errorf("voice language %q", m.Voices[0].Lang)
	}
}

func TestReadEnvFile(t *testing.T) {
	p := filepath.Join(t.TempDir(), "voice.conf")
	_ = os.WriteFile(p, []byte("# comment\nBASALT_VOICE_LANGUAGE=\"pt-BR\"\n\nBASALT_VOICE_MAX_MODEL_MB = 600\n"), 0o644)
	s := SystemFromFile(p)
	if s.Language != "pt-BR" || s.MaxModelMB != 600 {
		t.Errorf("%+v", s)
	}
}
