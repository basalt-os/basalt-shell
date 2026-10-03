// Package appearance makes regular applications follow the shell's
// tokens, so GTK, libadwaita, Qt, Electron and Flatpak apps match the
// shell when the theme changes (also when an agent changes it):
//
//   - gsettings org.gnome.desktop.interface: color-scheme and
//     accent-color (read by xdg-desktop-portal-gtk / -gnome and exposed as
//     org.freedesktop.appearance to every app, Flatpak included; libadwaita,
//     Firefox and Electron follow it live), gtk-theme (adw-gtk3 or
//     adw-gtk3-dark for GTK 3), icon and cursor themes, fonts;
//   - ~/.config/gtk-4.0/gtk.css: a managed block of libadwaita CSS
//     variables with the exact palette for both modes (prefers-color-scheme
//     media query, so a later mode switch stays consistent); read when an
//     app starts;
//   - ~/.config/gtk-3.0/gtk.css: the accent only (GTK 3 has no media
//     queries; the adw-gtk3 theme follows the mode);
//   - qt6ct: a generated color scheme and qt6ct.conf (Qt apps started with
//     QT_QPA_PLATFORMTHEME=qt6ct).
package appearance

import (
	"context"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/openbasalt/basalt-shell/internal/theme"
)

// Result lists what was applied and what failed.
type Result struct {
	Applied []string `json:"applied"`
	Errors  []string `json:"errors,omitempty"`
}

// Apply pushes the tokens to the application settings: t is the current
// set, light and dark the same settings resolved in each mode. configDir
// is $XDG_CONFIG_HOME.
func Apply(ctx context.Context, t, lightT, darkT theme.Tokens, configDir string) Result {
	var r Result
	ok := func(s string) { r.Applied = append(r.Applied, s) }
	fail := func(s string, err error) { r.Errors = append(r.Errors, s+": "+err.Error()) }

	dark := t.Str("mode") == "dark"
	scheme := "prefer-light"
	gtk3 := "adw-gtk3"
	if dark {
		scheme, gtk3 = "prefer-dark", "adw-gtk3-dark"
	}
	accent := theme.NearestAccent(t.Str("color.accent"))
	font := fmt.Sprintf("%s %g", t.Str("font.family"), t.Num("font.size")-1)
	mono := fmt.Sprintf("%s %g", t.Str("font.mono"), t.Num("font.size")-1)
	sets := [][2]string{
		{"color-scheme", scheme},
		{"accent-color", accent},
		{"gtk-theme", gtk3},
		{"font-name", font},
		{"document-font-name", font},
		{"monospace-font-name", mono},
	}
	if v := t.Str("apps.iconTheme"); v != "" {
		sets = append(sets, [2]string{"icon-theme", v})
	}
	if v := t.Str("apps.cursorTheme"); v != "" {
		sets = append(sets, [2]string{"cursor-theme", v})
	}
	if v := int(t.Num("apps.cursorSize")); v > 0 {
		sets = append(sets, [2]string{"cursor-size", fmt.Sprint(v)})
	}
	if t.Num("motion.normal") == 0 {
		sets = append(sets, [2]string{"enable-animations", "false"})
	} else {
		sets = append(sets, [2]string{"enable-animations", "true"})
	}
	if _, err := exec.LookPath("gsettings"); err == nil {
		for _, kv := range sets {
			c, cancel := context.WithTimeout(ctx, 3*time.Second)
			out, err := exec.CommandContext(c, "gsettings", "set", "org.gnome.desktop.interface", kv[0], kv[1]).CombinedOutput()
			cancel()
			if err != nil {
				fail("gsettings "+kv[0], fmt.Errorf("%v: %s", err, strings.TrimSpace(string(out))))
			}
		}
		ok("gsettings org.gnome.desktop.interface (color-scheme " + scheme + ", accent-color " + accent + ", gtk-theme " + gtk3 + ")")
	} else {
		fail("gsettings", err)
	}

	palette := t.Str("apps.palette")
	if palette == "" {
		palette = "full"
	}
	gtk4 := GTK4CSS(lightT, darkT, palette)
	p4 := filepath.Join(configDir, "gtk-4.0", "gtk.css")
	if err := writeManaged(p4, gtk4); err != nil {
		fail(p4, err)
	} else {
		ok(p4)
	}
	css3 := GTK3CSS(t, palette)
	p3 := filepath.Join(configDir, "gtk-3.0", "gtk.css")
	if err := writeManaged(p3, css3); err != nil {
		fail(p3, err)
	} else {
		ok(p3)
	}
	if err := writeQt6ct(t, configDir); err != nil {
		fail("qt6ct", err)
	} else {
		ok("qt6ct colors")
	}
	return r
}

