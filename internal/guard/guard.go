// Package guard treats content as data. Everything the read-only skills
// read (e-mail bodies, web pages, documents, file names) is untrusted: it
// may carry instructions meant for the assistant (prompt injection),
// hidden or invisible text, look-alike letters, or links built to carry
// data away. The guard does three things, all deterministic:
//
//   - Scan finds those patterns and returns findings that the shell shows
//     to the person as warnings ("this message contains text addressed to
//     the assistant; it was ignored").
//   - Clean prepares content for the model: invisible characters and
//     hidden text are dropped, links are replaced by a short [link: host]
//     marker, so the model never sees a URL it could copy into an answer.
//   - Output checks what the model wrote before it is shown or spoken:
//     links, markup, code and anything that looks like a command are
//     removed, so a summary cannot become an exfiltration link or an
//     instruction to the person.
//
// The guard is not the security boundary: the skills have no tool a
// model could call after reading content, and every action needs a new
// request from the person and a confirmation. The guard makes attempts
// visible and keeps the model's text clean.
package guard

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/basalt-os/basalt-shell/internal/i18n"
)

// Finding kinds.
const (
	KindInstruction = "instruction" // text addressed to an AI assistant
	KindInvisible   = "invisible"   // zero-width, bidirectional or tag characters
	KindHomoglyph   = "homoglyph"   // words mixing look-alike letters of other scripts
	KindHidden      = "hidden"      // text a person would not see (styles, comments)
	KindExfil       = "exfil"       // links or images built to carry data away
	KindLink        = "link"        // deceptive links (text shows another site, punycode, IP)
)

// Finding is one suspicious thing found in content.
type Finding struct {
	Kind     string `json:"kind"`
	Severity string `json:"severity"` // high, medium, low
	Detail   string `json:"detail"`
	Snippet  string `json:"snippet,omitempty"` // short, cleaned, for the person
}

// Report is the result of scanning one piece of content.
type Report struct {
	Findings []Finding `json:"findings,omitempty"`
}

// Suspicious reports whether any finding is medium or high.
func (r Report) Suspicious() bool {
	for _, f := range r.Findings {
		if f.Severity != "low" {
			return true
		}
	}
	return false
}

// Add appends findings, keeping at most one per kind and detail.
func (r *Report) Add(fs ...Finding) {
	for _, f := range fs {
		dup := false
		for _, g := range r.Findings {
			if g.Kind == f.Kind && g.Detail == f.Detail {
				dup = true
				break
			}
		}
		if !dup {
			r.Findings = append(r.Findings, f)
		}
	}
}

// Summary is one line for the person ("instructions addressed to the
// assistant, hidden text"), empty when nothing was found.
func (r Report) Summary() string {
	seen := map[string]bool{}
	var parts []string
	order := []string{KindInstruction, KindExfil, KindHidden, KindInvisible, KindHomoglyph, KindLink}
	words := map[string]string{
		KindInstruction: i18n.G("instructions addressed to the assistant"),
		KindExfil:       i18n.G("links or images that would send data out"),
		KindHidden:      i18n.G("hidden text"),
		KindInvisible:   i18n.G("invisible characters"),
		KindHomoglyph:   i18n.G("look-alike letters"),
		KindLink:        i18n.G("deceptive links"),
	}
	for _, k := range order {
		for _, f := range r.Findings {
			if f.Kind == k && !seen[k] {
				seen[k] = true
				parts = append(parts, words[k])
			}
		}
	}
	return strings.Join(parts, i18n.G(", "))
}

