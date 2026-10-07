// Package keyboard is the person's keyboard: layouts and their variants,
// the key that switches between them, Caps Lock and the compose key, key
// repeat. Every layout, variant and option is checked against the XKB
// registry the system ships (xkeyboard-config's evdev.xml), so a value
// that is not a real layout never reaches the compositor or a proposal.
//
// The package is pure (no compositor, no daemon): the shell package
// applies its result through the compositor adapter, and the system
// default (the login screen and new accounts) goes through the system
// assistant's proposal, never from here.
package keyboard

import (
	"encoding/xml"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strings"
)

// RegistryPaths are the XKB rule registries read, in order (the extras
// file holds the less common layouts; both are xkeyboard-config's).
var RegistryPaths = []string{"/usr/share/X11/xkb/rules/evdev.xml", "/usr/share/X11/xkb/rules/evdev.extras.xml"}

// Layout is one XKB layout with its variants.
type Layout struct {
	Name        string    `json:"name"`        // "br"
	Short       string    `json:"short"`       // "pt" (the registry's indicator text)
	Description string    `json:"description"` // "Portuguese (Brazil)"
	Variants    []Variant `json:"variants"`
}

// Variant is one variant of a layout.
type Variant struct {
	Name        string `json:"name"`        // "intl"
	Description string `json:"description"` // "English (US, intl., with dead keys)"
}

// Registry is what the system's XKB data knows.
type Registry struct {
	Layouts []Layout
	index   map[string]int
	options map[string]bool
}

type xmlItem struct {
	Name        string `xml:"name"`
	Short       string `xml:"shortDescription"`
	Description string `xml:"description"`
}

type xmlRegistry struct {
	Layouts []struct {
		Item     xmlItem `xml:"configItem"`
		Variants []struct {
			Item xmlItem `xml:"configItem"`
		} `xml:"variantList>variant"`
	} `xml:"layoutList>layout"`
	Groups []struct {
		Options []struct {
			Item xmlItem `xml:"configItem"`
		} `xml:"option"`
	} `xml:"optionList>group"`
}

// LoadRegistry reads the registries that exist among paths (default:
// RegistryPaths; at least one must exist). A layout listed in two files
// gets the variants of both.
func LoadRegistry(paths ...string) (*Registry, error) {
	if len(paths) == 0 {
		paths = RegistryPaths
	}
	r := &Registry{index: map[string]int{}, options: map[string]bool{}}
	read := 0
	for _, p := range paths {
		b, err := os.ReadFile(p)
		if os.IsNotExist(err) {
			continue
		}
		if err != nil {
			return nil, err
		}
		var x xmlRegistry
		if err := xml.Unmarshal(b, &x); err != nil {
			return nil, fmt.Errorf("%s: %v", filepath.Base(p), err)
		}
		read++
		for _, l := range x.Layouts {
			name := strings.TrimSpace(l.Item.Name)
			if !reLayout.MatchString(name) {
				continue
			}
			i, ok := r.index[name]
			if !ok {
				r.Layouts = append(r.Layouts, Layout{Name: name, Short: strings.TrimSpace(l.Item.Short), Description: strings.TrimSpace(l.Item.Description)})
				i = len(r.Layouts) - 1
				r.index[name] = i
			}
			for _, v := range l.Variants {
				vn := strings.TrimSpace(v.Item.Name)
				if !reVariant.MatchString(vn) || r.Layouts[i].variant(vn) != nil {
					continue
				}
				r.Layouts[i].Variants = append(r.Layouts[i].Variants, Variant{Name: vn, Description: strings.TrimSpace(v.Item.Description)})
			}
		}
		for _, g := range x.Groups {
			for _, o := range g.Options {
				if n := strings.TrimSpace(o.Item.Name); reOption.MatchString(n) {
					r.options[n] = true
				}
			}
		}
	}
	if read == 0 {
		return nil, fmt.Errorf("no XKB registry found (%s)", strings.Join(paths, ", "))
	}
	sort.SliceStable(r.Layouts, func(i, j int) bool { return r.Layouts[i].Name < r.Layouts[j].Name })
	for i := range r.Layouts {
		r.index[r.Layouts[i].Name] = i
	}
	return r, nil
}

func (l *Layout) variant(name string) *Variant {
	for i := range l.Variants {
		if l.Variants[i].Name == name {
			return &l.Variants[i]
		}
	}
	return nil
}

// Layout returns a layout by name, or nil.
func (r *Registry) Layout(name string) *Layout {
	i, ok := r.index[name]
	if !ok {
		return nil
	}
	return &r.Layouts[i]
}

// HasOption reports an XKB option the registry lists.
func (r *Registry) HasOption(name string) bool { return r.options[name] }

// Check refuses a layout or a variant the registry does not list.
func (r *Registry) Check(c Choice) error {
	l := r.Layout(c.Layout)
	if l == nil {
		return fmt.Errorf("%q is not a keyboard layout of this system", c.Layout)
	}
	if c.Variant != "" && l.variant(c.Variant) == nil {
		return fmt.Errorf("%q is not a variant of the keyboard layout %q", c.Variant, c.Layout)
	}
	return nil
}

// Describe is the registry's English name of a layout or a variant.
func (r *Registry) Describe(c Choice) string {
	l := r.Layout(c.Layout)
	if l == nil {
		return c.String()
	}
	if c.Variant == "" {
		return l.Description
	}
	if v := l.variant(c.Variant); v != nil {
		return v.Description
	}
	return c.String()
}

// Entry is one choice of the layout picker: a layout or one of its
// variants, with its name for people.
type Entry struct {
	Layout  string `json:"layout"`
	Variant string `json:"variant"`
	Name    string `json:"name"`    // in the session's language when xkeyboard-config translates it
	English string `json:"english"` // the registry's own name (searched too)
	Short   string `json:"short"`   // the panel's indicator text: "BR", "US"
}

// Entries lists every layout and variant, sorted by name, the names
// translated with tr (nil: English).
func (r *Registry) Entries(tr func(string) string) []Entry {
	if tr == nil {
		tr = func(s string) string { return s }
	}
	out := make([]Entry, 0, len(r.Layouts)*6)
	for _, l := range r.Layouts {
		out = append(out, Entry{Layout: l.Name, Name: tr(l.Description), English: l.Description, Short: Choice{Layout: l.Name}.ShortLabel()})
		for _, v := range l.Variants {
			c := Choice{Layout: l.Name, Variant: v.Name}
			out = append(out, Entry{Layout: l.Name, Variant: v.Name, Name: tr(v.Description), English: v.Description, Short: c.ShortLabel()})
		}
	}
	sort.SliceStable(out, func(i, j int) bool { return strings.ToLower(out[i].Name) < strings.ToLower(out[j].Name) })
	return out
}
