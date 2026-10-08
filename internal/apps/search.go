package apps

import (
	"sort"
	"strings"
	"unicode"
)

// kinds maps a freedesktop category to the plain words people use for
// that kind of app, in the languages the desktop speaks. A search by
// kind ("text editor", "editor de texto", "terminal") finds the apps of
// that category before apps that only mention the words.
var kinds = map[string][]string{
	"TextEditor":        {"text editor", "editor", "notepad", "editor de texto", "bloco de notas"},
	"TerminalEmulator":  {"terminal", "console", "command line", "terminal emulator", "linha de comando"},
	"WebBrowser":        {"browser", "web browser", "internet", "navegador", "navegador web"},
	"FileManager":       {"file manager", "files", "file browser", "gerenciador de arquivos", "arquivos"},
	"Email":             {"email", "e-mail", "mail", "correio", "correio eletronico"},
	"Calculator":        {"calculator", "calculadora"},
	"Calendar":          {"calendar", "calendario", "agenda"},
	"ContactManagement": {"contacts", "address book", "contatos"},
	"Player":            {"player", "media player", "music player", "video player", "reprodutor"},
	"InstantMessaging":  {"chat", "messenger", "mensagens"},
	"Settings":          {"settings", "preferences", "configuracoes"},
	"WordProcessor":     {"word processor", "documents", "processador de texto"},
	"Spreadsheet":       {"spreadsheet", "planilha"},
	"Presentation":      {"presentation", "slides", "apresentacao"},
	"PasswordManager":   {"password manager", "passwords", "senhas", "gerenciador de senhas"},
	"Viewer":            {"viewer", "visualizador"},
	"PDF":               {"pdf", "pdf viewer", "leitor de pdf"},
}

func kindsOf(categories []string) []string {
	var out []string
	for _, c := range categories {
		out = append(out, kinds[c]...)
	}
	return out
}

// fold lowercases and drops accents, so "configurações" matches
// "configuracoes" and "Text Editor" matches "text editor".
func fold(s string) string {
	return strings.Join(strings.Fields(unaccent.Replace(strings.ToLower(s))), " ")
}

// unaccent drops the accents of the Latin letters the desktop's
// languages use (no dependency on a normalization table).
var unaccent = strings.NewReplacer(
	"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a", "å", "a",
	"é", "e", "è", "e", "ê", "e", "ë", "e",
	"í", "i", "ì", "i", "î", "i", "ï", "i",
	"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
	"ú", "u", "ù", "u", "û", "u", "ü", "u",
	"ç", "c", "ñ", "n", "ý", "y", "ÿ", "y",
)

func words(s string) []string {
	return strings.FieldsFunc(fold(s), func(r rune) bool { return !unicode.IsLetter(r) && !unicode.IsDigit(r) })
}

// Score ranks how well an app answers a query (higher is better, 0 is
// no match). The tiers, best first: the desktop id, the name, the
// generic name ("Text Editor"), the kind of app (its category's words),
// a name that starts with the query, a keyword, every word of the query
// in what describes the app, and last a plain substring. So "text
// editor" finds Mousepad (generic name Text Editor, category TextEditor)
// before a terminal, and "mousepad" finds org.xfce.mousepad.
func Score(a App, query string) int {
	q := fold(strings.TrimSuffix(strings.TrimSpace(query), ".desktop"))
	if q == "" {
		return 0
	}
	id := fold(a.ID)
	names := append([]string{a.Name}, a.Aliases...)
	eq := func(list []string) bool {
		for _, s := range list {
			if s != "" && fold(s) == q {
				return true
			}
		}
		return false
	}
	switch {
	case id == q || lastSegment(id) == q:
		return 1000
	case eq(names[:1]):
		return 900
	case eq(names):
		return 880
	case eq([]string{a.Generic}):
		return 800
	case eq(a.Kinds):
		return 700
	case strings.HasPrefix(fold(a.Name), q):
		return 600
	case strings.HasPrefix(fold(a.Generic), q):
		return 560
	case eq(a.Keywords):
		return 500
	}
	// Every word of the query starts a word of the name, generic name,
	// kinds or keywords (the comment counts less).
	strong := words(strings.Join(append(append(append([]string{a.Name, a.Generic}, a.Aliases...), a.Kinds...), a.Keywords...), " "))
	weak := words(a.Comment)
	qw := words(q)
	if len(qw) > 0 {
		inStrong, inAny := true, true
		for _, w := range qw {
			s, k := hasPrefixWord(strong, w), hasPrefixWord(weak, w)
			inStrong = inStrong && s
			inAny = inAny && (s || k)
		}
		if inStrong {
			return 400
		}
		if inAny {
			return 200
		}
	}
	if strings.Contains(fold(a.Name), q) || strings.Contains(id, q) || strings.Contains(fold(a.Generic), q) {
		return 300
	}
	return 0
}

func lastSegment(id string) string {
	if i := strings.LastIndex(id, "."); i >= 0 {
		return id[i+1:]
	}
	return id
}

func hasPrefixWord(list []string, w string) bool {
	for _, x := range list {
		if strings.HasPrefix(x, w) {
			return true
		}
	}
	return false
}

// Search returns the apps that match query, best first (ties by name).
func Search(list []App, query string) []App {
	type hit struct {
		a App
		s int
	}
	var hits []hit
	for _, a := range list {
		if s := Score(a, query); s > 0 {
			hits = append(hits, hit{a, s})
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		if hits[i].s != hits[j].s {
			return hits[i].s > hits[j].s
		}
		return strings.ToLower(hits[i].a.Name) < strings.ToLower(hits[j].a.Name)
	})
	out := make([]App, len(hits))
	for i, h := range hits {
		out[i] = h.a
	}
	return out
}

// Find resolves a reference (a desktop id, a name, or the kind of app:
// "text editor") to the best matching app.
func Find(list []App, ref string) (App, bool) {
	if r := Search(list, ref); len(r) > 0 {
		return r[0], true
	}
	return App{}, false
}
