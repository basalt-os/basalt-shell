package skills

import (
	"regexp"

	"github.com/basalt-os/basalt-shell/internal/i18n"
	"strconv"
	"strings"
	"time"
)

// Skill names.
const (
	SkillFiles  = "files"
	SkillMail   = "mail"
	SkillWeb    = "web"
	SkillOpen   = "open"   // open a result of the last file search
	SkillGrant  = "grant"  // the person grants a scope
	SkillRevoke = "revoke" // the person ends grants
	// Acting skills: always a typed action, previewed and confirmed.
	SkillReply  = "reply"  // draft a reply to a message, sent after confirmation
	SkillMove   = "move"   // move files inside a granted folder
	SkillRename = "rename" // rename a file inside a granted folder
	SkillUndo   = "undo"   // put the last moved or renamed files back
	// Requests for actions the assistant does not do (answered, never acted on).
	SkillUnsupported = "unsupported"
)

// Route is how a request was understood by the fixed rules.
type Route struct {
	Skill    string
	N        int    // open: result number (1-based)
	Kind     string // grant: folder, mailbox, site
	Targets  []string
	Duration time.Duration
	Host     string // web: a host or URL written in the request
	URL      string
	// Acting skills: what to act on (the request's words before "to",
	// "saying", ":") and the rest (the reply's text, the destination).
	Select string
	Rest   string
	// Unsupported: which action was asked for.
	Asked string
}

var (
	reHost     = regexp.MustCompile(`(?i)\b((?:https?://)?((?:[a-z0-9-]+\.)+(?:test|lab|org|com|net|dev|io|br|info|gov|edu|uk|de|internal|lan))(/[^\s]*)?)\b`)
	reOpen     = regexp.MustCompile(`(?i)^\s*(please\s+)?(open|show|abre|abrir)\s+(the\s+|o\s+|a\s+)?(result|file|item|resultado|arquivo|number|n[uú]mero)?\s*(number\s+|n[uú]mero\s+)?(\d+|one|two|three|four|five|six|seven|eight|first|second|third|fourth|fifth|last|it|um|dois|tr[eê]s|primeiro|segundo|terceiro)\b`)
	reDuration = regexp.MustCompile(`(?i)\bfor\s+(an?|one|two|three|four|five|ten|fifteen|thirty|\d+)\s+(seconds?|minutes?|hours?|days?)\b`)
	reMonths   = regexp.MustCompile(`(?i)\b(january|february|march|april|may|june|july|august|september|october|november|december)\b`)
)

var numberWord = map[string]int{"one": 1, "a": 1, "an": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8,
	"ten": 10, "fifteen": 15, "thirty": 30, "first": 1, "second": 2, "third": 3, "fourth": 4, "fifth": 5, "um": 1, "dois": 2, "três": 3, "tres": 3,
	"primeiro": 1, "segundo": 2, "terceiro": 3, "it": 1}

func has(t string, words ...string) bool {
	for _, w := range words {
		if regexp.MustCompile(`\b` + regexp.QuoteMeta(w) + `\b`).MatchString(t) {
			return true
		}
	}
	return false
}

