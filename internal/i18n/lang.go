package i18n

import (
	"encoding/json"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"
)

// Language tags. The person's voice and answer languages are BCP 47 tags
// ("pt-BR", "en-US", "es"); catalogs and the session use POSIX locale
// names ("pt_BR"). These helpers convert between them. The voice and the
// assistant's answers have their own language setting, independent of
// the session's locale (an English desktop can be spoken to, and answer,
// in Brazilian Portuguese).

var reTag = regexp.MustCompile(`^([a-z]{2,3})(?:-([A-Z]{2}|[0-9]{3}))?$`)

// Tag normalizes a language tag or a locale name ("pt_BR.UTF-8",
// "pt-br", "PT_BR") to a BCP 47 language tag with an optional region
// ("pt-BR"). It returns "" for anything that is not one (including
// "auto", "C" and "POSIX").
func Tag(s string) string {
	s = strings.TrimSpace(s)
	s = strings.SplitN(strings.SplitN(s, ".", 2)[0], "@", 2)[0]
	s = strings.ReplaceAll(s, "_", "-")
	parts := strings.Split(s, "-")
	if len(parts) == 0 || len(parts) > 2 {
		return ""
	}
	t := strings.ToLower(parts[0])
	if len(parts) == 2 {
		t += "-" + strings.ToUpper(parts[1])
	}
	if !reTag.MatchString(t) {
		return ""
	}
	return t
}

// Base is the language of a tag without its region ("pt" for "pt-BR"),
// the ISO 639 code speech recognition takes.
func Base(tag string) string {
	return strings.SplitN(Tag(tag), "-", 2)[0]
}

// LocaleName is the catalog name of a tag ("pt_BR" for "pt-BR").
func LocaleName(tag string) string {
	return strings.ReplaceAll(Tag(tag), "-", "_")
}

// SessionTag is the session's language as a tag ("en" when unset).
func SessionTag() string {
	if t := Tag(Locale()); t != "" {
		return t
	}
	return "en"
}

// Language describes a language offered in the settings.
type Language struct {
	Tag     string `json:"tag"`
	Native  string `json:"native"`  // its own name, as the person reads it in a list
	English string `json:"english"` // its English name, for the model's instruction
}

// Languages are the languages the settings offer for speech and answers.
// Speech recognition (multilingual Whisper models) knows many more; any
// valid tag in the settings file is accepted.
var Languages = []Language{
	{"en-US", "English (United States)", "English"},
	{"en-GB", "English (United Kingdom)", "British English"},
	{"pt-BR", "Português (Brasil)", "Brazilian Portuguese"},
	{"pt-PT", "Português (Portugal)", "European Portuguese"},
	{"es-ES", "Español (España)", "Spanish"},
	{"es-MX", "Español (México)", "Mexican Spanish"},
	{"fr-FR", "Français", "French"},
	{"de-DE", "Deutsch", "German"},
	{"it-IT", "Italiano", "Italian"},
}

var baseNames = map[string]string{
	"en": "English", "pt": "Portuguese", "es": "Spanish", "fr": "French", "de": "German", "it": "Italian",
	"nl": "Dutch", "pl": "Polish", "ru": "Russian", "uk": "Ukrainian", "ja": "Japanese", "zh": "Chinese",
	"ko": "Korean", "ar": "Arabic", "tr": "Turkish", "sv": "Swedish", "cs": "Czech", "hi": "Hindi",
}

// EnglishName names a tag's language in English, with the tag, for the
// model's instruction: "Brazilian Portuguese (pt-BR)".
func EnglishName(tag string) string {
	t := Tag(tag)
	for _, l := range Languages {
		if l.Tag == t {
			return l.English + " (" + t + ")"
		}
	}
	if n, ok := baseNames[Base(t)]; ok {
		return n + " (" + t + ")"
	}
	return "the language " + t
}

// IsEnglish reports whether a tag is English (any region).
func IsEnglish(tag string) bool { return Base(tag) == "en" }

// Dirs are the directories searched for compiled catalogs:
// BASALT_SHELL_LOCALE_DIR, else Dir plus the ones next to the program (a
// checkout or a ~/.local install).
func Dirs() []string {
	if d := os.Getenv("BASALT_SHELL_LOCALE_DIR"); d != "" {
		return []string{d}
	}
	dirs := []string{Dir}
	if exe, err := os.Executable(); err == nil {
		for _, rel := range []string{"../share/basalt-shell/locale", "../../locale", "../locale"} {
			p := filepath.Clean(filepath.Join(filepath.Dir(exe), rel))
			if st, err := os.Stat(p); err == nil && st.IsDir() {
				dirs = append(dirs, p)
			}
		}
	}
	return dirs
}

// Catalog reads the catalog of a locale name or tag ("pt_BR", "pt-BR";
// falling back to the bare language, "pt"); nil for English or when
// there is none. The shell UI gets the catalog of the session's language
// this way.
func Catalog(l string) map[string][]string {
	name := strings.ReplaceAll(l, "-", "_")
	if t := Tag(name); t != "" {
		name = LocaleName(t)
	}
	if name == "" || strings.HasPrefix(name, "en") {
		return nil
	}
	for _, n := range []string{name, strings.SplitN(name, "_", 2)[0]} {
		for _, d := range Dirs() {
			b, err := os.ReadFile(filepath.Join(d, n+".json"))
			if err != nil {
				continue
			}
			var cat map[string][]string
			if json.Unmarshal(b, &cat) == nil {
				return cat
			}
		}
	}
	return nil
}

// monthNames are the month names as messages (each one translated in the
// catalogs; G needs literal messages).
var monthNames = map[time.Month]func() string{
	time.January: func() string { return G("January") }, time.February: func() string { return G("February") },
	time.March: func() string { return G("March") }, time.April: func() string { return G("April") },
	time.May: func() string { return G("May") }, time.June: func() string { return G("June") },
	time.July: func() string { return G("July") }, time.August: func() string { return G("August") },
	time.September: func() string { return G("September") }, time.October: func() string { return G("October") },
	time.November: func() string { return G("November") }, time.December: func() string { return G("December") },
}

// Month is a month's name in the daemon's language.
func Month(m time.Month) string {
	if f, ok := monthNames[m]; ok {
		return f()
	}
	return m.String()
}

// Date is a day in the daemon's language ("15 September 2026",
// "15 de setembro de 2026").
func Date(t time.Time) string {
	return G("%d %s %d", t.Day(), Month(t.Month()), t.Year())
}
