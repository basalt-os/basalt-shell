package voice

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/basalt-os/basalt-shell/internal/i18n"
)

// System is the administrator's part of the voice settings: the
// programs, the default models and language, and the policy a person's
// own choices must stay inside. It comes from /etc/basalt/voice.conf
// (KEY=VALUE, also the environment of the voice service). The person's
// own choices (speech language, speech model, voice) live in their
// settings file and reach basalt-voiced with each request; basalt-voiced
// checks them against this policy on every request, so a person can never
// widen it.
//
//	BASALT_VOICE_LANGUAGE        default speech language: auto or a tag; empty (the
//	                             default): each person's session language
//	BASALT_VOICE_ALLOWED_MODELS  speech models and voices a person may choose
//	                             (names, space separated; empty: every installed one)
//	BASALT_VOICE_MAX_MODEL_MB    the largest speech model a person may choose (0: no limit)
//	BASALT_VOICE_MODEL_DIRS      more directories with models (colon separated)
//	BASALT_VOICE_MAX_HOLD        seconds the microphone may stay open (default 30)
type System struct {
	STTModel      string // path of the default speech model
	VADModel      string // path of the voice activity detector (Silero)
	TTSModel      string // path of the default voice
	TTSBin        string
	Language      string
	AllowedModels []string
	MaxModelMB    int
	ModelDirs     []string
	// MaxHold is how long the microphone may stay open (BASALT_VOICE_MAX_HOLD).
	MaxHold time.Duration
}

// Error codes of the voice service, so the shell can say them in the
// person's language.
const (
	CodeEnglishOnly = "english-only-model" // a .en speech model for another language
	CodeNotAllowed  = "model-not-allowed"  // outside the administrator's policy
	CodeNoModel     = "model-missing"      // not installed
	CodeBadLanguage = "bad-language"
)

// CodedError is an error with one of the codes above.
type CodedError struct {
	Code string
	Msg  string
}

func (e *CodedError) Error() string { return e.Msg }

// ErrorCode returns the code of an error from the voice service ("" when
// it has none).
func ErrorCode(err error) string {
	var ce *CodedError
	if errors.As(err, &ce) {
		return ce.Code
	}
	return ""
}

// SystemFromEnv reads the system settings with get (os.Getenv, or a
// parsed voice.conf).
func SystemFromEnv(get func(string) string) System {
	def := func(k, d string) string {
		if v := strings.TrimSpace(get(k)); v != "" {
			return v
		}
		return d
	}
	s := System{
		STTModel: def("BASALT_VOICE_STT_MODEL", "/var/lib/basalt-voice/models/ggml-base.en.bin"),
		VADModel: def("BASALT_VOICE_VAD_MODEL", "/var/lib/basalt-voice/models/ggml-silero-v5.1.2.bin"),
		TTSModel: def("BASALT_VOICE_TTS_MODEL", "/var/lib/basalt-voice/models/en_US-ljspeech-medium.onnx"),
		TTSBin:   def("BASALT_VOICE_TTS_BIN", "/usr/libexec/basalt-voice/piper/piper"),
		// Empty: the speech language follows each person's session.
		Language: strings.TrimSpace(get("BASALT_VOICE_LANGUAGE")),
	}
	s.AllowedModels = strings.Fields(get("BASALT_VOICE_ALLOWED_MODELS"))
	s.MaxModelMB, _ = strconv.Atoi(strings.TrimSpace(get("BASALT_VOICE_MAX_MODEL_MB")))
	s.MaxHold = 30 * time.Second
	if n, err := strconv.Atoi(strings.TrimSpace(get("BASALT_VOICE_MAX_HOLD"))); err == nil && n > 0 {
		s.MaxHold = time.Duration(n) * time.Second
	}
	seen := map[string]bool{}
	for _, d := range append([]string{filepath.Dir(s.STTModel), filepath.Dir(s.TTSModel)}, filepath.SplitList(get("BASALT_VOICE_MODEL_DIRS"))...) {
		if d != "" && d != "." && !seen[d] {
			seen[d] = true
			s.ModelDirs = append(s.ModelDirs, d)
		}
	}
	return s
}

// ReadEnvFile parses a KEY=VALUE file (comments and blank lines skipped,
// optional quotes removed), as systemd's EnvironmentFile does.
func ReadEnvFile(path string) (map[string]string, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	out := map[string]string{}
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || l[0] == '#' || l[0] == ';' {
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		out[strings.TrimSpace(k)] = strings.Trim(strings.TrimSpace(v), `"'`)
	}
	return out, sc.Err()
}

// SystemFromFile reads /etc/basalt/voice.conf (or another file); a
// missing file gives the defaults.
func SystemFromFile(path string) System {
	m, _ := ReadEnvFile(path)
	return SystemFromEnv(func(k string) string { return m[k] })
}

// ModelName is a model's name: its file name without the directory and
// the extension ("ggml-small-q5_1", "en_US-ljspeech-medium").
func ModelName(path string) string {
	b := filepath.Base(path)
	for _, ext := range []string{".bin", ".onnx"} {
		b = strings.TrimSuffix(b, ext)
	}
	return b
}

// EnglishOnly reports whether a Whisper model understands English only
// (the ".en" models: ggml-base.en, ggml-small.en-q5_1).
func EnglishOnly(model string) bool {
	n := ModelName(model)
	return strings.HasSuffix(n, ".en") || strings.Contains(n, ".en-")
}