const (
	beginMark = "/* BEGIN basalt-shell (managed, rewritten on theme change) */"
	endMark   = "/* END basalt-shell */"
)

// gtk4Vars maps one mode's tokens to libadwaita's CSS variables.
func gtk4Vars(t theme.Tokens, palette string) string {
	var b strings.Builder
	v := func(name, val string) { fmt.Fprintf(&b, "  --%s: %s;\n", name, val) }
	accent := t.Str("color.accent")
	v("accent-bg-color", accent)
	v("accent-fg-color", t.Str("color.accentText"))
	v("accent-color", accent)
	if palette == "full" {
		bg, surface, alt, text := t.Str("color.bg"), t.Str("color.surface"), t.Str("color.surfaceAlt"), t.Str("color.text")
		v("window-bg-color", surface)
		v("window-fg-color", text)
		v("view-bg-color", bg)
		v("view-fg-color", text)
		v("headerbar-bg-color", alt)
		v("headerbar-fg-color", text)
		v("headerbar-backdrop-color", surface)
		v("sidebar-bg-color", alt)
		v("sidebar-fg-color", text)
		v("secondary-sidebar-bg-color", surface)
		v("card-bg-color", alt)
		v("card-fg-color", text)
		v("dialog-bg-color", alt)
		v("dialog-fg-color", text)
		v("popover-bg-color", alt)
		v("popover-fg-color", text)
		v("thumbnail-bg-color", alt)
		v("destructive-bg-color", t.Str("color.danger"))
		v("success-bg-color", t.Str("color.success"))
		v("warning-bg-color", t.Str("color.warning"))
		v("error-bg-color", t.Str("color.danger"))
	}
	return b.String()
}

// GTK4CSS returns the libadwaita palette for both modes (libadwaita 1.6+
// CSS variables, GTK 4.16+ media queries).
func GTK4CSS(light, dark theme.Tokens, palette string) string {
	if palette == "off" {
		return ""
	}
	return ":root {\n" + gtk4Vars(light, palette) + "}\n@media (prefers-color-scheme: dark) {\n  :root {\n" +
		strings.ReplaceAll(gtk4Vars(dark, palette), "  --", "    --") + "  }\n}\n"
}

// GTK3CSS returns the accent for GTK 3 (adw-gtk3 reads libadwaita's
// named colors).
func GTK3CSS(t theme.Tokens, palette string) string {
	if palette == "off" {
		return ""
	}
	return fmt.Sprintf("@define-color accent_bg_color %s;\n@define-color accent_fg_color %s;\n@define-color accent_color %s;\n",
		t.Str("color.accent"), t.Str("color.accentText"), t.Str("color.accent"))
}

