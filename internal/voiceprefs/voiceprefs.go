// Package voiceprefs holds a person's own voice and assistant settings
// and the administrator's policy around them.
//
// The person's file, $XDG_CONFIG_HOME/basalt/voice-and-assistant.conf
// (~/.config/basalt/voice-and-assistant.conf), overrides the system
// defaults for that person only, whatever the desktop's language is:
//
//	[speech]
//	language = pt-BR            # auto, or a language tag; empty: the system's default
//	model = ggml-base-q5_1      # an installed speech model; empty: the system's default
//
//	[answers]
//	language = pt-BR            # empty: the speech language, then the session's language
//	spoken = yes                # read short answers aloud when a voice for the language exists
//
//	[voices]
//	en = en_US-ljspeech-medium  # the voice per language (installed voices only)
//
//	[model]
//	choice = local              # local, or a remote model the administrator lists
//	allow_remote = no           # the person's own opt-in for a remote model
//	api_key_file = ~/.config/basalt/remote.key   # mode 0600, sent only to that endpoint
//
// Only the shell daemon reads it (the voice service and the skill
// workers get the values from the daemon, and have no access to home
// files); the daemon writes it only when the person changes the settings
// in the shell's Settings window.
//
// The administrator's policy stays in /etc and is enforced by the
// daemons on every use: speech models and voices in /etc/basalt/voice.conf
// (checked by basalt-voiced, see internal/voice.System), language models
// in /etc/basalt/desktop-models.conf (Policy below). A person's choice
// outside the policy is refused when they save it and ignored, with a
// note, when the file was edited by hand.
package voiceprefs

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"github.com/basalt-os/basalt-shell/internal/i18n"
	"github.com/basalt-os/basalt-shell/internal/intent"
)

// FileName is the person's settings file in $XDG_CONFIG_HOME/basalt.
const FileName = "voice-and-assistant.conf"

// PolicyPath is the administrator's language-model policy for the desktop.
var PolicyPath = "/etc/basalt/desktop-models.conf"

// Prefs are the person's settings; empty values mean the default.
type Prefs struct {
	SpeechLang  string            `json:"speech_language"`
	SpeechModel string            `json:"speech_model"`
	AnswerLang  string            `json:"answer_language"`
	Spoken      string            `json:"spoken"` // yes, no, or "" (yes)
	Voices      map[string]string `json:"voices"` // language (en, pt) -> voice name
	Model       string            `json:"model"`  // local, or a remote name from the policy
	AllowRemote bool              `json:"allow_remote"`
	APIKeyFile  string            `json:"api_key_file,omitempty"`
}

// Path is the person's settings file.
func Path(configDir string) string {
	return filepath.Join(configDir, "basalt", FileName)
}

