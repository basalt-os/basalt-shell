package theme

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
	"sync"
)

// File is a theme on disk: shared tokens plus one color set per mode.
type File struct {
	ID          string         `json:"id"`
	Name        string         `json:"name"`
	Description string         `json:"description,omitempty"`
	Tokens      map[string]any `json:"tokens"`
	Light       map[string]any `json:"light"`
	Dark        map[string]any `json:"dark"`
	// Origin is "system" or "user" (not stored).
	Origin string `json:"-"`
}

// Meta describes a theme for pickers.
type Meta struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description,omitempty"`
	Origin      string `json:"origin"`
	// Swatch colors for a preview: bg, surface, accent, text (per mode).
	Light []string `json:"light"`
	Dark  []string `json:"dark"`
}

// Settings is the user's choice, stored in settings.json.
type Settings struct {
	Theme  string `json:"theme"`
	Mode   string `json:"mode"`   // light, dark
	Motion string `json:"motion"` // auto, full, reduced
	// Overrides of shared tokens.
	Overrides map[string]any `json:"overrides,omitempty"`
	// Color overrides per mode.
	Light map[string]any `json:"light,omitempty"`
	Dark  map[string]any `json:"dark,omitempty"`
}

// Clone deep-copies settings.
func (s Settings) Clone() Settings {
	c := s
	c.Overrides = cloneMap(s.Overrides)
	c.Light = cloneMap(s.Light)
	c.Dark = cloneMap(s.Dark)
	return c
}

func cloneMap(m map[string]any) map[string]any {
	if m == nil {
		return nil
	}
	c := make(map[string]any, len(m))
	for k, v := range m {
		c[k] = v
	}
	return c
}

// Defaults for a fresh user.
func Defaults() Settings {
	return Settings{Theme: "basalt", Mode: "dark", Motion: "auto"}
}