// writeManaged replaces the managed block of a file, keeping the rest.
func writeManaged(path, body string) error {
	old, _ := os.ReadFile(path)
	s := string(old)
	if i := strings.Index(s, beginMark); i >= 0 {
		if j := strings.Index(s[i:], endMark); j >= 0 {
			s = s[:i] + s[i+j+len(endMark):]
		}
	}
	s = strings.TrimLeft(s, "\n")
	block := ""
	if body != "" {
		block = beginMark + "\n" + body + endMark + "\n"
	}
	next := block + s
	if next == string(old) {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".basalt-tmp"
	if err := os.WriteFile(tmp, []byte(next), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// writeQt6ct writes a qt6ct color scheme (QPalette roles in qt6ct order)
// and points qt6ct.conf at it, keeping the user's other qt6ct settings.
func writeQt6ct(t theme.Tokens, configDir string) error {
	dir := filepath.Join(configDir, "qt6ct")
	if err := os.MkdirAll(filepath.Join(dir, "colors"), 0o755); err != nil {
		return err
	}
	argb := func(hex string) string {
		c, err := theme.ParseColor(hex)
		if err != nil {
			return "#ff000000"
		}
		return fmt.Sprintf("#%02x%s", int(c.A*255+0.5), strings.TrimPrefix(c.Hex()[:7], "#"))
	}
	bg, surface, alt, text := t.Str("color.bg"), t.Str("color.surface"), t.Str("color.surfaceAlt"), t.Str("color.text")
	muted, accent, accentText, border := t.Str("color.textMuted"), t.Str("color.accent"), t.Str("color.accentText"), t.Str("color.border")
	light := theme.Mix(alt, "#ffffff", 0.15)
	mid := theme.Mix(surface, border, 0.5)
	dark := theme.Mix(surface, "#000000", 0.3)
	// Roles: WindowText, Button, Light, Midlight, Dark, Mid, Text, BrightText,
	// ButtonText, Base, Window, Shadow, Highlight, HighlightedText, Link,
	// LinkVisited, AlternateBase, NoRole, ToolTipBase, ToolTipText,
	// PlaceholderText, Accent.
	active := []string{text, alt, light, alt, dark, mid, text, "#ffffff", text, bg, surface, "#000000",
		accent, accentText, accent, accent, surface, "#000000", alt, text, muted, accent}
	disabled := make([]string, len(active))
	for i, c := range active {
		disabled[i] = c
	}
	for _, i := range []int{0, 6, 8} {
		disabled[i] = muted
	}
	row := func(cs []string) string {
		out := make([]string, len(cs))
		for i, c := range cs {
			out[i] = argb(c)
		}
		return strings.Join(out, ", ")
	}
	scheme := "[ColorScheme]\nactive_colors=" + row(active) + "\ndisabled_colors=" + row(disabled) +
		"\ninactive_colors=" + row(active) + "\n"
	schemePath := filepath.Join(dir, "colors", "basalt.conf")
	if err := os.WriteFile(schemePath, []byte(scheme), 0o644); err != nil {
		return err
	}
	confPath := filepath.Join(dir, "qt6ct.conf")
	conf := map[string]map[string]string{}
	order := []string{}
	if b, err := os.ReadFile(confPath); err == nil {
		sec := ""
		for _, line := range strings.Split(string(b), "\n") {
			l := strings.TrimSpace(line)
			if strings.HasPrefix(l, "[") && strings.HasSuffix(l, "]") {
				sec = l[1 : len(l)-1]
				if _, ok := conf[sec]; !ok {
					conf[sec] = map[string]string{}
					order = append(order, sec)
				}
				continue
			}
			if k, v, ok := strings.Cut(l, "="); ok && sec != "" {
				conf[sec][k] = v
			}
		}
	}
	set := func(sec, k, v string) {
		if _, ok := conf[sec]; !ok {
			conf[sec] = map[string]string{}
			order = append(order, sec)
		}
		conf[sec][k] = v
	}
	set("Appearance", "color_scheme_path", schemePath)
	set("Appearance", "custom_palette", "true")
	if _, ok := conf["Appearance"]["style"]; !ok {
		set("Appearance", "style", "Fusion")
	}
	if v := t.Str("apps.iconTheme"); v != "" {
		set("Appearance", "icon_theme", v)
	}
	size := int(t.Num("font.size") - 1)
	set("Fonts", "general", fmt.Sprintf("\"%s,%d,-1,5,400,0,0,0,0,0,0,0,0,0,0,1\"", t.Str("font.family"), size))
	set("Fonts", "fixed", fmt.Sprintf("\"%s,%d,-1,5,400,0,0,0,0,0,0,0,0,0,0,1\"", t.Str("font.mono"), size))
	var b strings.Builder
	for _, sec := range order {
		fmt.Fprintf(&b, "[%s]\n", sec)
		keys := make([]string, 0, len(conf[sec]))
		for k := range conf[sec] {
			keys = append(keys, k)
		}
		sort.Strings(keys)
		for _, k := range keys {
			fmt.Fprintf(&b, "%s=%s\n", k, conf[sec][k])
		}
		b.WriteString("\n")
	}
	return os.WriteFile(confPath, []byte(b.String()), 0o644)
}
