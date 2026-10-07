package keyboard

import (
	"os"
	"regexp"
	"strings"
)

// The system's keyboard, as systemd-localed keeps it (localectl
// set-x11-keymap writes the X11 file, set-keymap the console one). The
// login screen and every session start from it; the shell only reads
// these files.
var (
	X11Conf      = "/etc/X11/xorg.conf.d/00-keyboard.conf"
	VConsoleConf = "/etc/vconsole.conf"
)

var reX11Option = regexp.MustCompile(`^\s*Option\s+"(Xkb[A-Za-z]+)"\s+"([^"]*)"`)

// System is the system's keyboard: the X11 keymap (what graphical
// sessions and the login screen use) and the console keymap.
type System struct {
	XKB     XKB    `json:"xkb"`
	Console string `json:"console"` // the console keymap ("br-abnt2"), "" when unset
	// Set reports a layout configured on this computer (else US).
	Set bool `json:"set"`
}

// ReadSystem reads the system's keyboard the way basalt-session and the
// login screen do: the X11 keymap, else a layout from the console keymap.
func ReadSystem() System {
	var sys System
	vals := map[string]string{}
	if b, err := os.ReadFile(X11Conf); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if m := reX11Option.FindStringSubmatch(line); m != nil {
				if _, seen := vals[m[1]]; !seen {
					vals[m[1]] = m[2]
				}
			}
		}
	}
	if b, err := os.ReadFile(VConsoleConf); err == nil {
		for _, line := range strings.Split(string(b), "\n") {
			if k, v, ok := strings.Cut(strings.TrimSpace(line), "="); ok && k == "KEYMAP" {
				sys.Console = strings.Trim(strings.TrimSpace(v), `"'`)
			}
		}
	}
	layouts := splitList(vals["XkbLayout"])
	variants := splitList(vals["XkbVariant"])
	for i, l := range layouts {
		c := Choice{Layout: l}
		if i < len(variants) {
			c.Variant = variants[i]
		}
		if reLayout.MatchString(c.Layout) && (c.Variant == "" || reVariant.MatchString(c.Variant)) {
			sys.XKB.Layouts = append(sys.XKB.Layouts, c)
		}
	}
	sys.XKB.Model = vals["XkbModel"]
	for _, o := range splitList(vals["XkbOptions"]) {
		if reOption.MatchString(o) {
			sys.XKB.Options = append(sys.XKB.Options, o)
		}
	}
	if len(sys.XKB.Layouts) == 0 && sys.Console != "" {
		// As basalt-session does: a console keymap like "br-abnt2" gives "br".
		l := ConsoleLayout(sys.Console)
		if reLayout.MatchString(l) {
			sys.XKB.Layouts = []Choice{{Layout: l}}
		}
	}
	sys.Set = len(sys.XKB.Layouts) > 0
	if !sys.Set {
		sys.XKB.Layouts = []Choice{{Layout: "us"}}
	}
	return sys
}

// ConsoleLayout is the XKB layout of a console keymap name, as the
// session script maps it.
func ConsoleLayout(km string) string {
	switch km {
	case "uk":
		return "gb"
	case "br-abnt2", "br-abnt":
		return "br"
	}
	l, _, _ := strings.Cut(km, "-")
	return l
}

// splitList splits an XKB list, keeping empty positions ("abnt2,," has
// three variants). An empty string is no list.
func splitList(s string) []string {
	if strings.TrimSpace(s) == "" {
		return nil
	}
	p := strings.Split(s, ",")
	for i := range p {
		p[i] = strings.TrimSpace(p[i])
	}
	return p
}

// SameLayouts reports the person's layouts being exactly the system's.
func SameLayouts(a, b []Choice) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}