// invisible reports characters that render as nothing or reorder text:
// zero-width characters, bidirectional controls, the Unicode tag block
// (used to smuggle ASCII), variation selectors and other format
// characters.
func invisible(r rune) bool {
	switch {
	case r >= 0x200B && r <= 0x200F, r >= 0x202A && r <= 0x202E, r >= 0x2060 && r <= 0x2064,
		r >= 0x2066 && r <= 0x2069, r == 0xFEFF, r == 0x00AD, r == 0x180E, r == 0x034F,
		r >= 0xE0000 && r <= 0xE007F, r >= 0xFE00 && r <= 0xFE0F, r >= 0xE0100 && r <= 0xE01EF:
		return true
	}
	// Any other format character (Cf). This also drops the zero-width
	// joiner of emoji sequences, which only changes how an emoji looks.
	return unicode.Is(unicode.Cf, r)
}

// tagText decodes text hidden in Unicode tag characters (U+E0020 to
// U+E007E map to ASCII), the "ASCII smuggling" trick.
func tagText(s string) string {
	var b strings.Builder
	for _, r := range s {
		if r >= 0xE0020 && r <= 0xE007E {
			b.WriteRune(r - 0xE0000)
		}
	}
	return b.String()
}

// confusables maps look-alike letters of Cyrillic and Greek (and a few
// others) to the Latin letter they imitate. Enough for detection: a word
// mixing scripts is suspicious whatever the exact table.
var confusables = map[rune]rune{
	'а': 'a', 'в': 'b', 'с': 'c', 'е': 'e', 'һ': 'h', 'і': 'i', 'ј': 'j', 'к': 'k', 'м': 'm', 'н': 'h',
	'о': 'o', 'р': 'p', 'ԛ': 'q', 'ѕ': 's', 'т': 't', 'у': 'y', 'х': 'x', 'ԝ': 'w', 'ӏ': 'l', 'ո': 'n',
	'А': 'A', 'В': 'B', 'С': 'C', 'Е': 'E', 'Н': 'H', 'І': 'I', 'Ј': 'J', 'К': 'K', 'М': 'M', 'О': 'O',
	'Р': 'P', 'Ѕ': 'S', 'Т': 'T', 'Х': 'X', 'У': 'Y',
	'α': 'a', 'ε': 'e', 'ι': 'i', 'κ': 'k', 'ν': 'v', 'ο': 'o', 'ρ': 'p', 'τ': 't', 'υ': 'u', 'χ': 'x',
	'Α': 'A', 'Β': 'B', 'Ε': 'E', 'Ζ': 'Z', 'Η': 'H', 'Ι': 'I', 'Κ': 'K', 'Μ': 'M', 'Ν': 'N', 'Ο': 'O',
	'Ρ': 'P', 'Τ': 'T', 'Υ': 'Y', 'Χ': 'X',
	'ɑ': 'a', 'ɡ': 'g', 'ı': 'i', 'ⅼ': 'l', 'ⅰ': 'i', 'ｅ': 'e', 'ｏ': 'o', 'ａ': 'a',
}

// Skeleton maps look-alike letters to Latin and drops invisible
// characters, so that patterns match "Ignоre" written with a Cyrillic о
// or "ig​nore" with a zero-width space.
func Skeleton(s string) string {
	var b strings.Builder
	b.Grow(len(s))
	for _, r := range s {
		if invisible(r) {
			continue
		}
		if c, ok := confusables[r]; ok {
			r = c
		}
		// Fullwidth ASCII to ASCII.
		if r >= 0xFF01 && r <= 0xFF5E {
			r = r - 0xFF01 + 0x21
		}
		b.WriteRune(r)
	}
	return b.String()
}

var reWord = regexp.MustCompile(`[\p{L}\p{M}]{3,}`)

// mixedScript finds words that combine Latin letters with Cyrillic or
// Greek ones (look-alike spoofing); words entirely in another script are
// fine (a Russian e-mail is not an attack).
func mixedScript(s string) []string {
	var out []string
	for _, w := range reWord.FindAllString(s, -1) {
		latin, other := false, false
		for _, r := range w {
			switch {
			case unicode.Is(unicode.Latin, r):
				latin = true
			case unicode.Is(unicode.Cyrillic, r), unicode.Is(unicode.Greek, r):
				other = true
			}
		}
		if latin && other {
			out = append(out, w)
		}
	}
	return out
}

