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
)

// App is one launchable application.
type App struct {
	ID       string   `json:"id"` // desktop file id, e.g. org.gnome.TextEditor
	Name     string   `json:"name"`
	Generic  string   `json:"generic,omitempty"`
	Comment  string   `json:"comment,omitempty"`
	Icon     string   `json:"icon,omitempty"`
	Keywords []string `json:"keywords,omitempty"`
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
	a.Name = kv["Name"]
	a.Generic = kv["GenericName"]
	a.Comment = kv["Comment"]
	a.Icon = kv["Icon"]
	a.Exec = kv["Exec"]
	a.Terminal = kv["Terminal"] == "true"
	a.Flatpak = kv["X-Flatpak"] != ""
	for _, k := range strings.Split(kv["Keywords"], ";") {
		if k = strings.TrimSpace(k); k != "" {
			a.Keywords = append(a.Keywords, k)
		}
	}
	return a, a.Name != "", nil
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

// Find resolves a reference (desktop id, or a name, case-insensitive,
// exact first, then prefix, then substring) to an app.
func Find(list []App, ref string) (App, bool) {
	r := strings.ToLower(strings.TrimSpace(ref))
	if r == "" {
		return App{}, false
	}
	for _, a := range list {
		if strings.ToLower(a.ID) == r || strings.ToLower(a.ID) == strings.TrimSuffix(r, ".desktop") {
			return a, true
		}
	}
	for _, a := range list {
		if strings.ToLower(a.Name) == r {
			return a, true
		}
	}
	for _, a := range list {
		if strings.HasPrefix(strings.ToLower(a.Name), r) {
			return a, true
		}
	}
	for _, a := range list {
		if strings.Contains(strings.ToLower(a.Name), r) || strings.Contains(strings.ToLower(a.ID), r) ||
			strings.Contains(strings.ToLower(a.Generic), r) {
			return a, true
		}
		for _, k := range a.Keywords {
			if strings.ToLower(k) == r {
				return a, true
			}
		}
	}
	return App{}, false
}
