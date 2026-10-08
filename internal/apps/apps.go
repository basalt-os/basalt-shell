// Package apps reads freedesktop desktop entries (applications that
// should appear in a launcher) from the XDG data directories, Flatpak
// exports included, and turns an entry into a command line.
package apps

import (
	"bufio"
	"errors"
	"os"
	"path/filepath"
	"sort"
	"strings"

	"github.com/basalt-os/basalt-shell/internal/i18n"
)

// App is one launchable application. Name, Generic, Comment and
// Keywords are in the session's language when the entry has them
// (Name[pt_BR], Name[pt]); Aliases keeps the untranslated name, generic
// name and keywords, so "text editor" finds Mousepad in any language.
type App struct {
	ID         string   `json:"id"` // desktop file id, e.g. org.gnome.TextEditor
	Name       string   `json:"name"`
	Generic    string   `json:"generic,omitempty"`
	Comment    string   `json:"comment,omitempty"`
	Icon       string   `json:"icon,omitempty"`
	Keywords   []string `json:"keywords,omitempty"`
	Aliases    []string `json:"aliases,omitempty"`
	Categories []string `json:"categories,omitempty"`
	// Kinds are the plain words of its categories ("text editor",
	// "editor de texto" for TextEditor), for searches by kind of app.
	Kinds    []string `json:"kinds,omitempty"`
	Exec     string   `json:"exec"`
	Terminal bool     `json:"terminal,omitempty"`
	Path     string   `json:"path"`
	Flatpak  bool     `json:"flatpak,omitempty"`
}

// Dirs returns the application directories in priority order.
func Dirs() []string {
	home, _ := os.UserHomeDir()
	dataHome := os.Getenv("XDG_DATA_HOME")
	if dataHome == "" {
		dataHome = filepath.Join(home, ".local", "share")
	}
	dirs := []string{filepath.Join(dataHome, "applications")}
	sys := os.Getenv("XDG_DATA_DIRS")
	if sys == "" {
		sys = "/usr/local/share:/usr/share"
	}
	extra := []string{
		filepath.Join(dataHome, "flatpak", "exports", "share"),
		"/var/lib/flatpak/exports/share",
	}
	seen := map[string]bool{}
	for _, d := range append(strings.Split(sys, ":"), extra...) {
		if d == "" || seen[d] {
			continue
		}
		seen[d] = true
		dirs = append(dirs, filepath.Join(d, "applications"))
	}
	return dirs
}

// List reads all visible applications; the first directory that has a
// given desktop id wins (user entries override system ones).
func List() []App {
	byID := map[string]App{}
	hidden := map[string]bool{}
	for _, dir := range Dirs() {
		_ = filepath.WalkDir(dir, func(path string, d os.DirEntry, err error) error {
			if err != nil || d.IsDir() || !strings.HasSuffix(path, ".desktop") {
				return nil
			}
			rel, _ := filepath.Rel(dir, path)
			id := strings.TrimSuffix(strings.ReplaceAll(rel, "/", "-"), ".desktop")
			if _, ok := byID[id]; ok || hidden[id] {
				return nil
			}
			a, show, err := parse(path)
			if err != nil {
				return nil
			}
			if !show {
				hidden[id] = true
				return nil
			}
			a.ID = id
			byID[id] = a
			return nil
		})
	}
	out := make([]App, 0, len(byID))
	for _, a := range byID {
		out = append(out, a)
	}
	sort.Slice(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}

func currentDesktops() []string {
	return strings.Split(os.Getenv("XDG_CURRENT_DESKTOP"), ":")
}

// parse reads the [Desktop Entry] group.
func parse(path string) (App, bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return App{}, false, err
	}
	defer f.Close()
	a := App{Path: path}
	kv := map[string]string{}
	in := false
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if line == "" || strings.HasPrefix(line, "#") {
			continue
		}
		if strings.HasPrefix(line, "[") {
			in = line == "[Desktop Entry]"
			continue
		}
		if !in {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		kv[strings.TrimSpace(k)] = strings.TrimSpace(v)
	}
	if kv["Type"] != "Application" || kv["Exec"] == "" {
		return a, false, nil
	}
	if kv["NoDisplay"] == "true" || kv["Hidden"] == "true" {
		return a, false, nil
	}
	if only := kv["OnlyShowIn"]; only != "" {
		match := false
		for _, d := range currentDesktops() {
			for _, o := range strings.Split(only, ";") {
				if o != "" && strings.EqualFold(o, d) {
					match = true
				}
			}
		}
		if !match {
			return a, false, nil
		}
	}
	for _, d := range currentDesktops() {
		for _, n := range strings.Split(kv["NotShowIn"], ";") {
			if n != "" && strings.EqualFold(n, d) {
				return a, false, nil
			}
		}
	}
	locs := localeKeys(i18n.Locale())
	a.Name = localized(kv, "Name", locs)
	a.Generic = localized(kv, "GenericName", locs)
	a.Comment = localized(kv, "Comment", locs)
	a.Icon = kv["Icon"]
	a.Exec = kv["Exec"]
	a.Terminal = kv["Terminal"] == "true"
	a.Flatpak = kv["X-Flatpak"] != ""
	a.Keywords = list(localized(kv, "Keywords", locs))
	// The untranslated words still find the app.
	seen := map[string]bool{fold(a.Name): true, fold(a.Generic): true}
	for _, k := range a.Keywords {
		seen[fold(k)] = true
	}
	for _, w := range append([]string{kv["Name"], kv["GenericName"]}, list(kv["Keywords"])...) {
		if w != "" && !seen[fold(w)] {
			seen[fold(w)] = true
			a.Aliases = append(a.Aliases, w)
		}
	}
	if a.Name == "" {
		a.Name = kv["Name"]
	}
	a.Categories = list(kv["Categories"])
	a.Kinds = kindsOf(a.Categories)
	return a, a.Name != "", nil
}