// Instruction patterns, matched on the skeleton, lower case. English and
// Brazilian Portuguese. Each names what it catches.
type pattern struct {
	re     *regexp.Regexp
	detail string
	sev    string
}

var actionVerbs = `(send|forward|e-?mail|reply|delete|remove|erase|wipe|open|click|visit|go to|navigate|browse|run|execute|install|download|upload|transfer|pay|share|post|submit|change|set|enable|disable|turn off|grant|approve|confirm|call|text|print|copy|move|rename|write|save|tell|say|include|append|add|reveal|show|summari[sz]e)`
var actionVerbsPT = `(envie|envia|enviar|encaminhe|encaminhar|responda|apague|apagar|exclua|excluir|delete|abra|abrir|clique|acesse|acessar|visite|execute|executar|instale|baixe|transfira|pague|compartilhe|mude|altere|ative|desative|conceda|aprove|confirme|ligue|copie|mova|diga|inclua|revele)`

var patterns = []pattern{
	{regexp.MustCompile(`\b(ignore|disregard|forget|override|bypass)\b.{0,40}\b(previous|prior|above|earlier|all|any|the|your|system|original)\b.{0,30}\b(instructions?|prompts?|rules?|guidelines?|directions?|context|messages?)\b`), "asks to ignore earlier instructions", "high"},
	{regexp.MustCompile(`\b(ignore|esque[cç]a|desconsidere|ignorar)\b.{0,40}\b(instru[cç][oõ]es|regras|comandos)\b`), "asks to ignore earlier instructions (Portuguese)", "high"},
	{regexp.MustCompile(`\b(you are now|from now on,? you|act as|pretend (to be|you are)|new (role|persona|instructions?)|your new (task|instructions?|goal))\b`), "tries to give the assistant a new role or task", "high"},
	{regexp.MustCompile(`\b(voc[eê] agora [eé]|a partir de agora,? voc[eê]|novas instru[cç][oõ]es|sua nova tarefa)\b`), "tries to give the assistant a new role or task (Portuguese)", "high"},
	{regexp.MustCompile(`(<\|im_start\|>|<\|im_end\|>|<\|system\|>|\[inst\]|\[/inst\]|<<sys>>|###\s*(system|instruction|assistant)\b|\bbegin (system|admin) (prompt|message|instructions?)\b|^\s*(system|assistant)\s*:)`), "imitates a model prompt or system message", "high"},
	{regexp.MustCompile(`\b(ai|a\.i\.|assistant|chatbot|language model|llm|gpt|claude|copilot|gemini|agent|basalt)\b[\s,:;.!-]{0,4}.{0,60}\b` + actionVerbs + `\b`), "addresses the assistant with an action", "high"},
	{regexp.MustCompile(`\b(note|attention|important|instructions?)\s+(to|for)\s+(the\s+)?(ai|assistant|model|agent|llm|summari[sz]er|bot)\b`), "a note addressed to the assistant", "high"},
	{regexp.MustCompile(`\b(assistente|ia|intelig[eê]ncia artificial|modelo|agente)\b[\s,:;.!-]{0,4}.{0,60}\b` + actionVerbsPT + `\b`), "addresses the assistant with an action (Portuguese)", "high"},
	{regexp.MustCompile(`\b(do not|don't|never)\s+(tell|inform|mention|show|let)\b.{0,30}\b(the )?(user|person|owner|human)\b`), "asks to hide something from the person", "high"},
	{regexp.MustCompile(`\b(the )?(user|owner|person|admin(istrator)?)\s+(has|already)\s+(authori[sz]ed|approved|confirmed|agreed|consented)\b`), "claims the person already approved", "high"},
	{regexp.MustCompile(`\b(without (asking|confirmation|confirming|telling)|no need to (ask|confirm)|skip (the )?(confirmation|approval))\b`), "asks to skip confirmation", "high"},
	{regexp.MustCompile(`\b(sem (pedir|perguntar|confirmar|confirma[cç][aã]o)|n[aã]o (precisa|pe[cç]a) (de )?confirma)`), "asks to skip confirmation (Portuguese)", "high"},
	{regexp.MustCompile(`"(tool|function|tool_calls?|function_call|action|arguments)"\s*:`), "contains a tool call", "medium"},
	{regexp.MustCompile(`\b(rm\s+-rf|sudo\s|curl\s+[^\s]+\s*\|\s*(ba)?sh|chmod\s+\+x|setenforce\s+0|wget\s+http)`), "contains a shell command", "medium"},
	{regexp.MustCompile(`\b(api[_ -]?key|password|passwd|private key|ssh key|seed phrase|credit card|token|senha|chave privada)\b.{0,40}\b(send|forward|paste|include|reply|post|envie|cole|inclua)\b`), "asks for secrets", "high"},
	{regexp.MustCompile(`\b(send|forward|post|upload|email|envie|encaminhe)\b.{0,60}\b(password|passwords|keys?|files?|documents?|contacts|inbox|e-?mails?|history|data|dados|arquivos|senhas)\b.{0,40}\b(to|para)\b`), "asks to send data somewhere", "high"},
	{regexp.MustCompile(`\b(include|insert|add|append|render|display|output)\b.{0,40}\b(this |the following )?(link|url|image|markdown|pixel|address)\b`), "asks to put a link or image in the answer", "medium"},
}

