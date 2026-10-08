// Package intent turns what a person types in the command bar into typed
// shell actions (or a request for the system assistant). Two backends:
// deterministic rules (always available, English and Portuguese) and an
// optional language model behind an OpenAI-compatible endpoint, whose
// output is constrained by a JSON schema and validated again by the
// shell. Neither backend executes anything: the result becomes a
// proposal the person confirms.
package intent

import (
	"fmt"
	"math"
	"regexp"
	"strconv"
	"strings"

	"github.com/basalt-os/basalt-shell/internal/theme"
)

// Call mirrors shell.Call (kept separate to avoid an import cycle).
type Call struct {
	Action string         `json:"action"`
	Args   map[string]any `json:"args"`
}

// Result of understanding a request.
type Result struct {
	Calls   []Call   `json:"calls,omitempty"`
	System  []string `json:"system,omitempty"` // basalt CLI arguments for a system request
	Explain []string `json:"explain,omitempty"`
	Unknown []string `json:"unknown,omitempty"` // clauses not understood
	Backend string   `json:"backend"`
	// From the language model only:
	AskSystem bool       `json:"ask_system,omitempty"` // a question for the system assistant (basalt ask)
	Clarify   []string   `json:"clarify,omitempty"`    // intents dropped by grounding
	None      bool       `json:"none,omitempty"`       // outside what the desktop does
	Phrases   []string   `json:"phrases,omitempty"`    // canonical phrases run through the rules
	Model     *ModelInfo `json:"model,omitempty"`
}

// Context is what the rules need to compute token changes.
type Context struct {
	Tokens theme.Tokens // resolved, current
	Themes []theme.Meta
	// Lang is the person's answer language (a tag); the model is told to
	// answer in it and to keep every identifier in English.
	Lang string
}

var reThemeComma = regexp.MustCompile(`\b(theme|tema)\s*,\s*`)

var splitRe = regexp.MustCompile(`\s*(?:,|;|\band then\b|\bthen\b|\band\b|\bwith\b|\bplus\b|\be depois\b|\be\b|\bcom\b|\bmas\b)\s*`)

// Rules understands short requests with keyword rules.
func Rules(text string, ctx Context) Result {
	res := Result{Backend: "rules"}
	t := strings.ToLower(strings.TrimSpace(text))
	t = strings.Trim(t, ".!?")
	if t == "" {
		return res
	}
	// Speech recognition writes "use o tema, Lichen": a comma right after
	// "theme" or "tema" is not a new clause.
	t = reThemeComma.ReplaceAllString(t, "$1 ")
	// Lock, log out, suspend, restart, power off: the whole request,
	// before anything else ("restart the computer" is not a service).
	if op, ok := PowerRequest(t); ok {
		res.Calls, res.Explain = powerCall(op)
		return res
	}
	// System requests go to the assistant whole.
	if sys := systemRequest(t); sys != nil {
		res.System = sys
		res.Explain = append(res.Explain, "system request: basalt "+strings.Join(sys, " "))
		return res
	}
	return compose(splitRe.Split(t, -1), ctx, res)
}

// Compose understands a list of clauses, each one a single request in the
// rules' own vocabulary (the translator's canonical phrases use this:
// splitting is not needed and app or window names may contain "and").
func Compose(clauses []string, ctx Context, backend string) Result {
	return compose(clauses, ctx, Result{Backend: backend})
}

func compose(clauses []string, ctx Context, res Result) Result {
	// One working copy of tokens so that clauses compose ("darker with
	// rounder corners" changes colors and radii in one proposal).
	work := ctx.Tokens.Clone()
	tokenSets := map[string]map[string]any{} // mode -> tokens
	setTok := func(mode, k string, v any) {
		if tokenSets[mode] == nil {
			tokenSets[mode] = map[string]any{}
		}
		tokenSets[mode][k] = v
		work[k] = v
	}
	mode := work.Str("mode")
	modeSwitched := ""
	for _, clause := range clauses {
		clause = strings.TrimSpace(stripFiller(strings.ToLower(clause)))
		if clause == "" {
			continue
		}
		calls, explain, ok := rulesClause(clause, ctx, work, &mode, &modeSwitched, setTok)
		if !ok {
			res.Unknown = append(res.Unknown, clause)
			continue
		}
		res.Calls = append(res.Calls, calls...)
		res.Explain = append(res.Explain, explain...)
	}
	if modeSwitched != "" {
		res.Calls = append([]Call{{Action: "theme.switch", Args: map[string]any{"mode": modeSwitched}}}, res.Calls...)
	}
	for _, m := range []string{"light", "dark", "current"} {
		if len(tokenSets[m]) > 0 {
			args := map[string]any{"tokens": tokenSets[m]}
			if m != "current" {
				args["mode"] = m
			}
			res.Calls = append(res.Calls, Call{Action: "theme.set_tokens", Args: args})
		}
	}
	return res
}