var reID = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,39}$`)

// Validate checks a theme file: known keys, valid values, complete.
func (f *File) Validate() error {
	if !reID.MatchString(f.ID) {
		return fmt.Errorf("theme id %q: lower case letters, digits and dashes", f.ID)
	}
	if strings.TrimSpace(f.Name) == "" {
		return fmt.Errorf("theme %s: no name", f.ID)
	}
	norm := func(m map[string]any, perMode bool) error {
		for k, v := range m {
			s, ok := Lookup(k)
			if !ok {
				return fmt.Errorf("theme %s: unknown token %q", f.ID, k)
			}
			if s.PerMode != perMode {
				if perMode {
					return fmt.Errorf("theme %s: %s is not a per-mode token", f.ID, k)
				}
				return fmt.Errorf("theme %s: %s belongs in light/dark", f.ID, k)
			}
			n, err := Normalize(k, v)
			if err != nil {
				return fmt.Errorf("theme %s: %v", f.ID, err)
			}
			m[k] = n
		}
		return nil
	}
	if err := norm(f.Tokens, false); err != nil {
		return err
	}
	if err := norm(f.Light, true); err != nil {
		return err
	}
	if err := norm(f.Dark, true); err != nil {
		return err
	}
	for _, s := range Specs {
		if s.PerMode {
			if _, ok := f.Light[s.Key]; !ok {
				return fmt.Errorf("theme %s: light is missing %s", f.ID, s.Key)
			}
			if _, ok := f.Dark[s.Key]; !ok {
				return fmt.Errorf("theme %s: dark is missing %s", f.ID, s.Key)
			}
		} else if _, ok := f.Tokens[s.Key]; !ok {
			if s.Default == nil {
				return fmt.Errorf("theme %s: missing %s", f.ID, s.Key)
			}
			if f.Tokens == nil {
				f.Tokens = map[string]any{}
			}
			f.Tokens[s.Key] = s.Default
		}
	}
	return nil
}

// ValidateSettings normalizes the user's settings against a theme set.
func ValidateSettings(s *Settings, themes map[string]*File) error {
	if _, ok := themes[s.Theme]; !ok {
		return fmt.Errorf("unknown theme %q", s.Theme)
	}
	if s.Mode != "light" && s.Mode != "dark" {
		return fmt.Errorf("mode must be light or dark, got %q", s.Mode)
	}
	switch s.Motion {
	case "auto", "full", "reduced":
	default:
		return fmt.Errorf("motion must be auto, full or reduced, got %q", s.Motion)
	}
	for _, pair := range []struct {
		m       map[string]any
		perMode bool
	}{{s.Overrides, false}, {s.Light, true}, {s.Dark, true}} {
		for k, v := range pair.m {
			spec, ok := Lookup(k)
			if !ok {
				return fmt.Errorf("unknown token %q", k)
			}
			if spec.PerMode != pair.perMode {
				return fmt.Errorf("token %s is in the wrong section", k)
			}
			n, err := Normalize(k, v)
			if err != nil {
				return err
			}
			pair.m[k] = n
		}
	}
	return nil
}

// Resolve computes the token set for settings. weak says the hardware
// cannot animate smoothly (motion "auto" then means reduced).
func Resolve(s Settings, f *File, weak bool) Tokens {
	t := Tokens{}
	for k, v := range f.Tokens {
		t[k] = v
	}
	colors := f.Dark
	over := s.Dark
	if s.Mode == "light" {
		colors, over = f.Light, s.Light
	}
	for k, v := range colors {
		t[k] = v
	}
	for k, v := range s.Overrides {
		t[k] = v
	}
	for k, v := range over {
		t[k] = v
	}
	motion := s.Motion
	if motion == "auto" {
		motion = "full"
		if weak {
			motion = "reduced"
		}
	}
	t["theme"] = f.ID
	t["mode"] = s.Mode
	t["motion"] = motion
	if motion == "reduced" {
		t["motion.fast"], t["motion.normal"], t["motion.slow"] = 0.0, 0.0, 0.0
	}
	return t
}

// Store loads themes and settings and persists changes.
type Store struct {
	SystemDirs []string // read-only theme directories
	UserDir    string   // user themes (writable)
	Path       string   // settings.json

	mu       sync.Mutex
	themes   map[string]*File
	settings Settings
	loadErrs []string
}

// NewStore returns a store with the standard locations.
func NewStore(systemDirs []string, configDir string) *Store {
	return &Store{
		SystemDirs: systemDirs,
		UserDir:    filepath.Join(configDir, "themes"),
		Path:       filepath.Join(configDir, "settings.json"),
	}
}

// Load reads every theme (user themes win over system ones with the same
// id) and the settings. Invalid themes are skipped and reported by
// LoadErrors; invalid settings fall back to the defaults.
func (st *Store) Load() error {
	themes := map[string]*File{}
	var errs []string
	for _, dir := range append(append([]string{}, st.SystemDirs...), st.UserDir) {
		origin := "system"
		if dir == st.UserDir {
			origin = "user"
		}
		entries, _ := os.ReadDir(dir)
		for _, e := range entries {
			if e.IsDir() || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			b, err := os.ReadFile(filepath.Join(dir, e.Name()))
			if err != nil {
				errs = append(errs, err.Error())
				continue
			}
			var f File
			if err := json.Unmarshal(b, &f); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", e.Name(), err))
				continue
			}
			if err := f.Validate(); err != nil {
				errs = append(errs, fmt.Sprintf("%s: %v", e.Name(), err))
				continue
			}
			f.Origin = origin
			themes[f.ID] = &f
		}
	}
	if len(themes) == 0 {
		return errors.New("no valid theme found in " + strings.Join(st.SystemDirs, ", "))
	}
	s := Defaults()
	if b, err := os.ReadFile(st.Path); err == nil {
		var u Settings
		if err := json.Unmarshal(b, &u); err != nil {
			errs = append(errs, fmt.Sprintf("settings: %v", err))
		} else if err := ValidateSettings(&u, themes); err != nil {
			errs = append(errs, fmt.Sprintf("settings: %v", err))
		} else {
			s = u
		}
	}
	if _, ok := themes[s.Theme]; !ok {
		for id := range themes {
			s.Theme = id
			break
		}
	}
	st.mu.Lock()
	st.themes, st.settings, st.loadErrs = themes, s, errs
	st.mu.Unlock()
	return nil
}

// LoadErrors lists problems found by the last Load.
func (st *Store) LoadErrors() []string {
	st.mu.Lock()
	defer st.mu.Unlock()
	return append([]string(nil), st.loadErrs...)
}

// Settings returns a copy of the current settings.
func (st *Store) Settings() Settings {
	st.mu.Lock()
	defer st.mu.Unlock()
	return st.settings.Clone()
}

// Theme returns a theme by id.
func (st *Store) Theme(id string) (*File, bool) {
	st.mu.Lock()
	defer st.mu.Unlock()
	f, ok := st.themes[id]
	return f, ok
}

// Themes lists theme metadata sorted by name.
func (st *Store) Themes() []Meta {
	st.mu.Lock()
	defer st.mu.Unlock()
	out := make([]Meta, 0, len(st.themes))
	for _, f := range st.themes {
		sw := func(m map[string]any) []string {
			var r []string
			for _, k := range []string{"color.bg", "color.surface", "color.accent", "color.text"} {
				s, _ := m[k].(string)
				r = append(r, s)
			}
			return r
		}
		out = append(out, Meta{ID: f.ID, Name: f.Name, Description: f.Description, Origin: f.Origin, Light: sw(f.Light), Dark: sw(f.Dark)})
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Name < out[j].Name })
	return out
}

// Resolve resolves settings (validated against the loaded themes).
func (st *Store) Resolve(s Settings, weak bool) (Tokens, error) {
	st.mu.Lock()
	themes := st.themes
	st.mu.Unlock()
	if err := ValidateSettings(&s, themes); err != nil {
		return nil, err
	}
	return Resolve(s, themes[s.Theme], weak), nil
}

// Check validates candidate settings without saving them.
func (st *Store) Check(s *Settings) error {
	st.mu.Lock()
	themes := st.themes
	st.mu.Unlock()
	return ValidateSettings(s, themes)
}

// Commit validates and saves settings atomically.
func (st *Store) Commit(s Settings) error {
	if err := st.Check(&s); err != nil {
		return err
	}
	b, err := json.MarshalIndent(s, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(filepath.Dir(st.Path), 0o755); err != nil {
		return err
	}
	tmp := st.Path + ".tmp"
	if err := os.WriteFile(tmp, append(b, '\n'), 0o644); err != nil {
		return err
	}
	if err := os.Rename(tmp, st.Path); err != nil {
		return err
	}
	st.mu.Lock()
	st.settings = s.Clone()
	st.mu.Unlock()
	return nil
}

// SaveAs writes the current resolved look (both modes) as a user theme.
func (st *Store) SaveAs(id, name string) (*File, error) {
	if !reID.MatchString(id) {
		return nil, fmt.Errorf("theme id %q: lower case letters, digits and dashes", id)
	}
	st.mu.Lock()
	base, ok := st.themes[st.settings.Theme]
	s := st.settings.Clone()
	st.mu.Unlock()
	if !ok {
		return nil, errors.New("no current theme")
	}
	f := &File{ID: id, Name: name, Description: "Saved from " + base.Name,
		Tokens: map[string]any{}, Light: map[string]any{}, Dark: map[string]any{}}
	for k, v := range base.Tokens {
		f.Tokens[k] = v
	}
	for k, v := range s.Overrides {
		f.Tokens[k] = v
	}
	for k, v := range base.Light {
		f.Light[k] = v
	}
	for k, v := range s.Light {
		f.Light[k] = v
	}
	for k, v := range base.Dark {
		f.Dark[k] = v
	}
	for k, v := range s.Dark {
		f.Dark[k] = v
	}
	if err := f.Validate(); err != nil {
		return nil, err
	}
	b, _ := json.MarshalIndent(f, "", "  ")
	if err := os.MkdirAll(st.UserDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(st.UserDir, id+".json"), append(b, '\n'), 0o644); err != nil {
		return nil, err
	}
	f.Origin = "user"
	st.mu.Lock()
	st.themes[id] = f
	st.mu.Unlock()
	return f, nil
}