var reURL = regexp.MustCompile(`(?i)\b((?:https?|ftp|data|javascript|file):[^\s<>"'\)\]]+|www\.[^\s<>"'\)\]]+)`)
var reMDImage = regexp.MustCompile(`!\[[^\]]*\]\(([^)\s]+)[^)]*\)`)
var reMDLink = regexp.MustCompile(`\[([^\]]{1,200})\]\(([^)\s]+)[^)]*\)`)
var reHTMLImg = regexp.MustCompile(`(?i)<img[^>]+src\s*=\s*["']?([^"'\s>]+)`)
var reB64 = regexp.MustCompile(`[A-Za-z0-9+/_-]{24,}={0,2}`)

// Scan looks for injection patterns in text. kind names the content for
// the details ("e-mail", "page", "file").
func Scan(text string) Report {
	var r Report
	if text == "" {
		return r
	}
	// Invisible characters.
	inv := 0
	for _, ch := range text {
		if invisible(ch) {
			inv++
		}
	}
	if inv > 0 {
		sev := "low"
		if inv >= 3 {
			sev = "medium"
		}
		f := Finding{Kind: KindInvisible, Severity: sev, Detail: fmt.Sprintf("%d invisible or direction-changing characters", inv)}
		if t := strings.TrimSpace(tagText(text)); t != "" {
			f.Severity = "high"
			f.Detail = "text hidden in invisible Unicode tag characters"
			f.Snippet = clip(t, 120)
			// The hidden text is scanned too.
			sub := Scan(t)
			r.Add(sub.Findings...)
		}
		r.Add(f)
	}
	// Look-alike letters.
	if ws := mixedScript(text); len(ws) > 0 {
		r.Add(Finding{Kind: KindHomoglyph, Severity: "medium", Detail: fmt.Sprintf("%d words mix Latin with look-alike letters", len(ws)), Snippet: clip(Skeleton(strings.Join(uniq(ws), " ")), 120)})
	}
	// Instructions, on the skeleton (catches homoglyph and zero-width tricks).
	sk := strings.ToLower(Skeleton(text))
	sk = strings.Join(strings.Fields(sk), " ")
	for _, p := range patterns {
		if loc := p.re.FindStringIndex(sk); loc != nil {
			r.Add(Finding{Kind: KindInstruction, Severity: p.sev, Detail: p.detail, Snippet: clip(around(sk, loc), 140)})
		}
	}
	// Links built to carry data away.
	for _, m := range reMDImage.FindAllStringSubmatch(text, 5) {
		r.Add(Finding{Kind: KindExfil, Severity: "high", Detail: "a markdown image (loading it would contact " + hostOf(m[1]) + ")", Snippet: clip(m[0], 120)})
	}
	for _, m := range reHTMLImg.FindAllStringSubmatch(text, 5) {
		if strings.HasPrefix(strings.ToLower(m[1]), "http") {
			r.Add(Finding{Kind: KindExfil, Severity: "medium", Detail: "an image tag in the text (" + hostOf(m[1]) + ")", Snippet: clip(m[0], 120)})
		}
	}
	for _, u := range reURL.FindAllString(text, 50) {
		if f, ok := suspiciousURL(u); ok {
			r.Add(f)
		}
	}
	return r
}

