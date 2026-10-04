package imap

import (
	"bytes"
	"encoding/base64"
	"io"
	"mime"
	"mime/multipart"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/basalt-os/basalt-shell/internal/guard"
)

// Message is a parsed e-mail, reduced to what the skill needs.
type Message struct {
	UID         int          `json:"uid"`
	FromName    string       `json:"from_name"`
	FromAddr    string       `json:"from_addr"`
	To          string       `json:"to,omitempty"`
	Subject     string       `json:"subject"`
	Date        time.Time    `json:"date"`
	Flags       []string     `json:"flags,omitempty"`
	Text        string       `json:"text"`             // what a reader sees (plain part, or the HTML part's visible text)
	Hidden      string       `json:"hidden,omitempty"` // hidden HTML text, never given to the model
	Attachments []string     `json:"attachments,omitempty"`
	Report      guard.Report `json:"report"`
}

var wordDecoder = &mime.WordDecoder{CharsetReader: charsetReader}

// charsetReader handles the charsets the standard library does not:
// ISO-8859-1 and Windows-1252 (the common ones); anything else is read
// as is, invalid bytes replaced.
func charsetReader(cs string, r io.Reader) (io.Reader, error) {
	b, err := io.ReadAll(io.LimitReader(r, 4<<20))
	if err != nil {
		return nil, err
	}
	return bytes.NewReader(toUTF8(cs, b)), nil
}

func toUTF8(cs string, b []byte) []byte {
	cs = strings.ToLower(strings.TrimSpace(cs))
	switch cs {
	case "", "utf-8", "utf8", "us-ascii", "ascii":
		return []byte(strings.ToValidUTF8(string(b), "�"))
	case "iso-8859-1", "latin1", "iso-8859-15", "windows-1252", "cp1252":
		var sb strings.Builder
		for _, c := range b {
			sb.WriteRune(rune(c))
		}
		return []byte(sb.String())
	}
	if utf8.Valid(b) {
		return b
	}
	return []byte(strings.ToValidUTF8(string(b), "�"))
}

func decodeHeader(s string) string {
	d, err := wordDecoder.DecodeHeader(s)
	if err != nil {
		return s
	}
	return d
}

// Parse reads a message. Errors are tolerated: whatever could be read is
// returned.
func Parse(raw []byte) Message {
	var m Message
	msg, err := mail.ReadMessage(bytes.NewReader(raw))
	if err != nil {
		m.Text = guard.Clean(string(toUTF8("", raw)), 20000)
		m.Report = guard.Scan(m.Text)
		return m
	}
	h := msg.Header
	m.Subject = decodeHeader(h.Get("Subject"))
	if a, err := (&mail.AddressParser{WordDecoder: wordDecoder}).Parse(h.Get("From")); err == nil {
		m.FromName, m.FromAddr = a.Name, strings.ToLower(a.Address)
	} else {
		m.FromName = decodeHeader(h.Get("From"))
	}
	m.To = decodeHeader(h.Get("To"))
	if d, err := h.Date(); err == nil {
		m.Date = d.UTC()
	}
	var plain, htmlText, hidden string
	var rep guard.Report
	walk(h.Get("Content-Type"), h.Get("Content-Transfer-Encoding"), msg.Body, 0, func(ct, name string, body []byte) {
		switch {
		case name != "":
			m.Attachments = append(m.Attachments, name)
		case ct == "text/plain" && plain == "":
			plain = string(body)
		case ct == "text/html" && htmlText == "":
			ht := guard.ParseHTML(string(body))
			htmlText, hidden = ht.Visible, ht.Hidden
			rep.Add(ht.Report.Findings...)
		}
	})
	switch {
	case plain != "":
		m.Text = plain
		// A plain part next to an HTML one: the HTML part's hidden text is
		// still what an attacker might count on (some clients show HTML).
		if hidden != "" {
			m.Hidden = hidden
		}
	default:
		m.Text, m.Hidden = htmlText, hidden
	}
	rep.Add(guard.Scan(m.Text).Findings...)
	// The subject and the sender's display name are content too.
	rep.Add(guard.Scan(m.Subject + "\n" + m.FromName).Findings...)
	if spoofedName(m.FromName, m.FromAddr) {
		rep.Add(guard.Finding{Kind: guard.KindLink, Severity: "medium", Detail: "the sender's name shows another address than the real one", Snippet: m.FromName + " <" + m.FromAddr + ">"})
	}
	m.Report = rep
	return m
}

// spoofedName: a display name that contains an e-mail address different
// from the real sender.
func spoofedName(name, addr string) bool {
	if !strings.Contains(name, "@") {
		return false
	}
	return !strings.Contains(strings.ToLower(name), addr)
}

// walk visits the leaf parts of a MIME body (depth limited).
func walk(ctype, cte string, body io.Reader, depth int, fn func(ct, name string, body []byte)) {
	if depth > 8 {
		return
	}
	mt, params, err := mime.ParseMediaType(ctype)
	if err != nil || ctype == "" {
		mt, params = "text/plain", map[string]string{}
	}
	if strings.HasPrefix(mt, "multipart/") {
		mr := multipart.NewReader(body, params["boundary"])
		for i := 0; i < 50; i++ {
			p, err := mr.NextRawPart()
			if err != nil {
				return
			}
			walk(p.Header.Get("Content-Type"), p.Header.Get("Content-Transfer-Encoding"), p, depth+1, func(ct, name string, b []byte) {
				if name == "" {
					if _, dp, err := mime.ParseMediaType(p.Header.Get("Content-Disposition")); err == nil && dp["filename"] != "" {
						name = decodeHeader(dp["filename"])
					}
				}
				fn(ct, name, b)
			})
		}
		return
	}
	var r io.Reader = body
	switch strings.ToLower(strings.TrimSpace(cte)) {
	case "base64":
		r = base64.NewDecoder(base64.StdEncoding, &stripNL{r: body})
	case "quoted-printable":
		r = quotedprintable.NewReader(body)
	}
	b, _ := io.ReadAll(io.LimitReader(r, 2<<20))
	name := ""
	if params["name"] != "" {
		name = decodeHeader(params["name"])
	}
	if strings.HasPrefix(mt, "text/") && name == "" {
		b = toUTF8(params["charset"], b)
	} else if name == "" && !strings.HasPrefix(mt, "text/") {
		name = "(" + mt + ")"
	}
	fn(mt, name, b)
}

// stripNL drops line breaks for the base64 decoder.
type stripNL struct{ r io.Reader }

func (s *stripNL) Read(p []byte) (int, error) {
	n, err := s.r.Read(p)
	j := 0
	for i := 0; i < n; i++ {
		if p[i] != '\r' && p[i] != '\n' {
			p[j] = p[i]
			j++
		}
	}
	return j, err
}