// list splits a desktop entry list value ("a;b;c;").
func list(v string) []string {
	var out []string
	for _, k := range strings.Split(v, ";") {
		if k = strings.TrimSpace(k); k != "" {
			out = append(out, k)
		}
	}
	return out
}

// localeKeys are the locale suffixes a localized key is looked up with,
// best first, per the desktop entry spec: lang_COUNTRY@MODIFIER,
// lang_COUNTRY, lang@MODIFIER, lang. Empty for English or C.
func localeKeys(locale string) []string {
	l := strings.SplitN(locale, ".", 2)[0]
	mod := ""
	if i := strings.Index(locale, "@"); i >= 0 {
		mod = locale[i:]
		l = strings.SplitN(l, "@", 2)[0]
	}
	if l == "" || l == "C" || l == "POSIX" {
		return nil
	}
	lang, country, _ := strings.Cut(l, "_")
	var out []string
	if country != "" && mod != "" {
		out = append(out, lang+"_"+country+mod)
	}
	if country != "" {
		out = append(out, lang+"_"+country)
	}
	if mod != "" {
		out = append(out, lang+mod)
	}
	return append(out, lang)
}

// localized is key's value in the first of locs the entry has, else the
// untranslated one.
func localized(kv map[string]string, key string, locs []string) string {
	for _, l := range locs {
		if v := kv[key+"["+l+"]"]; v != "" {
			return v
		}
	}
	return kv[key]
}

// Argv turns the Exec key into an argument vector: quoting per the
// desktop entry spec, field codes removed (the launcher passes no files).
func (a App) Argv() ([]string, error) {
	args, err := splitExec(a.Exec)
	if err != nil {
		return nil, err
	}
	var out []string
	for _, s := range args {
		switch s {
		case "%f", "%F", "%u", "%U", "%d", "%D", "%n", "%N", "%v", "%m", "%k":
			continue
		case "%i":
			if a.Icon != "" {
				out = append(out, "--icon", a.Icon)
			}
			continue
		case "%c":
			out = append(out, a.Name)
			continue
		}
		s = strings.ReplaceAll(s, "%%", "%")
		out = append(out, s)
	}
	if len(out) == 0 {
		return nil, errors.New("empty Exec")
	}
	if a.Terminal {
		out = append([]string{"foot", "--"}, out...)
	}
	return out, nil
}

func splitExec(s string) ([]string, error) {
	var args []string
	var cur strings.Builder
	inQ, have := false, false
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case inQ && c == '\\' && i+1 < len(s):
			i++
			cur.WriteByte(s[i])
		case c == '"':
			inQ = !inQ
			have = true
		case !inQ && (c == ' ' || c == '\t'):
			if have || cur.Len() > 0 {
				args = append(args, cur.String())
				cur.Reset()
				have = false
			}
		default:
			cur.WriteByte(c)
		}
	}
	if inQ {
		return nil, errors.New("unterminated quote in Exec")
	}
	if have || cur.Len() > 0 {
		args = append(args, cur.String())
	}
	return args, nil
}