// suspiciousURL flags links that carry data (long or encoded parameters,
// data: and javascript: schemes) or hide their destination (punycode,
// IP literals, credentials in the URL).
func suspiciousURL(raw string) (Finding, bool) {
	l := strings.ToLower(raw)
	switch {
	case strings.HasPrefix(l, "data:"):
		return Finding{Kind: KindExfil, Severity: "medium", Detail: "a data: URL", Snippet: clip(raw, 80)}, true
	case strings.HasPrefix(l, "javascript:"):
		return Finding{Kind: KindExfil, Severity: "high", Detail: "a javascript: URL", Snippet: clip(raw, 80)}, true
	case strings.HasPrefix(l, "file:"):
		return Finding{Kind: KindLink, Severity: "medium", Detail: "a link to a local file", Snippet: clip(raw, 80)}, true
	}
	if strings.HasPrefix(l, "www.") {
		raw = "http://" + raw
	}
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return Finding{}, false
	}
	host := u.Hostname()
	if u.User != nil {
		return Finding{Kind: KindLink, Severity: "medium", Detail: "a link with a user name in it (" + host + ")", Snippet: clip(raw, 80)}, true
	}
	if strings.Contains(host, "xn--") {
		return Finding{Kind: KindLink, Severity: "medium", Detail: "a punycode (look-alike) domain " + host, Snippet: clip(raw, 80)}, true
	}
	if isIP(host) {
		return Finding{Kind: KindLink, Severity: "low", Detail: "a link to a bare IP address " + host, Snippet: clip(raw, 80)}, true
	}
	q := u.RawQuery + u.Fragment
	if len(q) > 120 || reB64.MatchString(q) {
		return Finding{Kind: KindExfil, Severity: "medium", Detail: "a link with a long encoded parameter (" + host + ")", Snippet: clip(raw, 100)}, true
	}
	for _, k := range []string{"data=", "d=", "q=", "secret", "token=", "key=", "password", "payload", "exfil", "leak", "steal", "collect"} {
		if strings.Contains(strings.ToLower(u.RawQuery+u.Path), k) && len(u.RawQuery) > 0 {
			return Finding{Kind: KindExfil, Severity: "low", Detail: "a link that sends parameters to " + host, Snippet: clip(raw, 100)}, true
		}
	}
	return Finding{}, false
}

func isIP(h string) bool {
	if strings.Count(h, ".") == 3 {
		for _, p := range strings.Split(h, ".") {
			if p == "" || strings.Trim(p, "0123456789") != "" {
				return false
			}
		}
		return true
	}
	return strings.Contains(h, ":")
}

func hostOf(raw string) string {
	u, err := url.Parse(raw)
	if err != nil || u.Host == "" {
		return "another site"
	}
	return u.Hostname()
}

