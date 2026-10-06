package skills

import (
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/basalt-os/basalt-shell/internal/i18n"
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
	reOpen     = regexp.MustCompile(`(?i)^\s*(please\s+|por favor,?\s+)?(open|show|abre|abrir|abra|mostre|mostra)\s+(the\s+|o\s+|a\s+)?(result|file|item|resultado|arquivo|number|n[uú]mero)?\s*(number\s+|n[uú]mero\s+)?(\d+|one|two|three|four|five|six|seven|eight|first|second|third|fourth|fifth|last|it|um|dois|tr[eê]s|primeiro|segundo|terceiro|[uú]ltimo)(?:$|[^\p{L}\p{N}])`)
	reDuration = regexp.MustCompile(`(?i)(?:^|\s)(?:for|por|durante)\s+(an?|one|two|three|four|five|ten|fifteen|thirty|um|uma|dois|duas|tr[eê]s|cinco|dez|quinze|trinta|meia|\d+)\s+(seconds?|minutes?|hours?|days?|segundos?|minutos?|horas?|dias?)(?:$|[^\p{L}])`)
	reMonths   = regexp.MustCompile(`(?i)\b(january|february|march|april|may|june|july|august|september|october|november|december)\b`)
	// Month names in Portuguese, to the English ones TimeRange reads.
	reMonthsPT = regexp.MustCompile(`(?i)(?:^|[^\p{L}])(janeiro|fevereiro|mar[cç]o|abril|maio|junho|julho|agosto|setembro|outubro|novembro|dezembro)(?:$|[^\p{L}])`)
)

var monthsPT = map[string]string{"janeiro": "january", "fevereiro": "february", "março": "march", "marco": "march", "abril": "april",
	"maio": "may", "junho": "june", "julho": "july", "agosto": "august", "setembro": "september", "outubro": "october",
	"novembro": "november", "dezembro": "december"}

var numberWord = map[string]int{"one": 1, "a": 1, "an": 1, "two": 2, "three": 3, "four": 4, "five": 5, "six": 6, "seven": 7, "eight": 8,
	"ten": 10, "fifteen": 15, "thirty": 30, "first": 1, "second": 2, "third": 3, "fourth": 4, "fifth": 5, "um": 1, "uma": 1, "dois": 2, "duas": 2,
	"três": 3, "tres": 3, "cinco": 5, "dez": 10, "quinze": 15, "trinta": 30, "primeiro": 1, "segundo": 2, "terceiro": 3, "it": 1}

var (
	wordRe   = map[string]*regexp.Regexp{}
	wordReMu sync.Mutex
)

