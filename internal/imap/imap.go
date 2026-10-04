// Package imap is a small, read-only IMAP4rev1 client for the e-mail
// skill. It can log in, open a mailbox with EXAMINE (read-only: the
// server will not change flags), search and fetch messages with
// BODY.PEEK (which does not set \Seen). It cannot send, store flags,
// copy, move, append or expunge: the command set is closed and checked
// before anything is written to the server (Allowed).
package imap

import (
	"bufio"
	"crypto/tls"
	"errors"
	"fmt"
	"io"
	"net"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Allowed is the closed set of commands this client may send.
var Allowed = map[string]bool{
	"CAPABILITY": true, "LOGIN": true, "EXAMINE": true, "UID SEARCH": true, "UID FETCH": true, "LOGOUT": true, "NOOP": true,
}

// ErrNotAllowed is returned for a command outside Allowed.
var ErrNotAllowed = errors.New("imap: command not allowed in read-only mode")

// Conn is one IMAP connection.
type Conn struct {
	c   net.Conn
	r   *bufio.Reader
	tag int
	// Sent records every command verb sent (for tests and the audit).
	Sent []string
}

// Dial connects (tls: implicit TLS, else plain TCP).
func Dial(addr string, useTLS bool, timeout time.Duration) (*Conn, error) {
	d := net.Dialer{Timeout: timeout}
	var c net.Conn
	var err error
	if useTLS {
		host, _, _ := net.SplitHostPort(addr)
		c, err = tls.DialWithDialer(&d, "tcp", addr, &tls.Config{ServerName: host, MinVersion: tls.VersionTLS12})
	} else {
		c, err = d.Dial("tcp", addr)
	}
	if err != nil {
		return nil, err
	}
	_ = c.SetDeadline(time.Now().Add(2 * time.Minute))
	ic := &Conn{c: c, r: bufio.NewReaderSize(c, 1<<16)}
	line, err := ic.r.ReadString('\n')
	if err != nil {
		c.Close()
		return nil, err
	}
	if !strings.HasPrefix(line, "* OK") {
		c.Close()
		return nil, fmt.Errorf("imap: unexpected greeting %q", strings.TrimSpace(line))
	}
	return ic, nil
}

// Close logs out and closes.
func (ic *Conn) Close() {
	_, _ = ic.cmd("LOGOUT", "")
	ic.c.Close()
}

// Response is the untagged data of one command; literals are inlined.
type Response struct {
	Lines []string
}

func quote(s string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(s) + `"`
}

// cmd sends one command (verb from Allowed, then args) and reads until
// its tagged reply.
func (ic *Conn) cmd(verb, args string) (Response, error) {
	if !Allowed[verb] {
		return Response{}, fmt.Errorf("%w: %s", ErrNotAllowed, verb)
	}
	if strings.ContainsAny(args, "\r\n") {
		return Response{}, errors.New("imap: line break in arguments")
	}
	ic.tag++
	tag := "b" + strconv.Itoa(ic.tag)
	line := tag + " " + verb
	if args != "" {
		line += " " + args
	}
	if _, err := io.WriteString(ic.c, line+"\r\n"); err != nil {
		return Response{}, err
	}
	ic.Sent = append(ic.Sent, verb)
	var res Response
	for {
		l, err := ic.readLine()
		if err != nil {
			return res, err
		}
		if strings.HasPrefix(l, tag+" ") {
			st := strings.TrimPrefix(l, tag+" ")
			if strings.HasPrefix(st, "OK") {
				return res, nil
			}
			return res, fmt.Errorf("imap: %s: %s", verb, st)
		}
		res.Lines = append(res.Lines, l)
	}
}

var reLiteral = regexp.MustCompile(`\{(\d+)\}\r?\n?$`)

// readLine reads one response line, inlining literals ({N} then N bytes)
// as a quoted marker so callers can find them: the literal bytes follow
// "\x00LIT\x00" and end at "\x00END\x00".
func (ic *Conn) readLine() (string, error) {
	var b strings.Builder
	for {
		l, err := ic.r.ReadString('\n')
		if err != nil {
			return "", err
		}
		if m := reLiteral.FindStringSubmatchIndex(l); m != nil {
			n, _ := strconv.Atoi(l[m[2]:m[3]])
			if n > 16<<20 {
				return "", errors.New("imap: literal too large")
			}
			b.WriteString(l[:m[0]])
			buf := make([]byte, n)
			if _, err := io.ReadFull(ic.r, buf); err != nil {
				return "", err
			}
			b.WriteString("\x00LIT\x00")
			b.Write(buf)
			b.WriteString("\x00END\x00")
			continue
		}
		b.WriteString(strings.TrimRight(l, "\r\n"))
		return b.String(), nil
	}
}

// Login authenticates with a user name and password.
func (ic *Conn) Login(user, pass string) error {
	_, err := ic.cmd("LOGIN", quote(user)+" "+quote(pass))
	return err
}

// Examine opens a mailbox read-only and returns its message count.
func (ic *Conn) Examine(mailbox string) (int, error) {
	res, err := ic.cmd("EXAMINE", quote(mailbox))
	if err != nil {
		return 0, err
	}
	n := 0
	for _, l := range res.Lines {
		var k int
		if _, err := fmt.Sscanf(l, "* %d EXISTS", &k); err == nil {
			n = k
		}
	}
	return n, nil
}

// Criteria is a search, built only from typed fields (no raw IMAP).
type Criteria struct {
	Since   time.Time
	Before  time.Time
	From    string
	Subject string
	Text    string
	// AnyText matches messages that contain any of these words (OR).
	AnyText []string
}

func astr(s string) string {
	s = strings.Map(func(r rune) rune {
		if r < 0x20 || r == 0x7f {
			return -1
		}
		return r
	}, s)
	return quote(s)
}

// Search returns the UIDs matching (all when the criteria are empty).
func (ic *Conn) Search(c Criteria) ([]int, error) {
	var parts []string
	if !c.Since.IsZero() {
		parts = append(parts, "SINCE "+c.Since.Format("2-Jan-2006"))
	}
	if !c.Before.IsZero() {
		parts = append(parts, "BEFORE "+c.Before.Format("2-Jan-2006"))
	}
	if c.From != "" {
		parts = append(parts, "FROM "+astr(c.From))
	}
	if c.Subject != "" {
		parts = append(parts, "SUBJECT "+astr(c.Subject))
	}
	if c.Text != "" {
		parts = append(parts, "TEXT "+astr(c.Text))
	}
	if n := len(c.AnyText); n > 0 {
		// OR is binary in IMAP: OR TEXT a OR TEXT b TEXT c
		expr := "TEXT " + astr(c.AnyText[n-1])
		for i := n - 2; i >= 0; i-- {
			expr = "OR TEXT " + astr(c.AnyText[i]) + " " + expr
		}
		parts = append(parts, expr)
	}
	if len(parts) == 0 {
		parts = []string{"ALL"}
	}
	args := strings.Join(parts, " ")
	if strings.ContainsAny(args, "\x00") {
		return nil, errors.New("imap: bad criteria")
	}
	res, err := ic.cmd("UID SEARCH", "CHARSET UTF-8 "+args)
	if err != nil {
		// Some servers refuse CHARSET with plain ASCII; retry without.
		res, err = ic.cmd("UID SEARCH", args)
		if err != nil {
			return nil, err
		}
	}
	var uids []int
	for _, l := range res.Lines {
		if strings.HasPrefix(l, "* SEARCH") {
			for _, f := range strings.Fields(strings.TrimPrefix(l, "* SEARCH")) {
				if n, err := strconv.Atoi(f); err == nil {
					uids = append(uids, n)
				}
			}
		}
	}
	return uids, nil
}

// Raw is one fetched message.
type Raw struct {
	UID   int
	Flags []string
	Date  string // INTERNALDATE
	Size  int
	Body  []byte // the message (possibly truncated at max bytes)
}

var reFlags = regexp.MustCompile(`FLAGS \(([^)]*)\)`)
var reInternal = regexp.MustCompile(`INTERNALDATE "([^"]+)"`)
var reSize = regexp.MustCompile(`RFC822\.SIZE (\d+)`)
var reUID = regexp.MustCompile(`UID (\d+)`)

// Fetch reads messages by UID without marking them seen (BODY.PEEK),
// at most max bytes of each.
func (ic *Conn) Fetch(uids []int, max int) ([]Raw, error) {
	if len(uids) == 0 {
		return nil, nil
	}
	var set []string
	for _, u := range uids {
		set = append(set, strconv.Itoa(u))
	}
	res, err := ic.cmd("UID FETCH", strings.Join(set, ",")+fmt.Sprintf(" (UID FLAGS INTERNALDATE RFC822.SIZE BODY.PEEK[]<0.%d>)", max))
	if err != nil {
		return nil, err
	}
	var out []Raw
	for _, l := range res.Lines {
		if !strings.HasPrefix(l, "* ") || !strings.Contains(l, " FETCH ") {
			continue
		}
		head := l
		var body []byte
		if i := strings.Index(l, "\x00LIT\x00"); i >= 0 {
			head = l[:i]
			rest := l[i+5:]
			if j := strings.Index(rest, "\x00END\x00"); j >= 0 {
				body = []byte(rest[:j])
				head += rest[j+5:]
			}
		}
		r := Raw{Body: body}
		if m := reUID.FindStringSubmatch(head); m != nil {
			r.UID, _ = strconv.Atoi(m[1])
		}
		if m := reFlags.FindStringSubmatch(head); m != nil {
			r.Flags = strings.Fields(m[1])
		}
		if m := reInternal.FindStringSubmatch(head); m != nil {
			r.Date = m[1]
		}
		if m := reSize.FindStringSubmatch(head); m != nil {
			r.Size, _ = strconv.Atoi(m[1])
		}
		out = append(out, r)
	}
	return out, nil
}

// Flags returns the flags of messages without fetching bodies (for the
// read-only check in tests).
func (ic *Conn) Flags(uids []int) (map[int][]string, error) {
	var set []string
	for _, u := range uids {
		set = append(set, strconv.Itoa(u))
	}
	res, err := ic.cmd("UID FETCH", strings.Join(set, ",")+" (UID FLAGS)")
	if err != nil {
		return nil, err
	}
	out := map[int][]string{}
	for _, l := range res.Lines {
		m := reUID.FindStringSubmatch(l)
		f := reFlags.FindStringSubmatch(l)
		if m != nil && f != nil {
			u, _ := strconv.Atoi(m[1])
			out[u] = strings.Fields(f[1])
		}
	}
	return out, nil
}