var reName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._-]{0,79}$`)

// Validate checks the shape of every value (not the policy).
func (p Prefs) Validate() error {
	if l := p.SpeechLang; l != "" && !strings.EqualFold(l, "auto") && i18n.Tag(l) == "" {
		return fmt.Errorf("speech language %q is not a language tag", l)
	}
	if l := p.AnswerLang; l != "" && i18n.Tag(l) == "" {
		return fmt.Errorf("answer language %q is not a language tag", l)
	}
	if p.SpeechModel != "" && !reName.MatchString(p.SpeechModel) {
		return fmt.Errorf("bad speech model name %q", p.SpeechModel)
	}
	if p.Model != "" && !reName.MatchString(p.Model) {
		return fmt.Errorf("bad model name %q", p.Model)
	}
	switch p.Spoken {
	case "", "yes", "no":
	default:
		return fmt.Errorf("spoken must be yes or no")
	}
	for l, v := range p.Voices {
		if i18n.Base(l) == "" || !reName.MatchString(v) {
			return fmt.Errorf("bad voice %q for %q", v, l)
		}
	}
	return nil
}

func yes(v string) bool {
	switch strings.ToLower(strings.TrimSpace(v)) {
	case "yes", "true", "on", "1":
		return true
	}
	return false
}

// iniEach calls fn for every key of an INI file (sections lower case).
func iniEach(path string, fn func(sec, k, v string)) error {
	f, err := os.Open(path)
	if err != nil {
		return err
	}
	defer f.Close()
	sec := ""
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		l := strings.TrimSpace(sc.Text())
		if l == "" || l[0] == '#' || l[0] == ';' {
			continue
		}
		if strings.HasPrefix(l, "[") && strings.HasSuffix(l, "]") {
			sec = strings.Join(strings.Fields(strings.ToLower(l[1:len(l)-1])), " ")
			continue
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		v = strings.TrimSpace(v)
		if i := strings.Index(v, " #"); i >= 0 {
			v = strings.TrimSpace(v[:i])
		}
		fn(sec, strings.ToLower(strings.TrimSpace(k)), v)
	}
	return sc.Err()
}

// Load reads the person's file; a missing file gives empty settings
// (every value the default).
func Load(path, home string) (Prefs, error) {
	p := Prefs{Voices: map[string]string{}}
	err := iniEach(path, func(sec, k, v string) {
		switch sec + "." + k {
		case "speech.language":
			p.SpeechLang = v
		case "speech.model":
			p.SpeechModel = v
		case "answers.language":
			p.AnswerLang = v
		case "answers.spoken":
			p.Spoken = map[bool]string{true: "yes", false: "no"}[yes(v)]
			if v == "" {
				p.Spoken = ""
			}
		case "model.choice":
			p.Model = v
		case "model.allow_remote":
			p.AllowRemote = yes(v)
		case "model.api_key_file":
			if strings.HasPrefix(v, "~/") {
				v = filepath.Join(home, v[2:])
			}
			p.APIKeyFile = v
		default:
			if sec == "voices" && v != "" {
				p.Voices[i18n.Base(k)] = v
			}
		}
	})
	if errors.Is(err, os.ErrNotExist) {
		return p, nil
	}
	return p, err
}

// Save writes the person's file (the shell daemon does, when the person
// changes the settings): a commented INI file, replaced atomically.
func Save(path string, p Prefs) error {
	if err := p.Validate(); err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# Your voice and assistant settings (Basalt shell, Settings > Voice and assistant).\n")
	b.WriteString("# They apply to you only, whatever the language of the desktop is. Empty values\n")
	b.WriteString("# use the system's defaults; the administrator's policy always applies.\n\n")
	fmt.Fprintf(&b, "[speech]\n# auto, or a language tag such as pt-BR or en-US\nlanguage = %s\nmodel = %s\n\n", p.SpeechLang, p.SpeechModel)
	fmt.Fprintf(&b, "[answers]\n# empty: the speech language, then the session's language\nlanguage = %s\nspoken = %s\n\n", p.AnswerLang, p.Spoken)
	b.WriteString("[voices]\n")
	langs := make([]string, 0, len(p.Voices))
	for l := range p.Voices {
		langs = append(langs, l)
	}
	sort.Strings(langs)
	for _, l := range langs {
		fmt.Fprintf(&b, "%s = %s\n", l, p.Voices[l])
	}
	ar := "no"
	if p.AllowRemote {
		ar = "yes"
	}
	fmt.Fprintf(&b, "\n[model]\n# local, or a remote model your administrator lists\nchoice = %s\nallow_remote = %s\n", p.Model, ar)
	if p.APIKeyFile != "" {
		fmt.Fprintf(&b, "api_key_file = %s\n", p.APIKeyFile)
	}
	tmp, err := os.CreateTemp(filepath.Dir(path), ".voice-and-assistant-*.conf")
	if err != nil {
		return err
	}
	defer os.Remove(tmp.Name())
	if _, err := tmp.WriteString(b.String()); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmp.Name(), path)
}

// Effective are the settings in force for a person: their file over the
// system defaults.
type Effective struct {
	SpeechLang   string `json:"speech_language"` // auto or a tag
	SpeechModel  string `json:"speech_model"`    // "" = the system's default
	AnswerLang   string `json:"answer_language"` // always a tag
	AnswerSource string `json:"answer_source"`   // settings, speech or session
	Spoken       bool   `json:"spoken"`
	Model        string `json:"model"` // local or a remote name
}

// Resolve applies the precedence: the person's value, else the system's
// default, else (for the speech language, when the system sets none) the
// session's language, else English; the answer language is the person's answer language, else
// their speech language (when it is not auto), else the session's
// language (never the system-wide default locale), else English.
func Resolve(p Prefs, systemLang, sessionTag string) Effective {
	e := Effective{SpeechLang: systemLang, SpeechModel: p.SpeechModel, Spoken: p.Spoken != "no", Model: "local"}
	if p.SpeechLang != "" {
		e.SpeechLang = p.SpeechLang
	} else if strings.TrimSpace(systemLang) == "" {
		// No speech language set by the person or the administrator: the
		// person's session language (a Portuguese desktop is spoken to in
		// Portuguese), else English.
		e.SpeechLang = "en"
		if t := i18n.Tag(sessionTag); t != "" {
			e.SpeechLang = t
		}
	}
	if strings.EqualFold(e.SpeechLang, "auto") || i18n.Tag(e.SpeechLang) == "" {
		e.SpeechLang = "auto"
	} else {
		e.SpeechLang = i18n.Tag(e.SpeechLang)
	}
	switch {
	case i18n.Tag(p.AnswerLang) != "":
		e.AnswerLang, e.AnswerSource = i18n.Tag(p.AnswerLang), "settings"
	case p.SpeechLang != "" && e.SpeechLang != "auto":
		// The person chose a speech language: answers follow it.
		e.AnswerLang, e.AnswerSource = e.SpeechLang, "speech"
	case i18n.Tag(sessionTag) != "":
		e.AnswerLang, e.AnswerSource = i18n.Tag(sessionTag), "session"
	default:
		e.AnswerLang, e.AnswerSource = "en", "session"
	}
	if p.Model != "" {
		e.Model = p.Model
	}
	return e
}

// Remote is a remote language model the administrator lets people choose.
type Remote struct {
	Name     string `json:"name"`
	Label    string `json:"label"`
	Endpoint string `json:"endpoint"`
	Model    string `json:"model"`
}

// Policy is the administrator's language-model policy for the desktop
// (/etc/basalt/desktop-models.conf):
//
//	[policy]
//	allow_remote = no            # yes: people may opt in to a remote model listed below
//
//	[remote example]
//	label = Example provider
//	endpoint = https://api.example.com/v1
//	model = their-model
//
// The local choice is always the command bar's model settings (the
// [translator] section of /etc/basalt/assistant.conf). Nothing here
// widens what the person may do on their own: a remote model needs both
// the administrator's allow_remote and the person's opt-in.
type Policy struct {
	AllowRemote bool     `json:"allow_remote"`
	Remotes     []Remote `json:"remotes"`
}

// LoadPolicy reads the policy; a missing file allows the local model only.
func LoadPolicy(path string) Policy {
	var pol Policy
	idx := map[string]int{}
	_ = iniEach(path, func(sec, k, v string) {
		if sec == "policy" && k == "allow_remote" {
			pol.AllowRemote = yes(v)
			return
		}
		name, ok := strings.CutPrefix(sec, "remote ")
		if !ok || !reName.MatchString(name) || name == "local" {
			return
		}
		i, seen := idx[name]
		if !seen {
			i = len(pol.Remotes)
			idx[name] = i
			pol.Remotes = append(pol.Remotes, Remote{Name: name, Label: name})
		}
		switch k {
		case "label":
			pol.Remotes[i].Label = v
		case "endpoint":
			pol.Remotes[i].Endpoint = v
		case "model":
			pol.Remotes[i].Model = v
		}
	})
	// A remote entry needs an https endpoint and a model name.
	var ok []Remote
	for _, r := range pol.Remotes {
		if strings.HasPrefix(r.Endpoint, "https://") && r.Model != "" {
			ok = append(ok, r)
		}
	}
	pol.Remotes = ok
	return pol
}

// Check refuses a model choice outside the policy, with a message for
// the person.
func (pol Policy) Check(p Prefs) error {
	if p.Model == "" || p.Model == "local" {
		return nil
	}
	var found *Remote
	for i := range pol.Remotes {
		if pol.Remotes[i].Name == p.Model {
			found = &pol.Remotes[i]
		}
	}
	switch {
	case found == nil:
		return errors.New(i18n.G("The model %s is not one your administrator offers.", p.Model))
	case !pol.AllowRemote:
		return errors.New(i18n.G("Your administrator does not allow remote models on this computer."))
	case !p.AllowRemote:
		return errors.New(i18n.G("A remote model needs your own permission: turn on \"Allow a remote model\" first."))
	}
	if p.APIKeyFile != "" {
		st, err := os.Stat(p.APIKeyFile)
		if err != nil || !st.Mode().IsRegular() {
			return errors.New(i18n.G("The key file %s cannot be read.", p.APIKeyFile))
		}
		if st.Mode().Perm()&0o077 != 0 {
			return errors.New(i18n.G("The key file %s must be private (mode 0600).", p.APIKeyFile))
		}
	}
	return nil
}

// ChooseModel returns the language model for a person: local (the system
// translator settings, possibly nil when none is configured) or the
// remote model they chose within the policy. A choice outside the policy
// gives the local model and the reason.
func ChooseModel(pol Policy, p Prefs, local *intent.Model) (*intent.Model, error) {
	if p.Model == "" || p.Model == "local" {
		return local, nil
	}
	if err := pol.Check(p); err != nil {
		return local, err
	}
	for _, r := range pol.Remotes {
		if r.Name == p.Model {
			m := &intent.Model{Endpoint: r.Endpoint, Model: r.Model, AllowRemote: true, APIKeyFile: p.APIKeyFile}
			if local != nil {
				m.Timeout = local.Timeout
			}
			return m, nil
		}
	}
	return local, nil
}
