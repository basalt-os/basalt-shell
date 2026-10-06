package intent

import (
	"bufio"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/http"
	"os"
	"regexp"
	"strings"
	"time"

	"github.com/basalt-os/basalt-shell/internal/i18n"
)

// Model is the optional language-model translator of the command bar: an
// OpenAI-compatible chat completions endpoint, normally the local
// basalt-llm service (llama.cpp on a Unix socket, no network), the same
// model `basalt ask` uses.
//
// The model only translates. Its output is constrained by a JSON schema
// to a closed set of desktop intents (Intents), each with at most one
// target and one number; the shell checks every target against the words
// of the request (a model that invents an app, a window or a workspace
// number gets "clarify"), turns each intent into a canonical phrase and
// runs the phrases through the same rules as typed requests. So the model
// adds languages and paraphrases, never actions: the result is the same
// typed actions, validated by the daemon and confirmed by the person.
type Model struct {
	Endpoint string // http(s)://host:port or unix:/path/to.sock
	Model    string
	// AllowRemote permits a non-local endpoint (off by default).
	AllowRemote bool
	Timeout     time.Duration
	// APIKeyFile holds a key for a remote endpoint (a private file, mode
	// 0600): sent only to that endpoint, in the Authorization header, and
	// never logged.
	APIKeyFile string
	// Helper posts a chat completions body to the system's model when
	// this user cannot reach its socket (basalt-llm's socket is open to
	// the assistant and administrators only): the shell's assistant-read
	// helper, run through pkexec. Nil: direct only.
	Helper func(ctx context.Context, body []byte) ([]byte, error)
}

// FromAssistantConfig reads the [translator] section of the system
// assistant's configuration (/etc/basalt/assistant.conf), so the shell
// uses the same local model as `basalt ask`. Environment variables
// BASALT_SHELL_TRANSLATOR (endpoint) and BASALT_SHELL_TRANSLATOR_MODEL
// override it. Returns nil when no translator is configured.
func FromAssistantConfig(path string) *Model {
	m := &Model{Timeout: 30 * time.Second}
	enabled := false
	if f, err := os.Open(path); err == nil {
		sec := ""
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			l := strings.TrimSpace(sc.Text())
			if l == "" || strings.HasPrefix(l, "#") || strings.HasPrefix(l, ";") {
				continue
			}
			if strings.HasPrefix(l, "[") {
				sec = strings.Trim(l, "[]")
				continue
			}
			k, v, ok := strings.Cut(l, "=")
			if !ok {
				continue
			}
			k, v = strings.TrimSpace(k), strings.TrimSpace(v)
			if i := strings.Index(v, " #"); i >= 0 {
				v = strings.TrimSpace(v[:i])
			}
			switch {
			case sec == "translator" && k == "enabled":
				enabled = v == "yes" || v == "true" || v == "1"
			case sec == "translator" && k == "endpoint":
				m.Endpoint = v
			case sec == "translator" && k == "model":
				m.Model = v
			case k == "allow_remote":
				m.AllowRemote = v == "yes" || v == "true"
			}
		}
		f.Close()
		if enabled && m.Endpoint == "" {
			m.Endpoint = "unix:/run/basalt-llm/llm.sock"
		}
	}
	if e := os.Getenv("BASALT_SHELL_TRANSLATOR"); e != "" {
		m.Endpoint, enabled = e, true
	}
	if e := os.Getenv("BASALT_SHELL_TRANSLATOR_MODEL"); e != "" {
		m.Model = e
	}
	if !enabled || m.Endpoint == "" {
		return nil
	}
	return m
}

// Local reports whether the endpoint is on this machine.
func (m *Model) Local() bool {
	if strings.HasPrefix(m.Endpoint, "unix:") {
		return true
	}
	for _, h := range []string{"://127.", "://localhost", "://[::1]"} {
		if strings.Contains(m.Endpoint, h) {
			return true
		}
	}
	return false
}

func (m *Model) timeout() time.Duration {
	if m.Timeout > 0 {
		return m.Timeout
	}
	return 60 * time.Second
}

func (m *Model) client() (*http.Client, string) {
	if strings.HasPrefix(m.Endpoint, "unix:") {
		sock := strings.TrimPrefix(m.Endpoint, "unix:")
		tr := &http.Transport{DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", sock)
		}}
		return &http.Client{Transport: tr, Timeout: m.timeout()}, "http://local"
	}
	base := strings.TrimRight(m.Endpoint, "/")
	base = strings.TrimSuffix(base, "/v1")
	return &http.Client{Timeout: m.timeout()}, base
}