// WhisperLanguage is the -l argument of whisper-cli for a speech language
// (auto or a tag) and a model: "auto" lets a multilingual model detect
// the language; an English-only model always gets "en" for auto and
// refuses any other language (it would transcribe it as English words).
func WhisperLanguage(lang, model string) (string, error) {
	l := strings.TrimSpace(lang)
	if l == "" || strings.EqualFold(l, "auto") {
		if EnglishOnly(model) {
			return "en", nil
		}
		return "auto", nil
	}
	base := i18n.Base(l)
	if base == "" {
		return "", &CodedError{CodeBadLanguage, fmt.Sprintf("%q is not a language tag", lang)}
	}
	if base != "en" && EnglishOnly(model) {
		return "", &CodedError{CodeEnglishOnly, fmt.Sprintf("the speech model %s understands English only, not %s", ModelName(model), l)}
	}
	return base, nil
}

// VoiceLanguage is the language of a Piper voice from its name
// ("en_US-ljspeech-medium" is en-US); "" when the name has none.
func VoiceLanguage(name string) string {
	return i18n.Tag(strings.SplitN(ModelName(name), "-", 2)[0])
}

// ModelInfo describes an installed model for the settings.
type ModelInfo struct {
	Name         string `json:"name"`
	Kind         string `json:"kind"` // stt (speech to text) or tts (a voice)
	Lang         string `json:"lang,omitempty"`
	Multilingual bool   `json:"multilingual"`
	SizeMB       int    `json:"size_mb"`
	Allowed      bool   `json:"allowed"`
	Reason       string `json:"reason,omitempty"` // why it may not be chosen
	Default      bool   `json:"default,omitempty"`
}

// Models is what the voice service reports to the settings.
type Models struct {
	STT          []ModelInfo `json:"stt"`
	Voices       []ModelInfo `json:"voices"`
	Language     string      `json:"language"` // the system's default speech language
	DefaultSTT   string      `json:"default_stt"`
	DefaultVoice string      `json:"default_voice"`
	TTS          bool        `json:"tts"` // a speech synthesizer is installed
}

// Allowed checks a model against the policy (size in bytes).
func (s System) Allowed(name string, size int64) error {
	if len(s.AllowedModels) > 0 {
		ok := false
		for _, a := range s.AllowedModels {
			if a == name {
				ok = true
			}
		}
		if !ok && name != ModelName(s.STTModel) && name != ModelName(s.TTSModel) {
			return &CodedError{CodeNotAllowed, fmt.Sprintf("the administrator does not allow the model %s", name)}
		}
	}
	if s.MaxModelMB > 0 && size > int64(s.MaxModelMB)<<20 && name != ModelName(s.STTModel) {
		return &CodedError{CodeNotAllowed, fmt.Sprintf("the model %s is larger than the administrator allows (%d MiB)", name, s.MaxModelMB)}
	}
	return nil
}

// Find resolves a model name to its file in the model directories,
// checked against the policy. kind is stt or tts.
func (s System) Find(name, kind string) (string, error) {
	if name == "" {
		if kind == "tts" {
			return s.TTSModel, nil
		}
		return s.STTModel, nil
	}
	if name != filepath.Base(name) || strings.HasPrefix(name, ".") || strings.ContainsAny(name, "/\\") {
		return "", &CodedError{CodeNoModel, "bad model name"}
	}
	ext := ".bin"
	if kind == "tts" {
		ext = ".onnx"
	}
	for _, d := range s.ModelDirs {
		p := filepath.Join(d, name+ext)
		st, err := os.Stat(p)
		if err != nil || !st.Mode().IsRegular() {
			continue
		}
		if err := s.Allowed(name, st.Size()); err != nil {
			return "", err
		}
		return p, nil
	}
	return "", &CodedError{CodeNoModel, fmt.Sprintf("the model %s is not installed", name)}
}

// List reports the installed speech models and voices.
func (s System) List() Models {
	m := Models{Language: s.Language, DefaultSTT: ModelName(s.STTModel), DefaultVoice: ModelName(s.TTSModel)}
	if st, err := os.Stat(s.TTSBin); err == nil && st.Mode().IsRegular() {
		m.TTS = true
	}
	seen := map[string]bool{}
	for _, d := range s.ModelDirs {
		ents, _ := os.ReadDir(d)
		for _, e := range ents {
			n := e.Name()
			info, err := e.Info()
			if err != nil || !info.Mode().IsRegular() {
				continue
			}
			var mi ModelInfo
			switch {
			case strings.HasPrefix(n, "ggml-") && strings.HasSuffix(n, ".bin") && !strings.Contains(n, "silero"):
				mi = ModelInfo{Name: ModelName(n), Kind: "stt", Multilingual: !EnglishOnly(n)}
				if !mi.Multilingual {
					mi.Lang = "en"
				}
			case strings.HasSuffix(n, ".onnx"):
				if _, err := os.Stat(filepath.Join(d, n+".json")); err != nil {
					continue // a voice needs its settings file
				}
				mi = ModelInfo{Name: ModelName(n), Kind: "tts", Lang: VoiceLanguage(n)}
			default:
				continue
			}
			if seen[mi.Kind+mi.Name] {
				continue
			}
			seen[mi.Kind+mi.Name] = true
			mi.SizeMB = int((info.Size() + 1<<19) >> 20)
			mi.Allowed = true
			if err := s.Allowed(mi.Name, info.Size()); err != nil {
				mi.Allowed, mi.Reason = false, err.Error()
			}
			if mi.Kind == "stt" {
				mi.Default = mi.Name == m.DefaultSTT
				m.STT = append(m.STT, mi)
			} else {
				mi.Default = mi.Name == m.DefaultVoice
				m.Voices = append(m.Voices, mi)
			}
		}
	}
	sort.Slice(m.STT, func(i, j int) bool { return m.STT[i].SizeMB < m.STT[j].SizeMB })
	sort.Slice(m.Voices, func(i, j int) bool { return m.Voices[i].Name < m.Voices[j].Name })
	return m
}
