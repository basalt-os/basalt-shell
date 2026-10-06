package intent

import (
	"testing"

	"github.com/basalt-os/basalt-shell/internal/theme"
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
		// Brazilian Portuguese, as speech recognition writes it.
		{"Deixe mais escuro.", "theme.set_tokens"},
		{"deixe a tela mais escura", "theme.set_tokens"},
		{"Use o tema lichen.", "theme.switch"},
		{"use o tema Líquen", "theme.switch"},
		{"Use o tema, Lichen.", "theme.switch"},
		{"mude para o tema lichen", "theme.switch"},
		{"ative o modo escuro", "theme.switch"},
		{"modo noturno", "theme.switch"},
		{"Organize as janelas.", "windows.arrange"},
		{"arrume as janelas", "windows.arrange"},
		{"coloque as janelas lado a lado", "windows.arrange"},
		{"aumente o texto", "theme.set_tokens"},
		{"abra as configurações", "settings.open"},
		{"abra o editor de texto", "app.launch"},
		{"stop speaking answers", "voice.answers.set"},
		{"Pare de falar as respostas.", "voice.answers.set"},
		{"speak answers", "voice.answers.set"},
		{"Fale as respostas.", "voice.answers.set"},
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

func TestRulesSpokenAnswers(t *testing.T) {
	ctx := Context{Tokens: tokens()}
	for in, want := range map[string]string{
		"stop speaking answers":                  "off",
		"Stop speaking the answers.":             "off",
		"please stop reading my answers aloud":   "off",
		"don’t speak the answers":                "off",
		"turn off spoken answers":                "off",
		"pare de falar as respostas":             "off",
		"Por favor, pare de falar as respostas.": "off",
		"não fale as respostas":                  "off",
		"desligue as respostas faladas":          "off",
		"speak answers":                          "on",
		"read the answers aloud":                 "on",
		"start speaking answers again":           "on",
		"turn on spoken answers":                 "on",
		"fale as respostas":                      "on",
		"volte a falar as respostas":             "on",
		"ligue as respostas faladas":             "on",
		"fale as respostas de novo":              "on",
		// Not about the setting.
		"what did the answers say": "",
		"fale com a ana":           "",
	} {
		r := Rules(in, ctx)
		got := ""
		for _, k := range r.Calls {
			if k.Action == "voice.answers.set" {
				got = k.Args["spoken"].(string)
			}
		}
		if got != want {
			t.Errorf("%q: spoken %q, want %q (calls %+v)", in, got, want, r.Calls)
		}
		if want != "" && (len(r.Calls) != 1 || len(r.Unknown) != 0) {
			t.Errorf("%q: want exactly the setting, got %+v unknown %v", in, r.Calls, r.Unknown)
		}
	}
	// The model's intents map to the same phrases.
	for intent, want := range map[string]string{"spoken_answers_off": "off", "spoken_answers_on": "on"} {
		r := Compose([]string{fixed[intent]}, ctx, "model")
		if len(r.Calls) != 1 || r.Calls[0].Action != "voice.answers.set" || r.Calls[0].Args["spoken"] != want {
			t.Errorf("%s: %+v", intent, r.Calls)
		}
	}
}