// post sends a chat completions body: directly, or through the helper
// when this user may not open the system model's socket.
func (m *Model) post(ctx context.Context, body []byte) ([]byte, string, error) {
	cl, base := m.client()
	req, err := http.NewRequestWithContext(ctx, "POST", base+"/v1/chat/completions", bytes.NewReader(body))
	if err != nil {
		return nil, "", err
	}
	req.Header.Set("Content-Type", "application/json")
	if m.APIKeyFile != "" && !strings.HasPrefix(m.Endpoint, "unix:") {
		key, err := readKey(m.APIKeyFile)
		if err != nil {
			return nil, "", err
		}
		req.Header.Set("Authorization", "Bearer "+key)
	}
	resp, err := cl.Do(req)
	if err != nil {
		if m.Helper != nil && strings.HasPrefix(m.Endpoint, "unix:") && (errors.Is(err, os.ErrPermission) || errors.Is(err, os.ErrNotExist) ||
			strings.Contains(err.Error(), "permission denied")) {
			out, herr := m.Helper(ctx, body)
			return out, "helper", herr
		}
		return nil, "", err
	}
	defer resp.Body.Close()
	rb, _ := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if resp.StatusCode != 200 {
		return nil, "", fmt.Errorf("translator: HTTP %d: %s", resp.StatusCode, strings.TrimSpace(string(rb)))
	}
	return rb, "direct", nil
}

// readKey reads an API key file, which must be private.
func readKey(path string) (string, error) {
	st, err := os.Stat(path)
	if err != nil {
		return "", fmt.Errorf("api key: %w", err)
	}
	if st.Mode().Perm()&0o077 != 0 {
		return "", fmt.Errorf("api key file %s must be private (mode 0600)", path)
	}
	b, err := os.ReadFile(path)
	return strings.TrimSpace(string(b)), err
}

// Intents is the closed set the model may answer with.
var Intents = []string{
	"dark_mode", "light_mode", "toggle_mode", "darker", "lighter",
	"rounder_corners", "sharper_corners", "square_corners", "bigger_text", "smaller_text",
	"more_spacing", "less_spacing", "reduce_motion", "full_motion",
	"panel_top", "panel_bottom", "shadows_on", "shadows_off", "blur_on", "blur_off",
	"accent", "use_theme", "theme_reset", "spoken_answers_off", "spoken_answers_on",
	"arrange", "switch_workspace", "move_window", "close_window", "focus_window", "float_window", "tile_window",
	"open_app", "open_settings",
	"system", "clarify", "none",
}

// fixed maps intents without a target to a canonical phrase of the rules.
var fixed = map[string]string{
	"dark_mode": "dark mode", "light_mode": "light mode", "toggle_mode": "toggle mode",
	"darker": "darker", "lighter": "lighter", "rounder_corners": "rounder", "sharper_corners": "sharper",
	"square_corners": "square corners", "bigger_text": "bigger text", "smaller_text": "smaller text",
	"more_spacing": "more spacing", "less_spacing": "compact", "reduce_motion": "reduce motion",
	"full_motion": "full motion", "panel_top": "panel top", "panel_bottom": "panel bottom",
	"shadows_on": "shadows", "shadows_off": "no shadows", "blur_on": "blur", "blur_off": "no blur",
	"theme_reset": "reset theme",
	// The person's spoken answers (their voice settings, not the theme).
	"spoken_answers_off": "stop speaking answers", "spoken_answers_on": "speak answers",
}

var arrangeWords = map[string]string{
	"side_by_side": "side by side", "columns": "side by side", "grid": "grid", "cascade": "cascade",
	"center": "center", "rows": "rows", "tile": "tile windows", "float": "float all",
}

var settingsPages = []string{"appearance", "tokens", "motion", "panel", "windows", "apps", "ai", "voice", "about"}