var fillers = []string{
	"please", "por favor", "can you", "could you", "pode", "make it", "make the desktop", "make everything",
	"make", "deixa", "deixe", "deixar", "torne", "set the", "i want", "i'd like", "quero", "a bit", "um pouco",
	"slightly", "the desktop", "o desktop", "tudo", "everything", "it",
	// Brazilian Portuguese (spoken requests come with these).
	"você pode", "voce pode", "poderia", "eu quero", "gostaria de", "a tela", "o sistema", "a área de trabalho", "um pouquinho",
}

// themeAliases are the Portuguese names people (and speech recognition)
// give the shipped themes; the identifiers stay the English ones.
var themeAliases = map[string]string{"líquen": "lichen", "liquen": "lichen", "maré": "tide", "mare": "tide", "basalto": "basalt"}

func stripFiller(s string) string {
	s = " " + s + " "
	for _, f := range fillers {
		s = strings.ReplaceAll(s, " "+f+" ", " ")
	}
	return strings.Join(strings.Fields(s), " ")
}

func has(s string, words ...string) bool {
	for _, w := range words {
		if strings.Contains(s, w) {
			return true
		}
	}
	return false
}

var reShow = regexp.MustCompile(`^(?:show|mostra|review)\s+(p-[0-9a-f]{6})$`)

var reUnit = regexp.MustCompile(`^(?:why|por que|porque|por quê|what happened to|o que houve com|diagnose|diagnostica|diagnosticar)\s+(?:is\s+|did\s+|o\s+|a\s+|the\s+)?([a-z0-9@._:-]+)`)

// systemRequest maps assistant questions to basalt CLI arguments.
func systemRequest(t string) []string {
	if m := reShow.FindStringSubmatch(t); m != nil {
		return []string{"show", m[1]}
	}
	if m := reUnit.FindStringSubmatch(t); m != nil {
		unit := m[1]
		if unit != "my" && unit != "the" && unit != "it" {
			return []string{"why", unit}
		}
	}
	switch {
	case has(t, "selinux", "denial", "avc", "negaç"):
		return []string{"fix", "selinux"}
	case has(t, "snapshot"):
		return []string{"snapshots"}
	case has(t, "disk", "disco", "space left", "espaço"):
		return []string{"disk"}
	case has(t, "pending", "proposals", "pendente", "propostas", "waiting for my approval"):
		return []string{"pending"}
	case t == "status" || has(t, "system status", "status do sistema", "how is the system", "como está o sistema", "health"):
		return []string{"status"}
	}
	return nil
}

var colorNames = map[string]string{
	"terra": "#a3472e", "terracotta": "#b5532f", "red": "#c0392b", "vermelho": "#c0392b",
	"orange": "#d9772b", "laranja": "#d9772b", "yellow": "#c99a06", "amarelo": "#c99a06",
	"green": "#3f8f4f", "verde": "#3f8f4f", "teal": "#2f7d78", "blue": "#3b6fd1", "azul": "#3b6fd1",
	"purple": "#7b4fc9", "roxo": "#7b4fc9", "roxa": "#7b4fc9", "pink": "#c4497f", "rosa": "#c4497f", "slate": "#5f6f7f", "cinza": "#5f6f7f",
	"vermelha": "#c0392b", "amarela": "#c99a06",
}

var reHex = regexp.MustCompile(`#[0-9a-f]{6}\b`)
var reNum = regexp.MustCompile(`\b(\d{1,3})\b`)

func scaleColors(work theme.Tokens, factor float64, setTok func(mode, k string, v any)) []string {
	var changed []string
	for _, k := range []string{"color.bg", "color.surface", "color.surfaceAlt", "color.border"} {
		v, err := theme.ScaleLightness(work.Str(k), factor)
		if err == nil {
			setTok("current", k, v)
			changed = append(changed, k)
		}
	}
	return changed
}

