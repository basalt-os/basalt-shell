// Command basalt-skill is the worker of the read-only skills: it reads
// content for the shell and gives back text, metadata and the guard's
// findings. It never decides anything and never acts: the shell starts it
// for one job, in a session of its own (a systemd scope in a slice that
// basalt-resolver makes default-deny, with an allowlist made of the
// person's grants), and with the Basalt policy in its own SELinux domain.
//
// Installed twice, with two SELinux types:
//
//	/usr/libexec/basalt-shell/basalt-skill-index   builds the file index: may read the
//	                                               person's documents (never keys,
//	                                               keyrings or browser profiles)
//	/usr/libexec/basalt-shell/basalt-skill         searches the index, reads a mailbox
//	                                               (IMAP, read only), reads a web page
//	                                               (headless Chromium): an agent domain
//
// Protocol: it prints {"ready":true,"pid":N}, waits for one job (a JSON
// line on stdin; the shell registers the session's network policy in
// between), prints one result line and exits.
package main

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/basalt-os/basalt-shell/internal/browser"
	"github.com/basalt-os/basalt-shell/internal/docs"
	"github.com/basalt-os/basalt-shell/internal/guard"
	"github.com/basalt-os/basalt-shell/internal/imap"
)

// Job is one request from the shell.
type Job struct {
	Kind  string     `json:"kind"` // index, search, mail, web
	Home  string     `json:"home,omitempty"`
	Roots []string   `json:"roots,omitempty"`
	Index string     `json:"index,omitempty"`
	Query docs.Query `json:"query,omitempty"`
	Mail  *MailJob   `json:"mail,omitempty"`
	Web   *WebJob    `json:"web,omitempty"`
}

// MailJob reads messages from one mailbox.
type MailJob struct {
	Host     string    `json:"host"`
	Port     int       `json:"port"`
	TLS      bool      `json:"tls"`
	User     string    `json:"user"`
	Pass     string    `json:"pass"`
	Mailbox  string    `json:"mailbox"`
	Since    time.Time `json:"since,omitempty"`
	Before   time.Time `json:"before,omitempty"`
	From     string    `json:"from,omitempty"`
	Subject  string    `json:"subject,omitempty"`
	Text     string    `json:"text,omitempty"`
	Words    []string  `json:"words,omitempty"`
	Limit    int       `json:"limit,omitempty"`
	MaxBytes int       `json:"max_bytes,omitempty"`
}

// WebJob reads one page.
type WebJob struct {
	URL   string   `json:"url"`
	Allow []string `json:"allow"`
}

// Result goes back to the shell.
type Result struct {
	OK       bool            `json:"ok"`
	Error    string          `json:"error,omitempty"`
	Kind     string          `json:"kind"`
	MS       int64           `json:"ms"`
	Index    *IndexStats     `json:"index,omitempty"`
	Hits     []docs.Hit      `json:"hits,omitempty"`
	Messages []imap.Message  `json:"messages,omitempty"`
	Total    int             `json:"total,omitempty"`
	Page     *browser.Page   `json:"page,omitempty"`
	Sent     []string        `json:"imap_commands,omitempty"`
	Domain   string          `json:"domain,omitempty"`
	Extra    json.RawMessage `json:"extra,omitempty"`
}

// IndexStats summarizes a build.
type IndexStats struct {
	Docs    int      `json:"docs"`
	Skipped int      `json:"skipped"`
	MS      int64    `json:"ms"`
	Flagged int      `json:"flagged"`
	Errors  []string `json:"errors,omitempty"`
}

func main() {
	self := filepath.Base(os.Args[0])
	out := json.NewEncoder(os.Stdout)
	_ = out.Encode(map[string]any{"ready": true, "pid": os.Getpid()})
	in := bufio.NewReaderSize(os.Stdin, 1<<20)
	line, err := in.ReadBytes('\n')
	if err != nil && len(line) == 0 {
		os.Exit(1)
	}
	var job Job
	if err := json.Unmarshal(line, &job); err != nil {
		_ = out.Encode(Result{Error: "bad job: " + err.Error()})
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 90*time.Second)
	defer cancel()
	start := time.Now()
	res := run(ctx, self, job)
	res.Kind, res.MS = job.Kind, time.Since(start).Milliseconds()
	if b, err := os.ReadFile("/proc/self/attr/current"); err == nil {
		res.Domain = strings.TrimRight(string(b), "\x00\n")
	}
	_ = out.Encode(res)
}

// anyKind lets one binary do every job (development and unit tests, no
// SELinux policy).
var anyKind = os.Getenv("BASALT_SKILL_ANY") == "1"

