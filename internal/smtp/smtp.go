// Package smtp is the send-only mail client of the reply skill: it sends
// one message the person confirmed, to one recipient, and nothing else.
// Like the read-only IMAP client, its command set is closed (EHLO,
// STARTTLS, AUTH PLAIN, MAIL FROM, RCPT TO, DATA, RSET, QUIT, NOOP) and
// checked before anything is written; there is no VRFY, EXPN, or a way
// to add recipients after the fact.
//
// TLS is required (implicit TLS on 465, or STARTTLS) unless the account
// says tls = none, which is meant for lab servers on reserved names.
package smtp

import (
	"bufio"
	"crypto/tls"
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"net"
	"net/mail"
	"strconv"
	"strings"
	"time"
)

// Allowed is the closed set of commands.
var Allowed = map[string]bool{"EHLO": true, "STARTTLS": true, "AUTH": true, "MAIL": true, "RCPT": true, "DATA": true, "RSET": true, "QUIT": true, "NOOP": true}

// ErrNotAllowed is returned for a command outside Allowed.
var ErrNotAllowed = errors.New("smtp: command not allowed")

// Options of one send.
type Options struct {
	Addr    string // host:port
	TLS     string // "tls" (implicit), "starttls" (default), "none" (lab only)
	User    string
	Pass    string
	Helo    string // our name in EHLO (default "localhost")
	Timeout time.Duration
}

// Result of a send.
type Result struct {
	Reply string   `json:"reply"` // the server's answer to the end of DATA (queue id)
	Sent  []string `json:"commands"`
}

// Conn is one SMTP connection.
type Conn struct {
	c    net.Conn
	r    *bufio.Reader
	host string
	ext  map[string]string
	Sent []string
}

func dial(o Options) (*Conn, error) {
	host, _, err := net.SplitHostPort(o.Addr)
	if err != nil {
		return nil, err
	}
	d := net.Dialer{Timeout: o.Timeout}
	var c net.Conn
	if o.TLS == "tls" {
		c, err = tls.DialWithDialer(&d, "tcp", o.Addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	} else {
		c, err = d.Dial("tcp", o.Addr)
	}
	if err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Now().Add(2 * time.Minute))
	sc := &Conn{c: c, r: bufio.NewReader(c), host: host}
	if code, msg, err := sc.reply(); err != nil || code != 220 {
		c.Close()
		if err == nil {
			err = fmt.Errorf("smtp: greeting %d %s", code, msg)
		}
		return nil, err
	}
	return sc, nil
}

// reply reads a (multi-line) reply.
func (sc *Conn) reply() (int, string, error) {
	var lines []string
	code := 0
	for {
		l, err := sc.r.ReadString('\n')
		if err != nil {
			return 0, "", err
		}
		l = strings.TrimRight(l, "\r\n")
		if len(l) < 3 {
			return 0, "", fmt.Errorf("smtp: short reply %q", l)
		}
		code, err = strconv.Atoi(l[:3])
		if err != nil {
			return 0, "", fmt.Errorf("smtp: bad reply %q", l)
		}
		lines = append(lines, strings.TrimSpace(l[3:]))
		if len(l) == 3 || l[3] == ' ' {
			break
		}
		if len(lines) > 100 {
			return 0, "", errors.New("smtp: reply too long")
		}
	}
	return code, strings.Join(lines, "\n"), nil
}

// cmd sends one command (verb from Allowed) and reads the reply.
func (sc *Conn) cmd(verb, args string) (int, string, error) {
	if !Allowed[verb] {
		return 0, "", fmt.Errorf("%w: %s", ErrNotAllowed, verb)
	}
	if strings.ContainsAny(args, "\r\n") {
		return 0, "", errors.New("smtp: line break in arguments")
	}
	line := verb
	if args != "" {
		line += " " + args
	}
	if _, err := io.WriteString(sc.c, line+"\r\n"); err != nil {
		return 0, "", err
	}
	sc.Sent = append(sc.Sent, verb)
	return sc.reply()
}

