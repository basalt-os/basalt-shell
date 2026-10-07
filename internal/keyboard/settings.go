package keyboard

import (
	"bufio"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
)

// FileName is the person's keyboard settings file in $XDG_CONFIG_HOME/basalt
// (~/.config/basalt/keyboard.conf), next to voice-and-assistant.conf.
const FileName = "keyboard.conf"

// MaxLayouts is XKB's limit of layouts (groups) at once.
const MaxLayouts = 4

var (
	reLayout  = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}$`)
	reVariant = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9_.+-]{0,47}$`)
	reOption  = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}:[A-Za-z0-9_+.-]{1,48}$`)
	reChoice  = regexp.MustCompile(`^([a-z][a-z0-9_-]{0,31})(?:\(([A-Za-z0-9][A-Za-z0-9_.+-]{0,47})\))?$`)
)

// Choice is one layout, with a variant or not.
type Choice struct {
	Layout  string `json:"layout"`
	Variant string `json:"variant"`
}

// String is XKB's notation: "br", "us(intl)".
func (c Choice) String() string {
	if c.Variant == "" {
		return c.Layout
	}
	return c.Layout + "(" + c.Variant + ")"
}

// ShortLabel is the panel's indicator text: the layout in capitals.
func (c Choice) ShortLabel() string { return strings.ToUpper(c.Layout) }

// ParseChoice reads "br" or "us(intl)".
func ParseChoice(s string) (Choice, error) {
	s = strings.TrimSpace(s)
	m := reChoice.FindStringSubmatch(s)
	if m == nil {
		return Choice{}, fmt.Errorf("%q is not a keyboard layout (like br or us(intl))", s)
	}
	return Choice{Layout: m[1], Variant: m[2]}, nil
}

// Switch keys: Super+Shift+Space always works (a compositor key binding);
// these add an XKB key combination that switches too, in every app.
const (
	SwitchSuperSpace = "super+shift+space"
	SwitchAltShift   = "alt+shift"
	SwitchCtrlShift  = "ctrl+shift"
	SwitchBothAlts   = "both-alts"
)

// Caps Lock behaviors.
const (
	CapsNormal     = "normal"
	CapsCtrl       = "ctrl"
	CapsEscape     = "escape"
	CapsSwapEscape = "swap-escape"
	CapsOff        = "off"
)

// Compose keys (type accents and symbols in sequence: Compose, ', e).
const (
	ComposeNone  = "none"
	ComposeRAlt  = "right-alt"
	ComposeMenu  = "menu"
	ComposeRCtrl = "right-ctrl"
	ComposeCaps  = "caps-lock"
)

// The XKB option of each choice.
var (
	switchOptions  = map[string]string{SwitchSuperSpace: "", SwitchAltShift: "grp:alt_shift_toggle", SwitchCtrlShift: "grp:ctrl_shift_toggle", SwitchBothAlts: "grp:alts_toggle"}
	capsOptions    = map[string]string{CapsNormal: "", CapsCtrl: "ctrl:nocaps", CapsEscape: "caps:escape", CapsSwapEscape: "caps:swapescape", CapsOff: "caps:none"}
	composeOptions = map[string]string{ComposeNone: "", ComposeRAlt: "compose:ralt", ComposeMenu: "compose:menu", ComposeRCtrl: "compose:rctrl", ComposeCaps: "compose:caps"}
)

// SwitchChoices, CapsChoices and ComposeChoices list the choices, in the
// order the Settings page shows them.
var (
	SwitchChoices  = []string{SwitchSuperSpace, SwitchAltShift, SwitchCtrlShift, SwitchBothAlts}
	CapsChoices    = []string{CapsNormal, CapsCtrl, CapsEscape, CapsSwapEscape, CapsOff}
	ComposeChoices = []string{ComposeNone, ComposeRAlt, ComposeMenu, ComposeRCtrl, ComposeCaps}
)

// Key repeat: the compositors' defaults and the ranges offered.
const (
	DefaultRepeatDelay = 600 // ms before a held key repeats
	DefaultRepeatRate  = 25  // repeats per second
	MinRepeatDelay     = 150
	MaxRepeatDelay     = 1000
	MinRepeatRate      = 10
	MaxRepeatRate      = 80
)

// Settings are the person's keyboard. Empty Layouts follow the system's
// layouts (the login screen's); the other fields still apply on top.
type Settings struct {
	Layouts     []Choice `json:"layouts"`
	Switch      string   `json:"switch"`
	Caps        string   `json:"caps_lock"`
	Compose     string   `json:"compose"`
	RepeatDelay int      `json:"repeat_delay"` // ms; 0: the default
	RepeatRate  int      `json:"repeat_rate"`  // per second; 0: the default
}

// Defaults are the settings of a person who never changed them.
func Defaults() Settings {
	return Settings{Switch: SwitchSuperSpace, Caps: CapsNormal, Compose: ComposeNone}
}

// Normalize fills empty choices with their defaults.
func (s Settings) Normalize() Settings {
	if s.Switch == "" {
		s.Switch = SwitchSuperSpace
	}
	if s.Caps == "" {
		s.Caps = CapsNormal
	}
	if s.Compose == "" {
		s.Compose = ComposeNone
	}
	if s.Layouts == nil {
		s.Layouts = []Choice{}
	}
	return s
}

// IsDefault reports settings that change nothing: the system's keyboard.
func (s Settings) IsDefault() bool {
	n := s.Normalize()
	return len(n.Layouts) == 0 && n.Switch == SwitchSuperSpace && n.Caps == CapsNormal && n.Compose == ComposeNone &&
		n.RepeatDelay == 0 && n.RepeatRate == 0
}

// Validate checks every value; with a registry, the layouts, variants
// and the options they become must be ones the system knows.
func (s Settings) Validate(reg *Registry) error {
	s = s.Normalize()
	if len(s.Layouts) > MaxLayouts {
		return fmt.Errorf("at most %d keyboard layouts can be used at once", MaxLayouts)
	}
	seen := map[Choice]bool{}
	for _, c := range s.Layouts {
		if !reLayout.MatchString(c.Layout) || (c.Variant != "" && !reVariant.MatchString(c.Variant)) {
			return fmt.Errorf("%q is not a keyboard layout", c.String())
		}
		if seen[c] {
			return fmt.Errorf("the keyboard layout %s is listed twice", c.String())
		}
		seen[c] = true
		if reg != nil {
			if err := reg.Check(c); err != nil {
				return err
			}
		}
	}
	if _, ok := switchOptions[s.Switch]; !ok {
		return fmt.Errorf("unknown layout switch key %q", s.Switch)
	}
	if _, ok := capsOptions[s.Caps]; !ok {
		return fmt.Errorf("unknown Caps Lock behavior %q", s.Caps)
	}
	if _, ok := composeOptions[s.Compose]; !ok {
		return fmt.Errorf("unknown compose key %q", s.Compose)
	}
	if s.Compose == ComposeCaps && s.Caps != CapsNormal {
		return errors.New("Caps Lock cannot be the compose key and also change what it does")
	}
	if s.RepeatDelay != 0 && (s.RepeatDelay < MinRepeatDelay || s.RepeatDelay > MaxRepeatDelay) {
		return fmt.Errorf("the repeat delay must be between %d and %d ms", MinRepeatDelay, MaxRepeatDelay)
	}
	if s.RepeatRate != 0 && (s.RepeatRate < MinRepeatRate || s.RepeatRate > MaxRepeatRate) {
		return fmt.Errorf("the repeat rate must be between %d and %d per second", MinRepeatRate, MaxRepeatRate)
	}
	if reg != nil {
		for _, o := range s.ownOptions() {
			if !reg.HasOption(o) {
				return fmt.Errorf("the XKB option %s is not available on this system", o)
			}
		}
	}
	return nil
}

// ownOptions are the XKB options the settings choose.
func (s Settings) ownOptions() []string {
	s = s.Normalize()
	var out []string
	for _, o := range []string{switchOptions[s.Switch], capsOptions[s.Caps], composeOptions[s.Compose]} {
		if o != "" {
			out = append(out, o)
		}
	}
	return out
}

// managedOption reports an option these settings own (switch keys, Caps
// Lock, compose): the system's own ones are replaced, the rest kept.
func managedOption(o string) bool {
	return strings.HasPrefix(o, "grp:") || strings.HasPrefix(o, "caps:") || strings.HasPrefix(o, "compose:") || o == "ctrl:nocaps" ||
		o == "ctrl:swapcaps"
}

// XKB is a full keyboard configuration for a compositor.
type XKB struct {
	Layouts []Choice `json:"layouts"`
	Model   string   `json:"model"`
	Options []string `json:"options"`
}

// Layout is XKB's comma-separated layout list: "br,us".
func (x XKB) Layout() string {
	p := make([]string, len(x.Layouts))
	for i, c := range x.Layouts {
		p[i] = c.Layout
	}
	return strings.Join(p, ",")
}

// Variant is the comma-separated variant list, one per layout: ",intl".
// Empty when no layout has a variant.
func (x XKB) Variant() string {
	p := make([]string, len(x.Layouts))
	has := false
	for i, c := range x.Layouts {
		p[i] = c.Variant
		has = has || c.Variant != ""
	}
	if !has {
		return ""
	}
	return strings.Join(p, ",")
}

// Option is the comma-separated option list.
func (x XKB) Option() string { return strings.Join(x.Options, ",") }

// Effective is the configuration of a session: the person's layouts (or
// the system's), and the options of the system that these settings do not
// own plus the ones they choose.
func (s Settings) Effective(sys XKB) XKB {
	s = s.Normalize()
	out := XKB{Model: sys.Model}
	if len(s.Layouts) > 0 {
		out.Layouts = append(out.Layouts, s.Layouts...)
	} else {
		out.Layouts = append(out.Layouts, sys.Layouts...)
	}
	if len(out.Layouts) == 0 {
		out.Layouts = []Choice{{Layout: "us"}}
	}
	for _, o := range sys.Options {
		if !managedOption(o) {
			out.Options = append(out.Options, o)
		}
	}
	out.Options = append(out.Options, s.ownOptions()...)
	return out
}

// Labels are the indicator texts of a list of layouts; a layout listed
// more than once (us and us(intl)) gets its position: "US1", "US2".
func Labels(cs []Choice) []string {
	count := map[string]int{}
	for _, c := range cs {
		count[c.Layout]++
	}
	out := make([]string, len(cs))
	n := map[string]int{}
	for i, c := range cs {
		out[i] = c.ShortLabel()
		if count[c.Layout] > 1 {
			n[c.Layout]++
			out[i] += strconv.Itoa(n[c.Layout])
		}
	}
	return out
}

// Path is the person's settings file under a configuration directory
// ($XDG_CONFIG_HOME).
func Path(configDir string) string { return filepath.Join(configDir, "basalt", FileName) }

// Load reads the person's settings. A missing file means the defaults;
// values the file cannot hold are reported and left at their default.
func Load(path string) (Settings, []string, error) {
	s := Defaults()
	f, err := os.Open(path)
	if os.IsNotExist(err) {
		return s, nil, nil
	}
	if err != nil {
		return s, nil, err
	}
	defer f.Close()
	var problems []string
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		if i := strings.IndexByte(line, '#'); i >= 0 {
			line = strings.TrimSpace(line[:i])
		}
		if line == "" || strings.HasPrefix(line, "[") {
			continue
		}
		k, v, ok := strings.Cut(line, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "layouts":
			s.Layouts = nil
			for _, part := range strings.Split(v, ",") {
				if strings.TrimSpace(part) == "" {
					continue
				}
				c, err := ParseChoice(part)
				if err != nil {
					problems = append(problems, err.Error())
					continue
				}
				s.Layouts = append(s.Layouts, c)
			}
		case "switch":
			s.Switch = v
		case "caps_lock":
			s.Caps = v
		case "compose":
			s.Compose = v
		case "repeat_delay", "repeat_rate":
			n, err := strconv.Atoi(v)
			if err != nil {
				problems = append(problems, fmt.Sprintf("%s = %q is not a number", k, v))
				continue
			}
			if k == "repeat_delay" {
				s.RepeatDelay = n
			} else {
				s.RepeatRate = n
			}
		}
	}
	return s.Normalize(), problems, sc.Err()
}

// Save writes the settings (mode 0644, through a temporary file).
func Save(path string, s Settings) error {
	s = s.Normalize()
	if err := s.Validate(nil); err != nil {
		return err
	}
	var b strings.Builder
	b.WriteString("# Your keyboard, written by the shell (Settings, Keyboard).\n")
	b.WriteString("# layouts: XKB layouts in order, the first is the default, e.g. br, us(intl);\n")
	b.WriteString("# empty: the system's layouts (the login screen's).\n")
	b.WriteString("[keyboard]\n")
	ls := make([]string, len(s.Layouts))
	for i, c := range s.Layouts {
		ls[i] = c.String()
	}
	fmt.Fprintf(&b, "layouts = %s\n", strings.Join(ls, ", "))
	fmt.Fprintf(&b, "switch = %s\n", s.Switch)
	fmt.Fprintf(&b, "caps_lock = %s\n", s.Caps)
	fmt.Fprintf(&b, "compose = %s\n", s.Compose)
	if s.RepeatDelay != 0 {
		fmt.Fprintf(&b, "repeat_delay = %d\n", s.RepeatDelay)
	}
	if s.RepeatRate != 0 {
		fmt.Fprintf(&b, "repeat_rate = %d\n", s.RepeatRate)
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(b.String()), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}