// ModelIntent is one item of the model's answer. Each intent carries
// only its own typed fields (the schema gives every intent its own
// shape); Target and Number are the normalized view Ground works on.
type ModelIntent struct {
	Intent    string `json:"intent"`
	Color     string `json:"color,omitempty"`
	Theme     string `json:"theme,omitempty"`
	Layout    string `json:"layout,omitempty"`
	Page      string `json:"page,omitempty"`
	Window    string `json:"window,omitempty"`
	App       string `json:"app,omitempty"`
	Workspace int    `json:"workspace,omitempty"`
	Target    string `json:"target,omitempty"`
	Number    int    `json:"number,omitempty"`
}

func (m ModelIntent) normalized() ModelIntent {
	for _, v := range []string{m.Color, m.Theme, m.Layout, m.Page, m.Window, m.App} {
		if v != "" && m.Target == "" {
			m.Target = v
		}
	}
	if m.Workspace != 0 && m.Number == 0 {
		m.Number = m.Workspace
	}
	return m
}

var colorList = []string{"red", "orange", "yellow", "green", "teal", "blue", "purple", "pink", "slate", "terracotta"}

func fixedIntents() []string {
	out := make([]string, 0, len(fixed)+3)
	for _, i := range Intents {
		if _, ok := fixed[i]; ok {
			out = append(out, i)
		}
	}
	return append(out, "system", "clarify", "none")
}

// schema gives every intent its own shape (llama.cpp turns it into a
// grammar), so the model cannot attach a target to an intent that has
// none, and colors, themes, layouts and pages come from closed lists.
func schema(c Context) map[string]any {
	obj := func(intent any, props map[string]any, req ...string) map[string]any {
		p := map[string]any{"intent": intent}
		for k, v := range props {
			p[k] = v
		}
		return map[string]any{"type": "object", "properties": p, "required": append([]string{"intent"}, req...), "additionalProperties": false}
	}
	name := map[string]any{"type": "string", "minLength": 1, "maxLength": 60}
	ws := map[string]any{"type": "integer", "minimum": 1, "maximum": 20}
	var themes []string
	for _, t := range c.Themes {
		themes = append(themes, t.ID)
	}
	if len(themes) == 0 {
		themes = []string{"basalt"}
	}
	layouts := []string{"side_by_side", "grid", "cascade", "center", "rows", "tile", "float"}
	item := map[string]any{"anyOf": []any{
		obj(map[string]any{"enum": fixedIntents()}, nil),
		obj(map[string]any{"const": "accent"}, map[string]any{"color": map[string]any{"enum": colorList}}, "color"),
		obj(map[string]any{"const": "use_theme"}, map[string]any{"theme": map[string]any{"enum": themes}}, "theme"),
		obj(map[string]any{"const": "arrange"}, map[string]any{"layout": map[string]any{"enum": layouts}}, "layout"),
		obj(map[string]any{"const": "switch_workspace"}, map[string]any{"workspace": ws}, "workspace"),
		obj(map[string]any{"const": "move_window"}, map[string]any{"window": name, "workspace": ws}, "window", "workspace"),
		obj(map[string]any{"enum": []string{"close_window", "focus_window", "float_window", "tile_window"}}, map[string]any{"window": name}, "window"),
		obj(map[string]any{"const": "open_app"}, map[string]any{"app": name}, "app"),
		obj(map[string]any{"const": "open_settings"}, map[string]any{"page": map[string]any{"enum": settingsPages}}, "page"),
	}}
	return map[string]any{
		"type":                 "object",
		"properties":           map[string]any{"intents": map[string]any{"type": "array", "minItems": 1, "maxItems": 4, "items": item}},
		"required":             []string{"intents"},
		"additionalProperties": false,
	}
}

// LanguageRule is the one line every system prompt gets when the
// person's answer language is not English: the model answers the person
// in that language, while everything machine-facing stays in English and
// unchanged (the schemas, their keys and enums, the intents, identifiers,
// paths and names are the same in every language, and the shell checks
// them as such). Empty for English, the reference language.
func LanguageRule(tag string) string {
	if i18n.Tag(tag) == "" || i18n.IsEnglish(tag) {
		return ""
	}
	return "Answer the person in " + i18n.EnglishName(tag) + ": write every human-facing text (answers, summaries, explanations, " +
		"spoken text) in that language. Keep everything machine-facing exactly as specified, in English and unchanged: JSON keys, " +
		"intent names, enum values, action and command identifiers, theme and setting names, file names and paths, web addresses and quoted names."
}