func (sc *Conn) ehlo(name string) error {
	code, msg, err := sc.cmd("EHLO", name)
	if err != nil {
		return err
	}
	if code != 250 {
		return fmt.Errorf("smtp: EHLO %d %s", code, msg)
	}
	sc.ext = map[string]string{}
	for i, l := range strings.Split(msg, "\n") {
		if i == 0 {
			continue
		}
		k, v, _ := strings.Cut(l, " ")
		sc.ext[strings.ToUpper(k)] = v
	}
	return nil
}

// Send delivers raw (a complete RFC 5322 message) from one address to one
// recipient.
func Send(o Options, from, to string, raw []byte) (Result, error) {
	var res Result
	if o.Timeout == 0 {
		o.Timeout = 20 * time.Second
	}
	if o.TLS == "" {
		o.TLS = "starttls"
	}
	for _, a := range []string{from, to} {
		if p, err := mail.ParseAddress(a); err != nil || p.Address != a {
			return res, fmt.Errorf("smtp: bad address %q", a)
		}
	}
	sc, err := dial(o)
	if err != nil {
		return res, err
	}
	defer func() {
		_, _, _ = sc.cmd("QUIT", "")
		sc.c.Close()
		res.Sent = sc.Sent
	}()
	helo := o.Helo
	if helo == "" {
		helo = "localhost"
	}
	if err := sc.ehlo(helo); err != nil {
		return res, err
	}
	if o.TLS == "starttls" {
		if _, ok := sc.ext["STARTTLS"]; !ok {
			return res, errors.New("smtp: the server does not offer STARTTLS (TLS is required)")
		}
		if code, msg, err := sc.cmd("STARTTLS", ""); err != nil || code != 220 {
			if err == nil {
				err = fmt.Errorf("smtp: STARTTLS %d %s", code, msg)
			}
			return res, err
		}
		tc := tls.Client(sc.c, &tls.Config{ServerName: sc.host, MinVersion: tls.VersionTLS12})
		if err := tc.Handshake(); err != nil {
			return res, err
		}
		sc.c, sc.r = tc, bufio.NewReader(tc)
		if err := sc.ehlo(helo); err != nil {
			return res, err
		}
	} else if o.TLS != "tls" && o.TLS != "none" {
		return res, fmt.Errorf("smtp: unknown tls mode %q", o.TLS)
	}
	if o.User != "" {
		tok := base64.StdEncoding.EncodeToString([]byte("\x00" + o.User + "\x00" + o.Pass))
		if code, msg, err := sc.cmd("AUTH", "PLAIN "+tok); err != nil || code != 235 {
			if err == nil {
				err = fmt.Errorf("smtp: authentication failed (%d %s)", code, msg)
			}
			return res, err
		}
	}
	if code, msg, err := sc.cmd("MAIL", "FROM:<"+from+">"); err != nil || code != 250 {
		return res, fail("MAIL FROM", code, msg, err)
	}
	if code, msg, err := sc.cmd("RCPT", "TO:<"+to+">"); err != nil || (code != 250 && code != 251) {
		return res, fail("RCPT TO", code, msg, err)
	}
	if code, msg, err := sc.cmd("DATA", ""); err != nil || code != 354 {
		return res, fail("DATA", code, msg, err)
	}
	if _, err := sc.c.Write(dotStuff(raw)); err != nil {
		return res, err
	}
	code, msg, err := sc.reply()
	if err != nil || code != 250 {
		return res, fail("end of data", code, msg, err)
	}
	res.Reply = msg
	return res, nil
}

func fail(what string, code int, msg string, err error) error {
	if err != nil {
		return err
	}
	return fmt.Errorf("smtp: %s refused (%d %s)", what, code, msg)
}

// dotStuff normalizes line ends to CRLF, doubles leading dots and adds
// the end-of-data line.
func dotStuff(raw []byte) []byte {
	s := strings.ReplaceAll(string(raw), "\r\n", "\n")
	var b strings.Builder
	for _, l := range strings.Split(strings.TrimSuffix(s, "\n"), "\n") {
		if strings.HasPrefix(l, ".") {
			b.WriteString(".")
		}
		b.WriteString(l)
		b.WriteString("\r\n")
	}
	b.WriteString(".\r\n")
	return []byte(b.String())
}