func clampSpec(k string, v float64) float64 {
	s, ok := theme.Lookup(k)
	if !ok {
		return v
	}
	return math.Max(s.Min, math.Min(s.Max, v))
}

// Spoken answers off and on, in English and Brazilian Portuguese ("stop
// speaking answers", "pare de falar as respostas", "speak answers", "fale
// as respostas"). Whole clauses only, so a request that merely mentions
// answers is not taken for one.
var (
	reSpokenOff = regexp.MustCompile(`^(?:` +
		`(?:stop|don't|dont|do not|no more)\s+(?:speaking|reading|saying|speak|read|say)\s+(?:the\s+|my\s+)?answers(?:\s+(?:aloud|out loud))?` +
		`|(?:turn off|switch off|disable|mute)\s+(?:the\s+)?(?:spoken|voice)\s+answers` +
		`|(?:spoken|voice)\s+answers\s+off` +
		`|(?:pare|parar|para)\s+de\s+(?:falar|ler|dizer)\s+(?:as\s+|minhas\s+)?respostas(?:\s+em\s+voz\s+alta)?` +
		`|(?:não|nao)\s+(?:fale|fala|leia|diga)\s+(?:as\s+)?respostas(?:\s+em\s+voz\s+alta)?` +
		`|(?:desligue|desliga|desligar|desative|desativa|desativar)\s+(?:as\s+)?respostas\s+(?:faladas|por\s+voz)` +
		`|respostas\s+(?:faladas|por\s+voz)\s+desligadas` +
		`)$`)
	reSpokenOn = regexp.MustCompile(`^(?:` +
		`(?:speak|read|say)\s+(?:the\s+|my\s+)?answers(?:\s+(?:aloud|out loud))?` +
		`|(?:start|resume)\s+(?:speaking|reading)\s+(?:the\s+|my\s+)?answers(?:\s+(?:aloud|out loud))?` +
		`|(?:turn on|switch on|enable|unmute)\s+(?:the\s+)?(?:spoken|voice)\s+answers` +
		`|(?:spoken|voice)\s+answers\s+on` +
		`|(?:fale|fala|falar|leia|diga)\s+(?:as\s+|minhas\s+)?respostas(?:\s+em\s+voz\s+alta)?` +
		`|(?:volte|voltar|volta)\s+a\s+(?:falar|ler)\s+(?:as\s+)?respostas(?:\s+em\s+voz\s+alta)?` +
		`|(?:ligue|liga|ligar|ative|ativa|ativar)\s+(?:as\s+)?respostas\s+(?:faladas|por\s+voz)` +
		`|respostas\s+(?:faladas|por\s+voz)\s+ligadas` +
		`)(?:\s+(?:again|de\s+novo|novamente))?$`)
)

func spokenAnswersClause(c string) ([]Call, []string, bool) {
	c = strings.ReplaceAll(c, "’", "'")
	switch {
	case reSpokenOff.MatchString(c):
		return []Call{{Action: "voice.answers.set", Args: map[string]any{"spoken": "off"}}}, []string{"spoken answers off"}, true
	case reSpokenOn.MatchString(c):
		return []Call{{Action: "voice.answers.set", Args: map[string]any{"spoken": "on"}}}, []string{"spoken answers on"}, true
	}
	return nil, nil, false
}