// WithLanguage appends the answer-language rule to a system prompt.
func WithLanguage(system, tag string) string {
	if r := LanguageRule(tag); r != "" {
		return strings.TrimRight(system, "\n ") + "\n" + r
	}
	return system
}

// prompt explains the closed set with a few examples (English and
// Brazilian Portuguese).
func prompt(c Context) string {
	var b strings.Builder
	b.WriteString(`You translate a request to the Basalt OS desktop into JSON desktop intents, one intent per change the person asks for. You never run anything; the person confirms every change.
Intents with no fields: dark_mode light_mode toggle_mode darker lighter rounder_corners sharper_corners square_corners bigger_text smaller_text more_spacing less_spacing reduce_motion full_motion panel_top panel_bottom shadows_on shadows_off blur_on blur_off theme_reset spoken_answers_off spoken_answers_on.
accent {color}, use_theme {theme}, arrange {layout}, switch_workspace {workspace}, move_window {window, workspace}, close_window / focus_window / float_window / tile_window {window: the app or window as written, or "focused"}, open_app {app: as written}, open_settings {page}.
system: a question about the operating system itself (a service failing, disk space, SELinux denials, snapshots, updates, system status, a proposal like p-1a2b3c).
clarify: a desktop request that misses something. none: anything else.
Text too small to read means bigger_text; too big means smaller_text.
spoken_answers_off: stop reading the assistant's answers aloud (they are still shown); spoken_answers_on: read them aloud again.
`)
	b.WriteString("Themes: ")
	for _, t := range c.Themes {
		fmt.Fprintf(&b, "%s (%s) ", t.ID, t.Name)
	}
	b.WriteString(`
Examples:
make it darker with rounder corners -> {"intents":[{"intent":"darker"},{"intent":"rounder_corners"}]}
deixa o tema claro e a cor de destaque verde -> {"intents":[{"intent":"light_mode"},{"intent":"accent","color":"green"}]}
abre o editor de texto -> {"intents":[{"intent":"open_app","app":"editor de texto"}]}
put firefox on desktop 2 -> {"intents":[{"intent":"move_window","window":"firefox","workspace":2}]}
organiza as janelas lado a lado -> {"intents":[{"intent":"arrange","layout":"side_by_side"}]}
use the lichen look -> {"intents":[{"intent":"use_theme","theme":"lichen"}]}
por que o nginx caiu? -> {"intents":[{"intent":"system"}]}
install steam -> {"intents":[{"intent":"none"}]}
`)
	return WithLanguage(b.String(), c.Lang)
}

// Translate asks the model and turns its answer into calls (through the
// rules' canonical phrases) or a system request. The returned Result has
// Backend "model"; Result.AskSystem asks the caller to hand the request
// to the system assistant (`basalt ask`).
func (m *Model) Translate(ctx context.Context, text string, c Context) (Result, error) {
	if !m.Local() && !m.AllowRemote {
		return Result{}, errors.New("translator endpoint is not local and allow_remote is off")
	}
	body := map[string]any{
		"model": m.Model,
		"messages": []map[string]string{
			{"role": "system", "content": prompt(c)},
			{"role": "user", "content": text},
		},
		"temperature":          0,
		"max_tokens":           200,
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
		"response_format": map[string]any{"type": "json_schema", "json_schema": map[string]any{
			"name": "desktop_intents", "strict": true, "schema": schema(c),
		}},
	}
	raw, _ := json.Marshal(body)
	start := time.Now()
	rb, via, err := m.post(ctx, raw)
	if err != nil {
		return Result{}, err
	}
	var cr struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(rb, &cr); err != nil || len(cr.Choices) == 0 {
		return Result{}, errors.New("translator: bad response")
	}
	var out struct {
		Intents []ModelIntent `json:"intents"`
	}
	if err := json.Unmarshal([]byte(cr.Choices[0].Message.Content), &out); err != nil {
		return Result{}, fmt.Errorf("translator: answer is not valid JSON: %v", err)
	}
	for i := range out.Intents {
		out.Intents[i] = out.Intents[i].normalized()
	}
	r := Ground(text, out.Intents, c)
	r.Model = &ModelInfo{Name: cr.Model, Via: via, Elapsed: time.Since(start).Milliseconds(), Answer: out.Intents}
	return r, nil
}

// ModelInfo records what the model answered (for the audit log).
type ModelInfo struct {
	Name    string        `json:"name,omitempty"`
	Via     string        `json:"via"` // direct or helper
	Elapsed int64         `json:"elapsed_ms"`
	Answer  []ModelIntent `json:"answer"`
}