// Classify routes a request to a skill with fixed rules, or returns ""
// (not a skill request: the desktop's own command bar handles it).
func Classify(text string) Route {
	t := strings.ToLower(strings.TrimSpace(text))
	t = strings.ReplaceAll(t, "e-mail", "email")
	var r Route
	if m := reOpen.FindStringSubmatch(t); m != nil && !has(t, "editor", "terminal", "settings", "browser", "firefox", "app") {
		n, err := strconv.Atoi(m[6])
		if err != nil {
			n = numberWord[m[6]]
		}
		if m[6] == "last" {
			n = -1
		}
		if n != 0 {
			return Route{Skill: SkillOpen, N: n}
		}
	}
	if has(t, "revoke", "forget my permissions", "stop access", "remove access", "end access", "cancel access", "revogar") {
		return Route{Skill: SkillRevoke}
	}
	if r, ok := classifyAct(text); ok {
		return r
	}
	if has(t, "allow", "grant", "give access", "permitir", "autorizar", "let the assistant") && !has(t, "allowance") {
		r.Skill = SkillGrant
		r.Duration = DefaultGrant
		if m := reDuration.FindStringSubmatch(t); m != nil {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				n = numberWord[m[1]]
			}
			unit := map[byte]time.Duration{'s': time.Second, 'm': time.Minute, 'h': time.Hour, 'd': 24 * time.Hour}[m[2][0]]
			r.Duration = time.Duration(n) * unit
		}
		switch {
		case has(t, "mail", "email", "emails", "inbox", "mailbox", "caixa de entrada"):
			r.Kind = GrantMailbox
		case reHost.MatchString(t) || has(t, "site", "website", "page", "web"):
			r.Kind = GrantSite
			if m := reHost.FindStringSubmatch(t); m != nil {
				r.Host = strings.ToLower(m[2])
			}
		default:
			r.Kind = GrantFolder
			for _, f := range []string{"documents", "downloads", "desktop", "pictures", "music", "videos", "home"} {
				if has(t, f) {
					r.Targets = append(r.Targets, f)
				}
			}
		}
		return r
	}
	mail := has(t, "email", "emails", "mail", "inbox", "mailbox", "unread", "newsletter", "messages from", "message from", "wrote me", "wrote to me")
	web := has(t, "page", "website", "site", "web page", "webpage", "article", "url", "link", "headline", "headlines", "blog") || reHost.MatchString(t)
	files := has(t, "file", "files", "pdf", "pdfs", "document", "documents", "folder", "spreadsheet", "spreadsheets", "scan", "photo",
		"picture", "contract", "receipt", "invoice", "statement", "slides", "presentation", "docx", "notes", "downloads", "report")
	find := has(t, "find", "where is", "where's", "look for", "search", "locate", "show me", "which file", "do i have", "get me")
	if has(t, "saved page", "saved web page", "saved pages") {
		web, files = false, true
	}
	switch {
	case mail && !strings.Contains(t, "pdf"):
		r.Skill = SkillMail
	case web && !files:
		r.Skill = SkillWeb
	case files && (find || has(t, "pdf", "spreadsheet", "contract", "receipt", "invoice", "statement", "scan")):
		r.Skill = SkillFiles
	case web:
		r.Skill = SkillWeb
	case find && has(t, "my", "the file", "a file"):
		// "find my travel itinerary": looking for something of the
		// person's, with no mail or web word: their files.
		r.Skill = SkillFiles
	}
	if r.Skill == SkillWeb {
		if m := reHost.FindStringSubmatch(t); m != nil {
			r.Host = strings.ToLower(m[2])
			if strings.HasPrefix(m[1], "http") {
				r.URL = m[1]
			}
		}
	}
	return r
}

// TimeRange reads a time phrase of the request: today, yesterday, this
// or last week, month or year, a month name ("in March": the last March
// that has begun), "recent" (two weeks). Zero times: no limit.
func TimeRange(text string, now time.Time) (after, before time.Time, phrase string) {
	t := strings.ToLower(text)
	day := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location())
	weekStart := day.AddDate(0, 0, -((int(day.Weekday()) + 6) % 7))
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, now.Location())
	yearStart := time.Date(now.Year(), 1, 1, 0, 0, 0, 0, now.Location())
	switch {
	case has(t, "today", "hoje"):
		return day, time.Time{}, i18n.G("today")
	case has(t, "yesterday", "ontem"):
		return day.AddDate(0, 0, -1), day, i18n.G("yesterday")
	case has(t, "last week", "semana passada"):
		return weekStart.AddDate(0, 0, -7), weekStart, i18n.G("last week")
	case has(t, "this week", "esta semana", "essa semana"):
		return weekStart, time.Time{}, i18n.G("this week")
	case has(t, "last month", "mês passado", "mes passado"):
		return monthStart.AddDate(0, -1, 0), monthStart, i18n.G("last month")
	case has(t, "this month", "este mês", "esse mês"):
		return monthStart, time.Time{}, i18n.G("this month")
	case has(t, "last year", "ano passado"):
		return yearStart.AddDate(-1, 0, 0), yearStart, i18n.G("last year")
	case has(t, "this year", "este ano"):
		return yearStart, time.Time{}, i18n.G("this year")
	case has(t, "recent", "recently", "latest", "last few days"):
		return day.AddDate(0, 0, -14), time.Time{}, i18n.G("the last two weeks")
	}
	if m := reMonths.FindString(t); m != "" {
		mon, _ := time.Parse("January", strings.Title(m))
		y := now.Year()
		if mon.Month() > now.Month() {
			y--
		}
		a := time.Date(y, mon.Month(), 1, 0, 0, 0, 0, now.Location())
		return a, a.AddDate(0, 1, 0), i18n.G("%s %d", m, y)
	}
	return time.Time{}, time.Time{}, ""
}

