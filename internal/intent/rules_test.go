package intent

import (
	"testing"

	"github.com/openbasalt/basalt-shell/internal/theme"
)

func tokens() theme.Tokens {
	return theme.Tokens{"mode": "dark", "color.bg": "#111418", "color.surface": "#181c21", "color.surfaceAlt": "#22272e",
		"color.border": "#343b44", "radius.sm": 6.0, "radius.md": 10.0, "radius.lg": 16.0, "radius.window": 10.0,
		"font.size": 11.0, "spacing.unit": 4.0, "window.gaps": 8.0}
}

func TestRules(t *testing.T) {
	ctx := Context{Tokens: tokens(), Themes: []theme.Meta{{ID: "lichen", Name: "Lichen"}}}
	cases := []struct {
		in, action string
	}{
		{"make it darker with rounder corners", "theme.set_tokens"},
		{"deixa mais escuro com cantos arredondados", "theme.set_tokens"},
		{"light mode", "theme.switch"},
		{"use the lichen theme", "theme.switch"},
		{"open text editor", "app.launch"},
		{"abre o firefox", "app.launch"},
		{"arrange windows side by side", "windows.arrange"},
		{"workspace 2", "workspace.switch"},
		{"close this window", "window.close"},
		{"minimize firefox", "window.set_state"},
		{"maximiza o terminal", "window.set_state"},
		{"snap firefox to the left", "window.set_state"},
		{"restore text editor", "window.set_state"},
		{"move firefox to workspace 3", "window.to_workspace"},
		{"reduce motion", "motion.set"},
		{"accent green", "theme.set_tokens"},
		{"open settings", "settings.open"},
	}
	for _, c := range cases {
		r := Rules(c.in, ctx)
		if len(r.Calls) == 0 {
			t.Errorf("%q: no calls (unknown %v)", c.in, r.Unknown)
			continue
		}
		found := false
		for _, k := range r.Calls {
			if k.Action == c.action {
				found = true
			}
		}
		if !found {
			t.Errorf("%q: want %s, got %+v", c.in, c.action, r.Calls)
		}
	}
	r := Rules("make it darker with rounder corners", ctx)
	if len(r.Calls) != 1 {
		t.Fatalf("want one composed call, got %+v", r.Calls)
	}
	tok := r.Calls[0].Args["tokens"].(map[string]any)
	if tok["radius.md"].(float64) != 19 || tok["color.bg"] == "#111418" {
		t.Errorf("composition: %+v", tok)
	}
	for _, q := range []string{"why nginx", "por que o nginx caiu", "disk space", "selinux denials"} {
		if r := Rules(q, ctx); len(r.System) == 0 {
			t.Errorf("%q: not routed to the assistant", q)
		}
	}
}