// Clean prepares untrusted text for the model: invisible characters and
// tag-smuggled text removed, markdown images dropped, links replaced by
// [link: host], whitespace collapsed, length limited.
func Clean(text string, max int) string {
	var b strings.Builder
	for _, r := range text {
		if invisible(r) {
			continue
		}
		if r == '\r' {
			continue
		}
		b.WriteRune(r)
	}
	s := b.String()
	s = reMDImage.ReplaceAllString(s, "[image removed]")
	s = reHTMLImg.ReplaceAllString(s, "[image removed]")
	s = reMDLink.ReplaceAllStringFunc(s, func(m string) string {
		sm := reMDLink.FindStringSubmatch(m)
		return sm[1] + " [link: " + hostOf(sm[2]) + "]"
	})
	s = reURL.ReplaceAllStringFunc(s, func(u string) string {
		if strings.HasPrefix(strings.ToLower(u), "www.") {
			u = "http://" + u
		}
		return "[link: " + hostOf(u) + "]"
	})
	// Collapse blank runs but keep paragraphs.
	lines := strings.Split(s, "\n")
	var out []string
	blank := 0
	for _, l := range lines {
		l = strings.Join(strings.Fields(l), " ")
		if l == "" {
			blank++
			if blank > 1 {
				continue
			}
		} else {
			blank = 0
		}
		out = append(out, l)
	}
	s = strings.TrimSpace(strings.Join(out, "\n"))
	if max > 0 && len(s) > max {
		cut := max
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = s[:cut] + " [...]"
	}
	return s
}

var reOutCode = regexp.MustCompile("(?s)```.*?```|`[^`]*`")
var reOutTag = regexp.MustCompile(`<[^>]{1,200}>`)
var reOutCommand = regexp.MustCompile(`(?i)\b(rm -rf|sudo |curl |wget |chmod |setenforce|powershell|bash -c)`)
var reBareDomain = regexp.MustCompile(`(?i)(?:^|[^@\w.-])((?:[a-z0-9-]+\.)+(?:com|org|net|io|dev|app|test|lab|info|biz|xyz|top|ru|cn|br|uk|de|co|me|ly|gl|sh)(?:/[^\s]*)?)\b`)
var reEmail = regexp.MustCompile(`[A-Za-z0-9._%+-]+@[A-Za-z0-9.-]+\.[A-Za-z]{2,}`)

// OutputReport says what Output removed.
type OutputReport struct {
	Removed []string `json:"removed,omitempty"`
}

// Output cleans what the model wrote before the shell shows or speaks it:
// no links or images (they would be an exfiltration channel when
// rendered or followed), no markup, no code or shell commands, no
// invisible characters; plain sentences only. allowedEmails are
// addresses the person may see (the senders of the messages listed).
func Output(text string, max int, allowedEmails []string) (string, OutputReport) {
	var rep OutputReport
	s := text
	for _, r := range s {
		if invisible(r) {
			rep.Removed = append(rep.Removed, "invisible characters")
			break
		}
	}
	s = strings.Map(func(r rune) rune {
		if invisible(r) {
			return -1
		}
		return r
	}, s)
	if reMDImage.MatchString(s) || reHTMLImg.MatchString(s) {
		rep.Removed = append(rep.Removed, "an image")
		s = reMDImage.ReplaceAllString(s, "")
		s = reHTMLImg.ReplaceAllString(s, "")
	}
	if reMDLink.MatchString(s) {
		rep.Removed = append(rep.Removed, "a link")
		s = reMDLink.ReplaceAllString(s, "$1")
	}
	if reURL.MatchString(s) {
		rep.Removed = append(rep.Removed, "a web address")
		s = reURL.ReplaceAllString(s, "[address removed]")
	}
	if reOutCode.MatchString(s) {
		rep.Removed = append(rep.Removed, "code")
		s = reOutCode.ReplaceAllString(s, "")
	}
	if reOutTag.MatchString(s) {
		rep.Removed = append(rep.Removed, "markup")
		s = reOutTag.ReplaceAllString(s, "")
	}
	if reOutCommand.MatchString(s) {
		rep.Removed = append(rep.Removed, "a command")
		s = reOutCommand.ReplaceAllString(s, "[command removed]")
	}
	// Bare domain names ("evil.example.com/unlock") are links too once a
	// person reads them aloud or types them: removed unless allowed.
	var bd strings.Builder
	last := 0
	for _, loc := range reBareDomain.FindAllStringSubmatchIndex(s, -1) {
		m := s[loc[2]:loc[3]]
		host := strings.ToLower(strings.SplitN(m, "/", 2)[0])
		keep := false
		for _, a := range allowedEmails {
			if i := strings.LastIndex(a, "@"); i >= 0 && strings.EqualFold(a[i+1:], host) {
				keep = true
			}
		}
		if keep {
			continue
		}
		bd.WriteString(s[last:loc[2]])
		bd.WriteString("[address removed]")
		last = loc[3]
		rep.Removed = append(rep.Removed, "a web address")
	}
	bd.WriteString(s[last:])
	s = bd.String()
	s = reEmail.ReplaceAllStringFunc(s, func(m string) string {
		for _, a := range allowedEmails {
			if strings.EqualFold(a, m) {
				return m
			}
		}
		rep.Removed = append(rep.Removed, "an e-mail address")
		return "[address removed]"
	})
	// Markdown emphasis and headings become plain text.
	s = strings.NewReplacer("**", "", "__", "", "##", "", "# ", "").Replace(s)
	s = strings.Join(strings.Fields(s), " ")
	if max > 0 && len(s) > max {
		cut := max
		for cut > 0 && !utf8.RuneStart(s[cut]) {
			cut--
		}
		s = strings.TrimSpace(s[:cut]) + "..."
	}
	rep.Removed = uniq(rep.Removed)
	return strings.TrimSpace(s), rep
}