// KindsIn reads file kinds named in a request.
func KindsIn(text string) []string {
	t := strings.ToLower(text)
	var out []string
	add := func(k string) {
		for _, x := range out {
			if x == k {
				return
			}
		}
		out = append(out, k)
	}
	if has(t, "pdf", "pdfs") {
		add("pdf")
	}
	if has(t, "spreadsheet", "spreadsheets", "sheet", "excel", "csv", "planilha") {
		add("spreadsheet")
	}
	if has(t, "photo", "photos", "picture", "pictures", "image", "images", "scan", "screenshot", "foto") {
		add("image")
		if has(t, "scan") {
			add("pdf")
		}
	}
	if has(t, "slides", "presentation", "deck") {
		add("presentation")
	}
	if has(t, "word document", "docx", "letter") {
		add("document")
	}
	return out
}

var stopWords = map[string]bool{}

func init() {
	for _, w := range strings.Fields(`a an the of to from my me for and or in on at by with that this these those it its is are was were be
		find show where what which who whom whose when how do does did i you he she we they them his her their our your
		file files document documents folder folders pdf pdfs please can could would will last month week year today yesterday
		sent send got get look search locate open any some all about there here have has had mail email emails inbox message
		messages say said tell summarize summary read page site website web new latest recent this that since ago one two
		spreadsheet spreadsheets photo picture scan me us up out into than then so just also`) {
		stopWords[w] = true
	}
}

// ContentWords are the request's words that can match content: not
// function words, not the words that only route the request.
func ContentWords(text string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range regexp.MustCompile(`[\p{L}\p{N}]+`).FindAllString(strings.ToLower(text), -1) {
		if len([]rune(w)) < 3 || stopWords[w] || seen[w] || reMonths.MatchString(w) {
			continue
		}
		seen[w] = true
		out = append(out, w)
	}
	return out
}

var (
	reReply  = regexp.MustCompile(`(?i)^\s*(?:please\s+|can you\s+|could you\s+)?(?:reply|respond|answer|write back|responda|responder)\b\s*(?:to\s+)?(.*)$`)
	reMove   = regexp.MustCompile(`(?i)^\s*(?:please\s+)?(?:move|put|file)\s+(.+?)\s+(?:to|into|in)\s+(?:the\s+|a\s+|my\s+)?(?:folder\s+(?:called\s+|named\s+)?)?(.+?)(?:\s+folder)?\s*[.!]?\s*$`)
	reRename = regexp.MustCompile(`(?i)^\s*(?:please\s+)?rename\s+(.+?)\s+(?:to|as)\s+(.+?)\s*[.!]?\s*$`)
	reUndo   = regexp.MustCompile(`(?i)^\s*(?:please\s+)?(?:undo|put (?:them|it|the files?) back|revert|desfazer|desfa[cç]a)\b`)
	reSend   = regexp.MustCompile(`(?i)^\s*(?:please\s+)?(?:send|forward)\b`)
	reDelete = regexp.MustCompile(`(?i)^\s*(?:please\s+)?(?:delete|remove|erase|trash|wipe)\b`)
	// Where the reply's text starts: "reply to Ana: ...", "... saying ...".
	reReplySep = regexp.MustCompile(`(?i)\s*(?::|\s+saying\s+|\s+and say\s+|,\s*say\s+|\s+and tell (?:her|him|them)\s+|\s+tell (?:her|him|them)\s+|\s+to say\s+|\s+with\s+)`)
)

// classifyAct recognizes the acting requests. They are recognized only
// at the start of the person's words: the content the skills read never
// goes through here.
func classifyAct(text string) (Route, bool) {
	t := strings.TrimSpace(text)
	switch {
	case reUndo.MatchString(t):
		return Route{Skill: SkillUndo}, true
	case reReply.MatchString(t):
		m := reReply.FindStringSubmatch(t)
		r := Route{Skill: SkillReply}
		if loc := reReplySep.FindStringIndex(m[1]); loc != nil {
			r.Select, r.Rest = strings.TrimSpace(m[1][:loc[0]]), strings.TrimSpace(m[1][loc[1]:])
		} else {
			r.Select = strings.TrimSpace(m[1])
		}
		return r, true
	case reRename.MatchString(t):
		m := reRename.FindStringSubmatch(t)
		return Route{Skill: SkillRename, Select: m[1], Rest: strings.Trim(m[2], "\"'"), Targets: nil}, true
	case reMove.MatchString(t):
		m := reMove.FindStringSubmatch(t)
		return Route{Skill: SkillMove, Select: m[1], Rest: strings.Trim(m[2], "\"'")}, true
	case reSend.MatchString(t):
		return Route{Skill: SkillUnsupported, Asked: "send"}, true
	case reDelete.MatchString(t) && !has(strings.ToLower(t), "permission", "permissions", "access"):
		return Route{Skill: SkillUnsupported, Asked: "delete"}, true
	}
	return Route{}, false
}