func run(ctx context.Context, self string, job Job) Result {
	indexer := self == "basalt-skill-index"
	if job.Kind == "index" && !indexer && !anyKind {
		return Result{Error: "index jobs run in basalt-skill-index"}
	}
	if job.Kind != "index" && indexer && !anyKind {
		return Result{Error: "basalt-skill-index only builds the index"}
	}
	switch job.Kind {
	case "index":
		if job.Home == "" || job.Index == "" || len(job.Roots) == 0 {
			return Result{Error: "index: home, roots and index are required"}
		}
		ix, err := docs.Build(ctx, docs.Options{Home: job.Home, Roots: job.Roots})
		if err != nil {
			return Result{Error: err.Error()}
		}
		if err := ix.Save(job.Index); err != nil {
			return Result{Error: err.Error()}
		}
		st := &IndexStats{Docs: len(ix.Docs), Skipped: ix.Skipped, MS: ix.MS, Errors: ix.Errors}
		for _, d := range ix.Docs {
			if d.Report.Suspicious() {
				st.Flagged++
			}
		}
		return Result{OK: true, Index: st}
	case "search":
		ix, err := docs.Load(job.Index)
		if err != nil {
			return Result{Error: "the file index is not built yet: " + err.Error()}
		}
		return Result{OK: true, Hits: docs.Search(ix, job.Query)}
	case "mail":
		if job.Mail == nil {
			return Result{Error: "mail: missing"}
		}
		return mail(ctx, *job.Mail)
	case "web":
		if job.Web == nil {
			return Result{Error: "web: missing"}
		}
		p, err := browser.Read(ctx, browser.Options{URL: job.Web.URL, Allow: job.Web.Allow,
			Profile: filepath.Join(os.TempDir(), fmt.Sprintf("basalt-skill-browser-%d", os.Getpid()))})
		if err != nil {
			return Result{Error: err.Error()}
		}
		p.Visible = guard.Clean(p.Visible, 30000)
		return Result{OK: true, Page: p}
	}
	return Result{Error: "unknown job " + strconv.Quote(job.Kind)}
}

func mail(ctx context.Context, j MailJob) Result {
	if j.Port == 0 {
		j.Port = 993
		if !j.TLS {
			j.Port = 143
		}
	}
	if j.Limit <= 0 || j.Limit > 20 {
		j.Limit = 8
	}
	if j.MaxBytes <= 0 {
		j.MaxBytes = 256 << 10
	}
	if j.Mailbox == "" {
		j.Mailbox = "INBOX"
	}
	c, err := imap.Dial(net.JoinHostPort(j.Host, strconv.Itoa(j.Port)), j.TLS, 15*time.Second)
	if err != nil {
		return Result{Error: "cannot reach the mail server: " + err.Error()}
	}
	defer c.Close()
	if err := c.Login(j.User, j.Pass); err != nil {
		return Result{Error: "login failed", Sent: c.Sent}
	}
	if _, err := c.Examine(j.Mailbox); err != nil {
		return Result{Error: err.Error(), Sent: c.Sent}
	}
	var uids []int
	var err2 error
	if len(j.Words) > 0 {
		// The messages that mention the request's words first (server-side
		// search), so an older matching message is not cut by the limit.
		w := j.Words
		if len(w) > 6 {
			w = w[:6]
		}
		uids, err2 = c.Search(imap.Criteria{Since: j.Since, Before: j.Before, From: j.From, AnyText: w})
		if err2 != nil {
			uids = nil
		}
	}
	if len(uids) == 0 {
		var err error
		uids, err = c.Search(imap.Criteria{Since: j.Since, Before: j.Before, From: j.From, Subject: j.Subject, Text: j.Text})
		if err != nil {
			return Result{Error: err.Error(), Sent: c.Sent}
		}
	}
	if len(uids) == 0 && j.From != "" {
		// The sender as said ("the landlord") may not be in the header:
		// the time range only, and the shell ranks by the request's words.
		uids, err = c.Search(imap.Criteria{Since: j.Since, Before: j.Before})
		if err != nil {
			return Result{Error: err.Error(), Sent: c.Sent}
		}
	}
	total := len(uids)
	if len(uids) > j.Limit {
		uids = uids[len(uids)-j.Limit:] // the newest
	}
	raws, err := c.Fetch(uids, j.MaxBytes)
	if err != nil {
		return Result{Error: err.Error(), Sent: c.Sent}
	}
	var msgs []imap.Message
	for i := len(raws) - 1; i >= 0; i-- {
		m := imap.Parse(raws[i].Body)
		m.UID, m.Flags = raws[i].UID, raws[i].Flags
		m.Text = guard.Clean(m.Text, 6000)
		if len(m.Hidden) > 2000 {
			m.Hidden = m.Hidden[:2000]
		}
		msgs = append(msgs, m)
	}
	if ctx.Err() != nil {
		return Result{Error: errors.New("timed out").Error()}
	}
	return Result{OK: true, Messages: msgs, Total: total, Sent: c.Sent}
}
