// Package mailout builds the one kind of message the reply skill sends:
// a plain-text reply (UTF-8, quoted-printable), no HTML, no attachment,
// one recipient, with the thread headers of the message it answers.
package mailout

import (
	"bytes"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"mime"
	"mime/quotedprintable"
	"net/mail"
	"strings"
	"time"
	"unicode/utf8"
)

// Draft is what the person confirms.
type Draft struct {
	FromName   string `json:"from_name,omitempty"`
	From       string `json:"from"`
	To         string `json:"to"`
	ToName     string `json:"to_name,omitempty"`
	Subject    string `json:"subject"`
	Body       string `json:"body"`
	InReplyTo  string `json:"in_reply_to,omitempty"`
	References string `json:"references,omitempty"`
}

// MaxBody is the longest body sent (bytes).
const MaxBody = 20000

// Check validates a draft.
func (d Draft) Check() error {
	for _, a := range []string{d.From, d.To} {
		p, err := mail.ParseAddress(a)
		if err != nil || p.Address != a {
			return fmt.Errorf("%q is not an e-mail address", a)
		}
	}
	for _, h := range []string{d.FromName, d.ToName, d.Subject, d.InReplyTo, d.References} {
		if strings.ContainsAny(h, "\r\n\x00") || !utf8.ValidString(h) {
			return errors.New("a header has a line break or invalid text")
		}
	}
	if strings.TrimSpace(d.Body) == "" {
		return errors.New("the message is empty")
	}
	if len(d.Body) > MaxBody || !utf8.ValidString(d.Body) || strings.ContainsRune(d.Body, 0) {
		return errors.New("the message is too long or not text")
	}
	if len(d.Subject) > 400 {
		return errors.New("the subject is too long")
	}
	for _, id := range append([]string{d.InReplyTo}, strings.Fields(d.References)...) {
		if id != "" && !(strings.HasPrefix(id, "<") && strings.HasSuffix(id, ">") && !strings.ContainsAny(id[1:len(id)-1], "<> ")) {
			return fmt.Errorf("bad message id %q", id)
		}
	}
	return nil
}

// Compose returns the message bytes and its Message-ID.
func Compose(d Draft, now time.Time) ([]byte, string, error) {
	if err := d.Check(); err != nil {
		return nil, "", err
	}
	b := make([]byte, 12)
	_, _ = rand.Read(b)
	domain := d.From[strings.LastIndex(d.From, "@")+1:]
	id := "<" + hex.EncodeToString(b) + "@" + domain + ">"
	var h bytes.Buffer
	addr := func(name, a string) string { return (&mail.Address{Name: name, Address: a}).String() }
	fmt.Fprintf(&h, "From: %s\r\n", addr(d.FromName, d.From))
	fmt.Fprintf(&h, "To: %s\r\n", addr(d.ToName, d.To))
	fmt.Fprintf(&h, "Subject: %s\r\n", mime.QEncoding.Encode("utf-8", d.Subject))
	fmt.Fprintf(&h, "Date: %s\r\n", now.Format(time.RFC1123Z))
	fmt.Fprintf(&h, "Message-ID: %s\r\n", id)
	if d.InReplyTo != "" {
		fmt.Fprintf(&h, "In-Reply-To: %s\r\n", d.InReplyTo)
		refs := strings.TrimSpace(d.References + " " + d.InReplyTo)
		fmt.Fprintf(&h, "References: %s\r\n", refs)
	}
	h.WriteString("MIME-Version: 1.0\r\nContent-Type: text/plain; charset=utf-8\r\nContent-Transfer-Encoding: quoted-printable\r\n")
	h.WriteString("User-Agent: Basalt desktop assistant\r\n\r\n")
	w := quotedprintable.NewWriter(&h)
	if _, err := w.Write([]byte(strings.ReplaceAll(strings.ReplaceAll(d.Body, "\r\n", "\n"), "\n", "\r\n"))); err != nil {
		return nil, "", err
	}
	_ = w.Close()
	h.WriteString("\r\n")
	return h.Bytes(), id, nil
}
