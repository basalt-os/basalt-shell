// Package i18n is the translation catalog of the shell's Go side (ADR
// 0014): every string a person reads or hears goes through G (a whole
// sentence with placeholders, never assembled from pieces) or N (with a
// plural form). English is the reference language: the message id is the
// English text. Translations are gettext-style catalogs compiled to JSON,
// /usr/share/basalt-shell/locale/<lang>.json:
//
//	{"I found %d file.": ["Encontrei %d arquivo.", "Encontrei %d arquivos."],
//	 "Please confirm on the screen.": ["Confirme na tela."]}
//
// (one entry per msgid; plural ids carry one string per plural form, in
// the order of the language's rule). The standard library has no gettext;
// the spike keeps this small reader and recommends real .po files and
// gettext tooling (xgettext extraction) for the product.
package i18n

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var (
	mu      sync.RWMutex
	lang    string
	catalog map[string][]string
	// Dir holds the compiled catalogs (variable for tests).
	Dir = "/usr/share/basalt-shell/locale"
)

// Locale returns the language of the session (LC_ALL, LC_MESSAGES, LANG),
// like "pt_BR" or "en_US"; "en" when unset or C/POSIX.
func Locale() string {
	for _, k := range []string{"LC_ALL", "LC_MESSAGES", "LANG"} {
		if v := os.Getenv(k); v != "" {
			v = strings.SplitN(strings.SplitN(v, ".", 2)[0], "@", 2)[0]
			if v == "C" || v == "POSIX" {
				return "en"
			}
			return v
		}
	}
	return "en"
}

// Load reads the catalog of a language ("" = the session's); English or a
// missing catalog means the reference strings.
func Load(l string) {
	if l == "" {
		l = Locale()
	}
	var cat map[string][]string
	for _, name := range []string{l, strings.SplitN(l, "_", 2)[0]} {
		b, err := os.ReadFile(filepath.Join(Dir, name+".json"))
		if err == nil && json.Unmarshal(b, &cat) == nil {
			break
		}
		cat = nil
	}
	mu.Lock()
	lang, catalog = l, cat
	mu.Unlock()
}

// Lang is the loaded language.
func Lang() string {
	mu.RLock()
	defer mu.RUnlock()
	return lang
}

// Translated reports whether a catalog for the session language is loaded
// (false for English, the reference).
func Translated() bool {
	mu.RLock()
	defer mu.RUnlock()
	return catalog != nil
}

// G translates a whole sentence and fills its placeholders (fmt verbs).
func G(msgid string, args ...any) string {
	mu.RLock()
	s, ok := catalog[msgid]
	mu.RUnlock()
	f := msgid
	if ok && len(s) > 0 && s[0] != "" {
		f = s[0]
	}
	if len(args) == 0 {
		return f
	}
	return fmt.Sprintf(f, args...)
}

// N translates a sentence with a count: singular and plural English ids,
// the count chooses the form (English and Portuguese rule: one, other;
// catalogs for languages with more forms list them in their rule's order).
func N(singular, plural string, n int, args ...any) string {
	mu.RLock()
	s, ok := catalog[singular]
	mu.RUnlock()
	f := plural
	if n == 1 {
		f = singular
	}
	if ok && len(s) > 0 {
		idx := 1
		if n == 1 {
			idx = 0
		}
		if idx < len(s) && s[idx] != "" {
			f = s[idx]
		}
	}
	return fmt.Sprintf(f, args...)
}

func init() { Load("") }
