package intent

import (
	"regexp"
	"strings"
)

// Power requests (lock, log out, suspend, restart, power off), in English
// and Brazilian Portuguese. They become the action session.power, which
// is planned only from the person's own words and confirmed in the shell
// like every other change; an agent cannot propose it.

// PowerOps are the session.power operations.
var PowerOps = []string{"lock", "logout", "suspend", "restart", "poweroff"}

var powerRules = []struct {
	op string
	re *regexp.Regexp
}{
	{"lock", regexp.MustCompile(`^(?:` +
		`lock(?:\s+(?:the|my))?(?:\s+(?:screen|computer|session|desktop))?(?:\s+now)?` +
		`|lock\s+screen` +
		`|(?:bloquear|bloqueie|bloqueia|travar|trave|trava)(?:\s+(?:a|o|minha|meu))?(?:\s+(?:tela|computador|sessão|sessao))?(?:\s+agora)?` +
		`)$`)},
	{"logout", regexp.MustCompile(`^(?:` +
		`(?:log|sign)\s*(?:me\s+)?out(?:\s+(?:of\s+)?(?:the|my)\s+session)?(?:\s+now)?` +
		`|end\s+(?:the|my)\s+session` +
		`|(?:sair|saia|sai)\s+da\s+(?:minha\s+)?(?:sessão|sessao)(?:\s+agora)?` +
		`|(?:encerrar|encerre|encerra|terminar|termine)\s+(?:a\s+)?(?:minha\s+)?(?:sessão|sessao)(?:\s+agora)?` +
		`|(?:fazer|faça|faca|faz)\s+logout` +
		`|deslogar|deslogue` +
		`)$`)},
	{"suspend", regexp.MustCompile(`^(?:` +
		`suspend(?:\s+(?:the|my))?(?:\s+(?:computer|system|pc|machine))?(?:\s+now)?` +
		`|(?:put|send)\s+(?:the|my)\s+(?:computer|pc|machine)\s+to\s+sleep` +
		`|(?:suspender|suspenda|suspende)(?:\s+(?:o|meu))?(?:\s+(?:computador|sistema|pc))?(?:\s+agora)?` +
		`|(?:colocar|coloque|coloca|pôr|por|ponha)\s+o\s+computador\s+(?:para|pra)\s+dormir` +
		`)$`)},
	{"restart", regexp.MustCompile(`^(?:` +
		`(?:restart|reboot)(?:\s+(?:the|my))?\s+(?:computer|system|pc|machine)(?:\s+now)?` +
		`|reboot(?:\s+now)?` +
		`|(?:reiniciar|reinicie|reinicia)(?:\s+(?:o|meu))?(?:\s+(?:computador|sistema|pc))?(?:\s+agora)?` +
		`)$`)},
	{"poweroff", regexp.MustCompile(`^(?:` +
		`shut\s*down(?:\s+(?:the|my))?(?:\s+(?:computer|system|pc|machine))?(?:\s+now)?` +
		`|power\s+(?:off|down)(?:\s+(?:the|my))?(?:\s+(?:computer|system|pc|machine))?(?:\s+now)?` +
		`|(?:turn|switch)\s+off\s+(?:the|my)\s+(?:computer|system|pc|machine)(?:\s+now)?` +
		`|(?:desligar|desligue|desliga)(?:\s+(?:o|meu))?(?:\s+(?:computador|sistema|pc))?(?:\s+agora)?` +
		`)$`)},
}

// PowerRequest recognizes a whole power request ("restart the computer",
// "desligar"); ok is false for anything else.
func PowerRequest(text string) (op string, ok bool) {
	t := strings.ToLower(strings.TrimSpace(text))
	t = strings.Trim(t, ".!? ")
	t = strings.Join(strings.Fields(strings.ReplaceAll(t, ",", " ")), " ")
	for _, p := range []string{"please ", "por favor ", "can you ", "could you ", "você pode ", "voce pode ", "pode "} {
		t = strings.TrimPrefix(t, p)
	}
	t = strings.TrimSuffix(strings.TrimSuffix(t, " please"), " por favor")
	for _, r := range powerRules {
		if r.re.MatchString(t) {
			return r.op, true
		}
	}
	return "", false
}

func powerCall(op string) ([]Call, []string) {
	return []Call{{Action: "session.power", Args: map[string]any{"op": op}}}, []string{"session " + op}
}