// has reports whether any of the words (or phrases) is in t as whole
// words. Word edges are Unicode letters and digits, not only ASCII
// (RE2's \b is ASCII only: "você" or "até" never ended a word).
func has(t string, words ...string) bool {
	for _, w := range words {
		wordReMu.Lock()
		re, ok := wordRe[w]
		if !ok {
			re = regexp.MustCompile(`(?:^|[^\p{L}\p{N}_])` + regexp.QuoteMeta(w) + `(?:$|[^\p{L}\p{N}_])`)
			wordRe[w] = re
		}
		wordReMu.Unlock()
		if re.MatchString(t) {
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
	if has(t, "revoke", "forget my permissions", "stop access", "remove access", "end access", "cancel access", "revogar", "revogue", "revoga",
		"remova o acesso", "remover o acesso", "retire o acesso", "tirar o acesso", "cancele o acesso", "cancelar o acesso") {
		return Route{Skill: SkillRevoke}
	}
	if r, ok := classifyAct(text); ok {
		return r
	}
	if has(t, "allow", "grant", "give access", "permitir", "autorizar", "let the assistant", "permita", "autorize", "permite", "autoriza",
		"dê acesso", "de acesso", "dar acesso", "libere o acesso", "liberar o acesso") && !has(t, "allowance") {
		r.Skill = SkillGrant
		r.Duration = DefaultGrant
		if m := reDuration.FindStringSubmatch(t); m != nil {
			n, err := strconv.Atoi(m[1])
			if err != nil {
				n = numberWord[strings.ToLower(m[1])]
			}
			unit := map[byte]time.Duration{'s': time.Second, 'm': time.Minute, 'h': time.Hour, 'd': 24 * time.Hour}[strings.ToLower(m[2])[0]]
			r.Duration = time.Duration(n) * unit
			if strings.EqualFold(m[1], "meia") && unit == time.Hour {
				r.Duration = 30 * time.Minute // "por meia hora"
			}
		}
		switch {
		case has(t, "mail", "email", "emails", "inbox", "mailbox", "caixa de entrada", "correio", "mensagens"):
			r.Kind = GrantMailbox
		case reHost.MatchString(t) || has(t, "site", "website", "page", "web", "página", "pagina"):
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
			// The same folders named in Portuguese (the folders themselves
			// keep their English names on disk).
			for pt, f := range map[string]string{"documentos": "documents", "área de trabalho": "desktop", "area de trabalho": "desktop",
				"imagens": "pictures", "fotos": "pictures", "músicas": "music", "musicas": "music", "vídeos": "videos", "pasta pessoal": "home"} {
				if has(t, pt) && !has(t, f) {
					r.Targets = append(r.Targets, f)
				}
			}
		}
		return r
	}
	// Words inside an address ("/a07-email.html") do not route: only the
	// request's own words.
	plain := reHost.ReplaceAllString(t, " ")
	mail := has(plain, "email", "emails", "mail", "inbox", "mailbox", "unread", "newsletter", "messages from", "message from", "wrote me", "wrote to me",
		// Brazilian Portuguese
		"caixa de entrada", "mensagem", "mensagens", "não lidos", "não lidas", "nao lidos", "me escreveu", "me mandou um")
	web := has(t, "page", "website", "site", "web page", "webpage", "article", "url", "link", "headline", "headlines", "blog",
		"página", "pagina", "artigo", "notícia", "notícias", "noticias", "manchete", "manchetes") || reHost.MatchString(t)
	files := has(t, "file", "files", "pdf", "pdfs", "document", "documents", "folder", "spreadsheet", "spreadsheets", "scan", "photo",
		"picture", "contract", "receipt", "invoice", "statement", "slides", "presentation", "docx", "notes", "downloads", "report",
		"arquivo", "arquivos", "documento", "documentos", "pasta", "planilha", "planilhas", "foto", "fotos", "contrato", "recibo",
		"nota fiscal", "fatura", "extrato", "comprovante", "boleto", "apresentação", "anotações", "relatório", "digitalização")
	find := has(t, "find", "where is", "where's", "look for", "search", "locate", "show me", "which file", "do i have", "get me",
		"encontre", "encontrar", "encontra", "ache", "achar", "acha", "procure", "procurar", "procura", "busque", "buscar", "busca",
		"onde está", "onde esta", "cadê", "cade", "me mostre", "me mostra", "localize", "qual arquivo", "eu tenho")
	if has(t, "saved page", "saved web page", "saved pages", "página salva", "páginas salvas") {
		web, files = false, true
	}
	switch {
	case mail && !strings.Contains(t, "pdf"):
		r.Skill = SkillMail
	case web && !files:
		r.Skill = SkillWeb
	case files && (find || has(t, "pdf", "spreadsheet", "contract", "receipt", "invoice", "statement", "scan",
		"planilha", "contrato", "recibo", "fatura", "extrato", "comprovante", "boleto", "nota fiscal")):
		r.Skill = SkillFiles
	case web:
		r.Skill = SkillWeb
	case find && has(t, "my", "the file", "a file", "meu", "minha", "meus", "minhas", "o arquivo", "um arquivo"):
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
	case has(t, "this week", "esta semana", "essa semana", "nesta semana", "nessa semana"):
		return weekStart, time.Time{}, i18n.G("this week")
	case has(t, "last month", "mês passado", "mes passado"):
		return monthStart.AddDate(0, -1, 0), monthStart, i18n.G("last month")
	case has(t, "this month", "este mês", "esse mês", "neste mês", "nesse mês", "este mes", "neste mes"):
		return monthStart, time.Time{}, i18n.G("this month")
	case has(t, "last year", "ano passado"):
		return yearStart.AddDate(-1, 0, 0), yearStart, i18n.G("last year")
	case has(t, "this year", "este ano", "neste ano", "esse ano"):
		return yearStart, time.Time{}, i18n.G("this year")
	case has(t, "recent", "recently", "latest", "last few days", "recente", "recentes", "recentemente", "últimos dias", "ultimos dias"):
		return day.AddDate(0, 0, -14), time.Time{}, i18n.G("the last two weeks")
	}
	if m := reMonthsPT.FindStringSubmatch(t); m != nil && !reMonths.MatchString(t) {
		if en, ok := monthsPT[m[1]]; ok {
			t = strings.Replace(t, m[1], en, 1)
		}
	}
	if m := reMonths.FindString(t); m != "" {
		mon, _ := time.Parse("January", strings.Title(m))
		y := now.Year()
		if mon.Month() > now.Month() {
			y--
		}
		a := time.Date(y, mon.Month(), 1, 0, 0, 0, 0, now.Location())
		return a, a.AddDate(0, 1, 0), i18n.G("%s %d", i18n.Month(mon.Month()), y)
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
	if has(t, "slides", "presentation", "deck", "apresentação", "apresentacao") {
		add("presentation")
	}
	if has(t, "word document", "docx", "letter", "carta", "documento do word") {
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
		spreadsheet spreadsheets photo picture scan me us up out into than then so just also
		o a os as um uma uns umas de do da dos das no na nos nas em por para com que qual quais quem onde quando como e ou
		meu minha meus minhas seu sua seus suas eu você voce me mim ele ela eles elas isso isto esse essa este esta aquele aquela
		encontre encontrar encontra ache achar acha procure procurar procura busque buscar busca mostre mostra cadê cade localize
		arquivo arquivos documento documentos pasta pastas mês mes passado passada semana ano hoje ontem último última ultimo ultima
		mandou enviou recebi mandaram enviaram tenho tem sobre algum alguma página pagina site resuma resumo leia ler abra abre
		email emails mensagem mensagens caixa entrada planilha planilhas foto fotos favor`) {
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

// crossWords are English equivalents of common Portuguese words about
// documents and mail, added to a search (deterministic, no model): file
// names and many documents are English even for a person who speaks
// Portuguese. The lab's small model did not add them reliably.
var crossWords = map[string][]string{
	"banco": {"bank"}, "extrato": {"statement"}, "fatura": {"invoice"}, "faturas": {"invoice"}, "recibo": {"receipt"},
	"recibos": {"receipt"}, "comprovante": {"receipt"}, "contrato": {"contract"}, "contratos": {"contract"},
	"planilha": {"spreadsheet"}, "orçamento": {"budget"}, "viagem": {"travel", "trip"}, "passaporte": {"passport"},
	"aluguel": {"rent"}, "senhorio": {"landlord"}, "proprietário": {"landlord"}, "luz": {"electricity"}, "energia": {"electricity"},
	"relatório": {"report"}, "relatórios": {"report"}, "anotações": {"notes"}, "notas": {"notes"}, "reunião": {"meeting"},
	"almoço": {"lunch"}, "dentista": {"dentist"}, "consulta": {"appointment"}, "voo": {"flight"}, "reserva": {"booking", "reservation"},
	"seguro": {"insurance"}, "imposto": {"tax"}, "impostos": {"tax"}, "salário": {"salary"}, "currículo": {"resume", "cv"},
	"apresentação": {"presentation", "slides"}, "foto": {"photo"}, "fotos": {"photo"}, "cartão": {"card"}, "conta": {"bill", "account"},
	"boleto": {"bill"}, "nota fiscal": {"invoice"}, "atrasada": {"overdue"}, "atrasado": {"overdue"},
}

// CrossWords returns the English equivalents of the Portuguese words.
func CrossWords(words []string) []string {
	var out []string
	seen := map[string]bool{}
	for _, w := range words {
		seen[w] = true
	}
	for _, w := range words {
		for _, e := range crossWords[strings.ToLower(w)] {
			if !seen[e] {
				seen[e] = true
				out = append(out, e)
			}
		}
	}
	return out
}

var (
	reReply  = regexp.MustCompile(`(?i)^\s*(?:please\s+|can you\s+|could you\s+)?(?:reply|replied|respond|answer|write back|responda|responder)\b\s*(?:to\s+)?(.*)$`)
	reMove   = regexp.MustCompile(`(?i)^\s*(?:please\s+)?(?:move|put|file)\s+(.+?)\s+(?:to|into|in)\s+(?:the\s+|a\s+|my\s+)?(?:folder\s+(?:called\s+|named\s+)?)?(.+?)(?:\s+folder)?\s*[.!]?\s*$`)
	reRename = regexp.MustCompile(`(?i)^\s*(?:please\s+)?rename\s+(.+?)\s+(?:to|as)\s+(.+?)\s*[.!]?\s*$`)
	reUndo   = regexp.MustCompile(`(?i)^\s*(?:please\s+)?(?:undo|put (?:them|it|the files?) back|revert|desfazer|desfa[cç]a)\b`)
	reSend   = regexp.MustCompile(`(?i)^\s*(?:please\s+|por favor,?\s+)?(?:send|forward|envie|enviar|encaminhe|encaminhar)\b`)
	reDelete = regexp.MustCompile(`(?i)^\s*(?:please\s+|por favor,?\s+)?(?:delete|remove|erase|trash|wipe|apague|apagar|exclua|excluir|deletar|remova|remover)\b`)
	// Where the reply's text starts: "reply to Ana: ...", "... saying ...".
	reNameFirst = regexp.MustCompile(`^([\p{Lu}][\p{L}'-]+)[,]?\s+(\S.*)$`)
	reReplySep  = regexp.MustCompile(`(?i)\s*(?::|\s+saying\s+|\s+and say\s+|,\s*say\s+|\s+and tell (?:her|him|them)\s+|\s+tell (?:her|him|them)\s+|\s+to say\s+|\s+with\s+)`)
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
		} else if n := reNameFirst.FindStringSubmatch(m[1]); n != nil && !stopWords[strings.ToLower(n[1])] {
			// Spoken, the colon is lost: "reply to Priya the slides are
			// ready". A capitalized first name, then the text.
			r.Select, r.Rest = n[1], strings.TrimSpace(n[2])
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
	case reDelete.MatchString(t) && !has(strings.ToLower(t), "permission", "permissions", "access", "permissão", "permissões", "acesso"):
		return Route{Skill: SkillUnsupported, Asked: "delete"}, true
	}
	return Route{}, false
}