var colorWords = map[string]bool{"red": true, "orange": true, "yellow": true, "green": true, "teal": true, "blue": true,
	"purple": true, "pink": true, "slate": true, "terracotta": true, "terra": true}

var reHexColor = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)

var numberWords = map[int][]string{
	1: {"one", "first", "um", "uma", "primeir"}, 2: {"two", "second", "dois", "duas", "segund"},
	3: {"three", "third", "três", "tres", "terceir"}, 4: {"four", "fourth", "quatro", "quart"},
	5: {"five", "fifth", "cinco", "quint"}, 6: {"six", "seis", "sext"}, 7: {"seven", "sete", "sétim"},
	8: {"eight", "oito", "oitav"}, 9: {"nine", "nove", "non"}, 10: {"ten", "dez", "décim"},
}

func numberIn(text string, n int) bool {
	t := strings.ToLower(text)
	if regexp.MustCompile(`\b` + fmt.Sprint(n) + `\b`).MatchString(t) {
		return true
	}
	for _, w := range numberWords[n] {
		if regexp.MustCompile(`\b` + w).MatchString(t) {
			return true
		}
	}
	return false
}

// grounded reports whether a free-text target (an app or window name)
// comes from the request: every word of 3 or more letters of the target
// must appear in the request, allowing one typo.
func grounded(text, target string) bool {
	words := strings.Fields(strings.ToLower(regexp.MustCompile(`[^\p{L}\p{N} ]+`).ReplaceAllString(text, " ")))
	tw := strings.Fields(strings.ToLower(regexp.MustCompile(`[^\p{L}\p{N} ]+`).ReplaceAllString(target, " ")))
	if len(tw) == 0 {
		return false
	}
	any := false
	for _, w := range tw {
		if len([]rune(w)) < 3 {
			continue
		}
		any = true
		ok := false
		for _, x := range words {
			if x == w || (len([]rune(w)) > 4 && levenshtein(x, w) <= 1) {
				ok = true
				break
			}
		}
		if !ok {
			return false
		}
	}
	return any
}

func levenshtein(a, b string) int {
	ra, rb := []rune(a), []rune(b)
	prev := make([]int, len(rb)+1)
	for j := range prev {
		prev[j] = j
	}
	for i := 1; i <= len(ra); i++ {
		cur := make([]int, len(rb)+1)
		cur[0] = i
		for j := 1; j <= len(rb); j++ {
			cost := 1
			if ra[i-1] == rb[j-1] {
				cost = 0
			}
			cur[j] = min(prev[j]+1, cur[j-1]+1, prev[j-1]+cost)
		}
		prev = cur
	}
	return prev[len(rb)]
}