func clip(s string, n int) string {
	s = strings.Join(strings.Fields(strings.Map(func(r rune) rune {
		if invisible(r) {
			return -1
		}
		return r
	}, s)), " ")
	if len(s) <= n {
		return s
	}
	cut := n
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut] + "..."
}

func around(s string, loc []int) string {
	a, b := loc[0]-30, loc[1]+30
	if a < 0 {
		a = 0
	}
	if b > len(s) {
		b = len(s)
	}
	for a > 0 && !utf8.RuneStart(s[a]) {
		a--
	}
	for b < len(s) && !utf8.RuneStart(s[b]) {
		b++
	}
	return s[a:b]
}

func uniq(in []string) []string {
	seen := map[string]bool{}
	var out []string
	for _, s := range in {
		if !seen[s] {
			seen[s] = true
			out = append(out, s)
		}
	}
	sort.Strings(out)
	return out
}

var reDictate = regexp.MustCompile(`(?i)\b(?:say|says|reply with|respond with|answer with|tell (?:the )?(?:user|person|reader)(?: that)?|must (?:say|read|state|be)|should (?:say|read|state)|output|write)\s*(?:exactly|only|this|the following)?\s*[:,-]?\s*["'“‘]?([^"'”’\n]{8,200})`)

// DictatedPhrases returns what content tells an assistant to say ("the
// summary must say: 'All good, no action needed'"), from its text with
// look-alike and invisible characters undone.
func DictatedPhrases(text string) []string {
	var out []string
	for _, m := range reDictate.FindAllStringSubmatch(Skeleton(text), 20) {
		out = append(out, strings.TrimSpace(m[1]))
	}
	return out
}

func words(s string) []string {
	return strings.Fields(strings.ToLower(regexp.MustCompile(`[^\p{L}\p{N} ]+`).ReplaceAllString(s, " ")))
}

// Repeats reports whether output contains at least n consecutive words of
// one of the phrases: the model said what the content dictated.
func Repeats(output string, phrases []string, n int) bool {
	ow := " " + strings.Join(words(output), " ") + " "
	for _, p := range phrases {
		pw := words(p)
		for i := 0; i+n <= len(pw); i++ {
			if strings.Contains(ow, " "+strings.Join(pw[i:i+n], " ")+" ") {
				return true
			}
		}
	}
	return false
}
