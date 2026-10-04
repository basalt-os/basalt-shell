package guard

import (
	"html"
	"net/url"
	"regexp"
	"strings"
)

// HTMLText is what a person would read in an HTML document (an e-mail's
// HTML part, an .html file), and what they would not.
type HTMLText struct {
	Title   string   `json:"title,omitempty"`
	Visible string   `json:"visible"`
	Hidden  string   `json:"hidden,omitempty"` // styled away, comments, noscript
	Links   []Link   `json:"links,omitempty"`
	Images  []string `json:"images,omitempty"` // hosts of remote images
	Report  Report   `json:"report"`
}

// Link is an anchor: its text and where it really goes.
type Link struct {
	Text string `json:"text"`
	Host string `json:"host"`
}

var reStyleHidden = []*regexp.Regexp{
	regexp.MustCompile(`display\s*:\s*none`),
	regexp.MustCompile(`visibility\s*:\s*(hidden|collapse)`),
	regexp.MustCompile(`opacity\s*:\s*0(\.0+)?\s*(;|$|!)`),
	regexp.MustCompile(`font-size\s*:\s*(0|0?\.\d+|1)(px|pt|em|rem)?\s*(;|$|!)`),
	regexp.MustCompile(`(max-)?height\s*:\s*0(px)?\s*(;|$|!)`),
	regexp.MustCompile(`(max-)?width\s*:\s*0(px)?\s*(;|$|!)`),
	regexp.MustCompile(`(left|top|text-indent|margin-left)\s*:\s*-\d{3,}`),
	regexp.MustCompile(`clip\s*:\s*rect\(\s*0`),
	regexp.MustCompile(`color\s*:\s*(#fff\b|#ffffff\b|white\b|rgba?\(\s*255\s*,\s*255\s*,\s*255|transparent)`),
}

var reAttr = regexp.MustCompile(`([a-zA-Z_:][-a-zA-Z0-9_:.]*)\s*(?:=\s*(?:"([^"]*)"|'([^']*)'|([^\s"'>]+)))?`)

var blockTags = map[string]bool{"p": true, "div": true, "br": true, "li": true, "tr": true, "h1": true, "h2": true, "h3": true,
	"h4": true, "h5": true, "h6": true, "table": true, "section": true, "article": true, "header": true, "footer": true,
	"blockquote": true, "pre": true, "ul": true, "ol": true, "hr": true, "td": false}

var voidTags = map[string]bool{"br": true, "img": true, "hr": true, "meta": true, "link": true, "input": true, "area": true,
	"base": true, "col": true, "embed": true, "source": true, "track": true, "wbr": true}