func rulesClause(c string, ctx Context, work theme.Tokens, mode *string, switched *string,
	setTok func(mode, k string, v any)) ([]Call, []string, bool) {

	// Spoken answers (the person's voice settings).
	if calls, explain, ok := spokenAnswersClause(c); ok {
		return calls, explain, true
	}
	// Power (the translator's canonical phrases come this way).
	if op, ok := PowerRequest(c); ok {
		calls, explain := powerCall(op)
		return calls, explain, true
	}
	// Mode.
	switch {
	case c == "escuro" || c == "escura" || c == "dark":
		*mode, *switched = "dark", "dark"
		return nil, []string{"dark mode"}, true
	case c == "claro" || c == "clara" || c == "light":
		*mode, *switched = "light", "light"
		return nil, []string{"light mode"}, true
	case has(c, "dark mode", "modo escuro", "dark theme", "tema escuro", "night mode", "modo noturno", "modo dark"):
		*mode, *switched = "dark", "dark"
		return nil, []string{"dark mode"}, true
	case has(c, "light mode", "modo claro", "light theme", "tema claro", "day mode", "modo diurno", "modo light"):
		*mode, *switched = "light", "light"
		return nil, []string{"light mode"}, true
	case has(c, "toggle mode", "switch mode", "alternar modo", "inverte"):
		if *mode == "dark" {
			*mode = "light"
		} else {
			*mode = "dark"
		}
		*switched = *mode
		return nil, []string{*mode + " mode"}, true
	}
	// Darker / lighter.
	if has(c, "darker", "mais escuro", "mais escura", "escurec", "escureç", "dimmer", "deeper") {
		if *mode == "light" {
			*mode, *switched = "dark", "dark"
			return nil, []string{"darker: switch to dark mode"}, true
		}
		ch := scaleColors(work, 0.72, setTok)
		return nil, []string{"darker: surfaces 28% darker (" + strings.Join(ch, ", ") + ")"}, true
	}
	if has(c, "lighter", "brighter", "mais claro", "mais clara", "clarear", "clareia", "clareie", "clareá") {
		if *mode == "dark" && has(c, "much", "muito") {
			*mode, *switched = "light", "light"
			return nil, []string{"lighter: switch to light mode"}, true
		}
		ch := scaleColors(work, 1.2, setTok)
		return nil, []string{"lighter: surfaces 20% lighter (" + strings.Join(ch, ", ") + ")"}, true
	}
	// Corners.
	corner := func(f float64, add float64, label string) ([]Call, []string, bool) {
		var parts []string
		for _, k := range []string{"radius.sm", "radius.md", "radius.lg", "radius.xl", "radius.window"} {
			old := work.Num(k)
			nv := math.Round(clampSpec(k, old*f+add))
			setTok("current", k, nv)
			parts = append(parts, fmt.Sprintf("%s %g->%g", k, old, nv))
		}
		return nil, []string{label + ": " + strings.Join(parts, ", ")}, true
	}
	switch {
	case has(c, "square corner", "no corner", "sem cantos", "cantos retos", "sharp corners off", "no rounding"):
		return corner(0, 0, "square corners")
	case has(c, "rounder", "more round", "round corner", "rounded", "arredondad", "mais redond", "softer corners"):
		return corner(1.5, 4, "rounder corners")
	case has(c, "sharper", "less round", "menos arredond", "squarer", "tighter corners"):
		return corner(0.5, 0, "sharper corners")
	}
	// Text size.
	if has(c, "bigger text", "larger text", "bigger font", "larger font", "fonte maior", "texto maior", "letra maior", "letras maiores", "zoom in",
		"aumente o texto", "aumentar o texto", "aumenta o texto", "aumente a fonte", "aumentar a fonte", "aumenta a fonte", "aumente as letras") {
		nv := clampSpec("font.size", work.Num("font.size")+1)
		setTok("current", "font.size", nv)
		return nil, []string{fmt.Sprintf("text size %g", nv)}, true
	}
	if has(c, "smaller text", "smaller font", "fonte menor", "texto menor", "letra menor", "letras menores", "zoom out",
		"diminua o texto", "diminuir o texto", "diminui o texto", "diminua a fonte", "diminuir a fonte", "diminua as letras") {
		nv := clampSpec("font.size", work.Num("font.size")-1)
		setTok("current", "font.size", nv)
		return nil, []string{fmt.Sprintf("text size %g", nv)}, true
	}
	// Spacing.
	if has(c, "roomier", "more space", "more spacing", "mais espaç", "spacious", "bigger gaps") {
		u := clampSpec("spacing.unit", work.Num("spacing.unit")+1)
		g := clampSpec("window.gaps", work.Num("window.gaps")+6)
		setTok("current", "spacing.unit", u)
		setTok("current", "window.gaps", g)
		return nil, []string{fmt.Sprintf("spacing unit %g, gaps %g", u, g)}, true
	}
	if has(c, "compact", "denser", "less space", "menos espaç", "smaller gaps", "no gaps") {
		u := clampSpec("spacing.unit", work.Num("spacing.unit")-1)
		g := clampSpec("window.gaps", work.Num("window.gaps")-6)
		if has(c, "no gaps") {
			g = 0
		}
		setTok("current", "spacing.unit", u)
		setTok("current", "window.gaps", g)
		return nil, []string{fmt.Sprintf("spacing unit %g, gaps %g", u, g)}, true
	}
	// Motion.
	switch {
	case has(c, "reduce motion", "reduced motion", "no animation", "less motion", "disable animation", "sem animaç", "menos animaç", "stop animating",
		"less animation", "animations off", "turn off the animation", "turn off animation", "desliga as animaç", "desligar animaç"):
		return []Call{{Action: "motion.set", Args: map[string]any{"motion": "reduced"}}}, []string{"reduced motion"}, true
	case has(c, "enable animation", "full motion", "more animation", "com animaç", "turn on animation"):
		return []Call{{Action: "motion.set", Args: map[string]any{"motion": "full"}}}, []string{"full motion"}, true
	}
	// Panel position.
	if has(c, "panel") || has(c, "painel") || has(c, "barra") {
		if has(c, "bottom", "embaixo", "baixo") {
			setTok("current", "panel.position", "bottom")
			return nil, []string{"panel at the bottom"}, true
		}
		if has(c, "top", "topo", "cima") {
			setTok("current", "panel.position", "top")
			return nil, []string{"panel at the top"}, true
		}
	}
	// Shadows / blur.
	if has(c, "shadow", "sombra") {
		on := !has(c, "no shadow", "without shadow", "off", "sem sombra", "remove")
		setTok("current", "window.shadows", on)
		return nil, []string{fmt.Sprintf("window shadows %v", on)}, true
	}
	if has(c, "blur", "desfoque") {
		on := !has(c, "no blur", "off", "sem desfoque", "remove")
		setTok("current", "window.blur", on)
		return nil, []string{fmt.Sprintf("blur %v", on)}, true
	}
	// Accent color.
	if has(c, "accent", "destaque", "cor de", "color", "cor ") {
		if hx := reHex.FindString(c); hx != "" {
			setTok("current", "color.accent", hx)
			return nil, []string{"accent " + hx}, true
		}
		for name, hx := range colorNames {
			if regexp.MustCompile(`\b` + name + `\b`).MatchString(c) {
				setTok("current", "color.accent", hx)
				return nil, []string{"accent " + name + " " + hx}, true
			}
		}
	}
	// Theme by name.
	for alias, id := range themeAliases {
		if strings.Contains(c, alias) && !strings.Contains(c, id) {
			c = strings.ReplaceAll(c, alias, id)
		}
	}
	for _, m := range ctx.Themes {
		n := strings.ToLower(m.Name)
		if (has(c, "theme", "tema", "use ", "switch to", "muda para", "mude para", "troque para", "troca para", "usar", "visual", "aparência")) && (strings.Contains(c, n) || strings.Contains(c, m.ID)) {
			return []Call{{Action: "theme.switch", Args: map[string]any{"theme": m.ID}}}, []string{"theme " + m.Name}, true
		}
	}
	if has(c, "reset theme", "default look", "reset the look", "restaurar tema", "padrão", "reset tokens", "undo theme") {
		return []Call{{Action: "theme.reset", Args: map[string]any{}}}, []string{"reset to the theme's values"}, true
	}
	// Windows and apps.
	if m := regexp.MustCompile(`^(?:move|send|mova|manda|envia)\s+(.+?)\s+(?:to|para)\s+(?:workspace|área|area|desktop)\s*(\d+)$`).FindStringSubmatch(c); m != nil {
		return []Call{{Action: "window.to_workspace", Args: map[string]any{"window": windowRef(m[1]), "workspace": m[2]}}}, []string{"send " + m[1] + " to workspace " + m[2]}, true
	}
	if m := regexp.MustCompile(`(?:workspace|área de trabalho|area de trabalho|desktop)\s*(\d+)`).FindStringSubmatch(c); m != nil {
		return []Call{{Action: "workspace.switch", Args: map[string]any{"workspace": m[1]}}}, []string{"workspace " + m[1]}, true
	}
	for _, l := range []struct {
		words  []string
		layout string
	}{
		{[]string{"side by side", "lado a lado", "columns", "colunas"}, "columns"},
		{[]string{"grid", "grade", "tile all", "organize", "organiza", "arrange", "arrume", "arruma", "arrumar", "ajeite", "ajeitar"}, "grid"},
		{[]string{"cascade", "cascata"}, "cascade"},
		{[]string{"center", "centraliz"}, "center"},
		{[]string{"rows", "linhas", "stack"}, "rows"},
		{[]string{"tiling", "tile windows", "tile the windows", "tile all", "tile them", "lado a lado automático"}, "tile"},
		{[]string{"float all", "float everything", "floating windows", "janelas flutuantes"}, "float"},
	} {
		if has(c, l.words...) && !has(c, "corner") {
			return []Call{{Action: "windows.arrange", Args: map[string]any{"layout": l.layout}}}, []string{"arrange: " + l.layout}, true
		}
	}
	if m := regexp.MustCompile(`^snap\s+(.*?)\s*(?:to\s+the\s+)?(left|right)(?:\s+half)?$`).FindStringSubmatch(c); m != nil {
		return []Call{{Action: "window.set_state", Args: map[string]any{"window": windowRef(m[1]), "state": m[2]}}}, []string{"snap " + orFocused(m[1]) + " " + m[2]}, true
	}
	for _, st := range []struct {
		re    string
		state string
	}{
		{`^(?:minimi[sz]e|minimiza|minimizar|minimize|hide)\s*(.*)$`, "minimized"},
		{`^(?:maximi[sz]e|maximiza|maximizar)\s*(.*)$`, "maximized"},
		{`^(?:restore|restaura|restaurar|unminimi[sz]e)\s*(.*)$`, "normal"},
	} {
		if m := regexp.MustCompile(st.re).FindStringSubmatch(c); m != nil {
			return []Call{{Action: "window.set_state", Args: map[string]any{"window": windowRef(m[1]), "state": st.state}}}, []string{st.state + ": " + orFocused(m[1])}, true
		}
	}
	if m := regexp.MustCompile(`^(?:close|fecha|fechar|feche|quit)\s*(.*)$`).FindStringSubmatch(c); m != nil {
		return []Call{{Action: "window.close", Args: map[string]any{"window": windowRef(m[1])}}}, []string{"close " + orFocused(m[1])}, true
	}
	if m := regexp.MustCompile(`^(?:focus|foca|switch to|go to|vai para|mostra|show)\s+(.+)$`).FindStringSubmatch(c); m != nil {
		return []Call{{Action: "window.focus", Args: map[string]any{"window": windowRef(m[1])}}}, []string{"focus " + m[1]}, true
	}
	if has(c, "settings", "configuraç", "preferences", "preferências", "ajustes") {
		page := "appearance"
		for _, p := range []string{"tokens", "motion", "panel", "windows", "apps", "ai", "voice", "about"} {
			if strings.Contains(c, p) {
				page = p
			}
		}
		if has(c, "voz", "fala", "idioma", "language", "speech") {
			page = "voice"
		}
		if has(c, "update", "atualizaç", "channel", "canais", "canal") {
			page = "updates"
		}
		return []Call{{Action: "settings.open", Args: map[string]any{"page": page}}}, []string{"open settings: " + page}, true
	}
	if m := regexp.MustCompile(`^(?:open|launch|start|run|abr[aei]r?|abre|inicia|iniciar|executa)\s+(?:the\s+|o\s+|a\s+)?(.+)$`).FindStringSubmatch(c); m != nil {
		return []Call{{Action: "app.launch", Args: map[string]any{"app": strings.TrimSuffix(m[1], " app")}}}, []string{"launch " + m[1]}, true
	}
	if m := regexp.MustCompile(`^(?:float|flutua)\s*(.*)$`).FindStringSubmatch(c); m != nil {
		return []Call{{Action: "window.set_floating", Args: map[string]any{"window": windowRef(m[1]), "floating": true}}}, []string{"float " + orFocused(m[1])}, true
	}
	if m := regexp.MustCompile(`^(?:tile|encaixa)\s*(.*)$`).FindStringSubmatch(c); m != nil {
		return []Call{{Action: "window.set_floating", Args: map[string]any{"window": windowRef(m[1]), "floating": false}}}, []string{"tile " + orFocused(m[1])}, true
	}
	if n := reNum.FindString(c); n != "" && has(c, "radius", "raio") {
		v, _ := strconv.Atoi(n)
		setTok("current", "radius.md", clampSpec("radius.md", float64(v)))
		return nil, []string{"radius.md " + n}, true
	}
	return nil, nil, false
}

func windowRef(s string) string {
	s = strings.TrimSpace(s)
	switch s {
	case "", "this", "this window", "the window", "esta janela", "essa janela", "janela", "window", "it":
		return "focused"
	}
	s = strings.TrimPrefix(s, "the ")
	s = strings.TrimPrefix(strings.TrimPrefix(s, "o "), "a ")
	s = strings.TrimSuffix(s, " window")
	return s
}

func orFocused(s string) string {
	if r := windowRef(s); r != "focused" {
		return r
	}
	return "the focused window"
}