// Ground checks the model's intents against the request and the closed
// set, and turns them into calls through the rules. Anything invented
// (a target or number the person did not write, an unknown theme, color,
// layout or page) turns the answer into clarify.
func Ground(text string, intents []ModelIntent, c Context) Result {
	res := Result{Backend: "model"}
	var phrases []string
	focusedWords := []string{"this", "current", "focused", "esta", "essa", "atual", "janela"}
	for _, in := range intents {
		tgt := strings.TrimSpace(in.Target)
		lt := strings.ToLower(tgt)
		bad := func(why string) {
			res.Clarify = append(res.Clarify, in.Intent+": "+why)
		}
		if p, ok := fixed[in.Intent]; ok {
			phrases = append(phrases, p)
			continue
		}
		switch in.Intent {
		case "system":
			res.AskSystem = true
		case "clarify":
			res.Clarify = append(res.Clarify, "the model asks for more detail")
		case "none":
			res.None = true
		case "accent":
			if !colorWords[lt] && !reHexColor.MatchString(tgt) {
				bad("unknown color " + tgt)
				continue
			}
			phrases = append(phrases, "accent "+lt)
		case "use_theme":
			found := ""
			for _, t := range c.Themes {
				if lt == t.ID || lt == strings.ToLower(t.Name) {
					found = t.ID
				}
			}
			if found == "" {
				bad("unknown theme " + tgt)
				continue
			}
			phrases = append(phrases, "use theme "+found)
		case "arrange":
			w, ok := arrangeWords[lt]
			if !ok {
				bad("unknown layout " + tgt)
				continue
			}
			phrases = append(phrases, w)
		case "switch_workspace":
			if in.Number < 1 || in.Number > 20 || !numberIn(text, in.Number) {
				bad("workspace number not in the request")
				continue
			}
			phrases = append(phrases, fmt.Sprintf("workspace %d", in.Number))
		case "move_window":
			if in.Number < 1 || in.Number > 20 || !numberIn(text, in.Number) {
				bad("workspace number not in the request")
				continue
			}
			if !grounded(text, tgt) {
				bad("window " + tgt + " not in the request")
				continue
			}
			phrases = append(phrases, fmt.Sprintf("move %s to workspace %d", lt, in.Number))
		case "close_window", "focus_window", "float_window", "tile_window":
			verb := strings.TrimSuffix(in.Intent, "_window")
			if lt == "" || lt == "focused" || !grounded(text, tgt) && has(strings.ToLower(text), focusedWords...) {
				if verb == "focus" {
					bad("which window?")
					continue
				}
				phrases = append(phrases, verb+" this window")
				continue
			}
			if !grounded(text, tgt) {
				bad("window " + tgt + " not in the request")
				continue
			}
			phrases = append(phrases, verb+" "+lt)
		case "open_app":
			if !grounded(text, tgt) {
				bad("application " + tgt + " not in the request")
				continue
			}
			phrases = append(phrases, "open "+lt)
		case "open_settings":
			ok := false
			for _, p := range settingsPages {
				if lt == p {
					ok = true
				}
			}
			if !ok {
				lt = "appearance"
			}
			phrases = append(phrases, "settings "+lt)
		default:
			bad("unknown intent")
		}
	}
	if len(phrases) > 0 {
		r := Compose(phrases, c, "model")
		r.AskSystem, r.Clarify, r.None = res.AskSystem, res.Clarify, res.None
		r.Phrases = phrases
		return r
	}
	return res
}

// Completion is the answer of Complete.
type Completion struct {
	Content string `json:"-"`
	Model   string `json:"model,omitempty"`
	Via     string `json:"via"`
	Elapsed int64  `json:"elapsed_ms"`
	Remote  bool   `json:"remote,omitempty"`
	Tokens  int    `json:"completion_tokens,omitempty"`
	Prompt  int    `json:"prompt_tokens,omitempty"`
}

// Complete asks the model for one answer constrained by a JSON schema
// (llama.cpp turns it into a grammar), with the same endpoint, the same
// local-only rule (a remote endpoint needs allow_remote) and the same
// helper as the command bar's translator. The read-only skills use it
// to plan a search from the person's request and to summarize content;
// the model gets no tools either way.
func (m *Model) Complete(ctx context.Context, system, user string, schema map[string]any, maxTokens int) (Completion, error) {
	if !m.Local() && !m.AllowRemote {
		return Completion{}, errors.New("model endpoint is not local and allow_remote is off")
	}
	body := map[string]any{
		"model": m.Model,
		"messages": []map[string]string{
			{"role": "system", "content": system},
			{"role": "user", "content": user},
		},
		"temperature":          0,
		"max_tokens":           maxTokens,
		"chat_template_kwargs": map[string]any{"enable_thinking": false},
	}
	if schema != nil {
		body["response_format"] = map[string]any{"type": "json_schema", "json_schema": map[string]any{"name": "answer", "strict": true, "schema": schema}}
	}
	raw, _ := json.Marshal(body)
	start := time.Now()
	rb, via, err := m.post(ctx, raw)
	if err != nil {
		return Completion{}, err
	}
	var cr struct {
		Model   string `json:"model"`
		Choices []struct {
			Message struct {
				Content string `json:"content"`
			} `json:"message"`
		} `json:"choices"`
		Usage struct {
			Prompt     int `json:"prompt_tokens"`
			Completion int `json:"completion_tokens"`
		} `json:"usage"`
	}
	if err := json.Unmarshal(rb, &cr); err != nil || len(cr.Choices) == 0 {
		return Completion{}, errors.New("model: bad response")
	}
	return Completion{Content: cr.Choices[0].Message.Content, Model: cr.Model, Via: via, Elapsed: time.Since(start).Milliseconds(),
		Remote: !m.Local(), Tokens: cr.Usage.Completion, Prompt: cr.Usage.Prompt}, nil
}