// ParseHTML extracts the visible and hidden text of an HTML document
// with a tolerant tokenizer (the standard library has no HTML parser).
// Hidden means: inside script, style, template or noscript, in a
// comment, or under an element whose inline style or attributes hide it
// (display none, zero size, white text, off screen, the hidden attribute,
// aria-hidden). Styles from style sheets are not evaluated here; the
// browser skill uses the browser's computed styles instead.
func ParseHTML(doc string) HTMLText {
	var out HTMLText
	var vis, hid strings.Builder
	type frame struct {
		tag    string
		hidden bool
		href   string
		text   strings.Builder
	}
	var stack []*frame
	hiddenNow := func() bool {
		for _, f := range stack {
			if f.hidden {
				return true
			}
		}
		return false
	}
	inTitle := false
	i := 0
	n := len(doc)
	for i < n {
		lt := strings.IndexByte(doc[i:], '<')
		if lt < 0 {
			lt = n - i
		}
		if lt > 0 {
			txt := html.UnescapeString(doc[i : i+lt])
			if inTitle {
				out.Title += txt
			} else if hiddenNow() {
				hid.WriteString(txt)
				hid.WriteByte(' ')
			} else {
				vis.WriteString(txt)
			}
			for _, f := range stack {
				if f.tag == "a" {
					f.text.WriteString(txt)
				}
			}
			i += lt
			continue
		}
		// At '<'.
		if strings.HasPrefix(doc[i:], "<!--") {
			end := strings.Index(doc[i+4:], "-->")
			if end < 0 {
				end = n - i - 4
			}
			c := strings.TrimSpace(doc[i+4 : i+4+end])
			if c != "" {
				hid.WriteString(c)
				hid.WriteByte('\n')
			}
			i += 4 + end + 3
			continue
		}
		gt := strings.IndexByte(doc[i:], '>')
		if gt < 0 {
			break
		}
		raw := doc[i+1 : i+gt]
		i += gt + 1
		if raw == "" || raw[0] == '!' || raw[0] == '?' {
			continue
		}
		closing := raw[0] == '/'
		if closing {
			raw = raw[1:]
		}
		selfClose := strings.HasSuffix(raw, "/")
		raw = strings.TrimSuffix(raw, "/")
		name := raw
		attrs := ""
		if k := strings.IndexAny(raw, " \t\n\r"); k >= 0 {
			name, attrs = raw[:k], raw[k:]
		}
		name = strings.ToLower(name)
		if closing {
			if name == "title" {
				inTitle = false
			}
			// Pop to the matching tag (tolerant of missing closes).
			for k := len(stack) - 1; k >= 0; k-- {
				if stack[k].tag == name {
					f := stack[k]
					if name == "a" && f.href != "" {
						t := strings.Join(strings.Fields(f.text.String()), " ")
						out.Links = append(out.Links, Link{Text: clip(t, 80), Host: hostOf(f.href)})
						if !f.hidden && !hiddenNow() {
							vis.WriteString(" [link: " + hostOf(f.href) + "]")
						}
						CheckLinkText(&out.Report, t, f.href)
					}
					stack = stack[:k]
					break
				}
			}
			if blockTags[name] {
				vis.WriteByte('\n')
			}
			continue
		}
		at := map[string]string{}
		for _, m := range reAttr.FindAllStringSubmatch(attrs, -1) {
			v := m[2] + m[3] + m[4]
			at[strings.ToLower(m[1])] = html.UnescapeString(v)
		}
		if blockTags[name] {
			vis.WriteByte('\n')
		}
		switch name {
		case "script", "style", "template", "noscript", "textarea":
			// Raw text elements: skip to the closing tag. Script and style
			// are not text; noscript and template are hidden text.
			end := strings.Index(strings.ToLower(doc[i:]), "</"+name)
			if end < 0 {
				end = n - i
			}
			if name == "noscript" || name == "template" {
				t := html.UnescapeString(stripTags(doc[i : i+end]))
				if strings.TrimSpace(t) != "" {
					hid.WriteString(t)
					hid.WriteByte('\n')
				}
			}
			i += end
			if k := strings.IndexByte(doc[i:], '>'); k >= 0 {
				i += k + 1
			}
			continue
		case "title":
			inTitle = true
			continue
		case "img":
			src := at["src"]
			if strings.HasPrefix(strings.ToLower(src), "http") {
				h := hostOf(src)
				out.Images = append(out.Images, h)
				w, hgt := at["width"], at["height"]
				if (w == "1" || w == "0") && (hgt == "1" || hgt == "0") || hiddenStyle(at["style"]) {
					out.Report.Add(Finding{Kind: KindExfil, Severity: "medium", Detail: "a hidden tracking image (" + h + ")", Snippet: clip(src, 100)})
				} else if f, ok := suspiciousURL(src); ok {
					out.Report.Add(f)
				}
			}
			if alt := at["alt"]; alt != "" && !hiddenNow() {
				vis.WriteString(" " + alt + " ")
			}
			continue
		case "meta":
			if strings.EqualFold(at["http-equiv"], "refresh") {
				out.Report.Add(Finding{Kind: KindLink, Severity: "medium", Detail: "the page redirects by itself", Snippet: clip(at["content"], 100)})
			}
			continue
		}
		if voidTags[name] || selfClose {
			continue
		}
		f := &frame{tag: name}
		_, hasHidden := at["hidden"]
		f.hidden = hasHidden || strings.EqualFold(at["aria-hidden"], "true") || hiddenStyle(at["style"])
		if name == "a" {
			f.href = at["href"]
		}
		stack = append(stack, f)
		if len(stack) > 512 {
			stack = stack[1:]
		}
	}
	out.Title = strings.Join(strings.Fields(out.Title), " ")
	out.Visible = normalizeSpace(vis.String())
	out.Hidden = normalizeSpace(hid.String())
	if out.Hidden != "" {
		sub := Scan(out.Hidden)
		sev := "low"
		if sub.Suspicious() {
			sev = "high"
		}
		out.Report.Add(Finding{Kind: KindHidden, Severity: sev, Detail: "text a reader would not see", Snippet: clip(out.Hidden, 140)})
		out.Report.Add(sub.Findings...)
	}
	out.Report.Add(Scan(out.Visible).Findings...)
	return out
}

func hiddenStyle(style string) bool {
	s := strings.ToLower(style)
	if s == "" {
		return false
	}
	for _, re := range reStyleHidden {
		if re.MatchString(s) {
			return true
		}
	}
	return false
}

var reDomainish = regexp.MustCompile(`(?i)\b([a-z0-9-]+\.)+(com|org|net|gov|edu|io|dev|br|uk|de|test|bank|app|co)\b`)

// CheckLinkText flags an anchor whose text shows one site while the link
// goes to another (the classic phishing link).
func CheckLinkText(r *Report, text, href string) {
	shown := reDomainish.FindString(text)
	if shown == "" {
		return
	}
	real := hostOf(href)
	if real == "another site" {
		return
	}
	sh := strings.ToLower(strings.TrimPrefix(shown, "www."))
	rl := strings.ToLower(strings.TrimPrefix(real, "www."))
	if sh != rl && !strings.HasSuffix(rl, "."+sh) {
		r.Add(Finding{Kind: KindLink, Severity: "medium", Detail: "a link that shows " + sh + " but goes to " + rl, Snippet: clip(text, 80)})
	}
}

var reTags = regexp.MustCompile(`<[^>]*>`)

func stripTags(s string) string { return reTags.ReplaceAllString(s, " ") }

func normalizeSpace(s string) string {
	lines := strings.Split(s, "\n")
	var out []string
	for _, l := range lines {
		l = strings.Join(strings.Fields(l), " ")
		if l != "" {
			out = append(out, l)
		}
	}
	return strings.Join(out, "\n")
}

// HostAllowed reports whether a URL's host is one of the allowed hosts
// (exact, or below a "*.example.org" entry).
func HostAllowed(raw string, allowed []string) bool {
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	h := strings.ToLower(u.Hostname())
	for _, a := range allowed {
		a = strings.ToLower(strings.TrimSpace(a))
		if strings.HasPrefix(a, "*.") {
			if strings.HasSuffix(h, a[1:]) {
				return true
			}
		} else if h == a {
			return true
		}
	}
	return false
}
