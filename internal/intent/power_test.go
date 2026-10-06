package intent

import "testing"

func TestPowerRequests(t *testing.T) {
	want := map[string]string{
		"lock":                             "lock",
		"lock the screen":                  "lock",
		"Bloquear a tela.":                 "lock",
		"bloqueie a tela":                  "lock",
		"log out":                          "logout",
		"Log me out":                       "logout",
		"sign out":                         "logout",
		"sair da sessão":                   "logout",
		"encerre a sessão":                 "logout",
		"suspend":                          "suspend",
		"put the computer to sleep":        "suspend",
		"suspender o computador":           "suspend",
		"restart the computer":             "restart",
		"Restart the computer, please":     "restart",
		"reboot":                           "restart",
		"reiniciar o computador":           "restart",
		"reinicie o computador":            "restart",
		"shut down":                        "poweroff",
		"shutdown the computer":            "poweroff",
		"power off":                        "poweroff",
		"turn off the computer":            "poweroff",
		"desligar":                         "poweroff",
		"desligue o computador":            "poweroff",
		"por favor, desligue o computador": "poweroff",
	}
	for in, op := range want {
		got, ok := PowerRequest(in)
		if !ok || got != op {
			t.Errorf("%q: %q %v, want %q", in, got, ok, op)
		}
		// The rules turn it into the one typed action.
		r := Rules(in, Context{})
		if len(r.Calls) != 1 || r.Calls[0].Action != "session.power" || r.Calls[0].Args["op"] != op || r.System != nil {
			t.Errorf("rules %q: %+v", in, r)
		}
	}
	// Not power: services, apps, spoken answers, the theme.
	for _, in := range []string{"restart nginx", "why did nginx restart", "restart firefox", "desligue as respostas faladas",
		"stop speaking answers", "turn off the lights", "lock the door", "make it darker", "close the window", "desligar o wifi"} {
		if op, ok := PowerRequest(in); ok {
			t.Errorf("%q taken for %s", in, op)
		}
	}
	// The translator's intents reach the same action through the rules.
	for intent, op := range map[string]string{"lock_screen": "lock", "log_out": "logout", "suspend": "suspend", "restart": "restart", "power_off": "poweroff"} {
		r := Compose([]string{fixed[intent]}, Context{}, "model")
		if len(r.Calls) != 1 || r.Calls[0].Action != "session.power" || r.Calls[0].Args["op"] != op {
			t.Errorf("intent %s: %+v", intent, r)
		}
	}
}
