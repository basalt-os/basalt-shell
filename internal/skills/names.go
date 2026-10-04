package skills

import (
	"context"
	"encoding/json"
	"regexp"
	"sort"
	"strings"
	"time"
	"unicode"
)

// Names the person is likely to say, for the speech recognition prompt
// (Whisper takes a short text that biases its spelling): the contacts the
// person listed in skills.conf ([contacts] names = ...) and, while a
// mailbox is granted, the display names of the senders of its recent
// messages. Only plain names are used (letters, one to three words); a
// sender name that tries to instruct the assistant is never used.

// reName: one to three words of letters (with ' and -), each starting
// with an upper-case letter.
var reName = regexp.MustCompile(`^\p{Lu}[\p{L}'-]*(?: \p{Lu}[\p{L}'-]*){0,2}$`)

// PlainName reports whether s looks like a person's name.
func PlainName(s string) bool {
	s = strings.TrimSpace(s)
	if len(s) < 2 || len(s) > 40 || !reName.MatchString(s) {
		return false
	}
	for _, r := range s {
		if unicode.IsDigit(r) {
			return false
		}
	}
	return true
}

type senderCache struct {
	at    time.Time
	names map[string]int // name -> messages
}

// SpeechNames returns at most n names: contacts first, then the most
// frequent senders of the granted mailbox.
func (e *Engine) SpeechNames(n int) []string {
	var out []string
	seen := map[string]bool{}
	add := func(s string) {
		s = strings.TrimSpace(s)
		k := strings.ToLower(s)
		if len(out) < n && PlainName(s) && !seen[k] {
			seen[k] = true
			out = append(out, s)
		}
	}
	for _, c := range e.Config.Contacts {
		add(c)
	}
	if len(e.Config.Accounts) == 0 {
		return out
	}
	if _, ok := e.Store.Mailbox(e.Config.Accounts[0].Name); !ok {
		return out
	}
	e.mu.Lock()
	sc := e.senders
	e.mu.Unlock()
	if sc == nil {
		return out
	}
	type kv struct {
		name string
		n    int
	}
	var all []kv
	for k, v := range sc.names {
		all = append(all, kv{k, v})
	}
	sort.Slice(all, func(i, j int) bool {
		if all[i].n != all[j].n {
			return all[i].n > all[j].n
		}
		return all[i].name < all[j].name
	})
	for _, x := range all {
		add(x.name)
	}
	return out
}

// RefreshSenders reads the From headers of the granted mailbox's last 90
// days in a worker (the mail session: the account's IMAP host only), at
// most every ten minutes.
func (e *Engine) RefreshSenders(ctx context.Context) error {
	if len(e.Config.Accounts) == 0 {
		return nil
	}
	ac := e.Config.Accounts[0]
	if _, ok := e.Store.Mailbox(ac.Name); !ok {
		return nil
	}
	e.mu.Lock()
	fresh := e.senders != nil && time.Since(e.senders.at) < 10*time.Minute
	e.mu.Unlock()
	if fresh {
		return nil
	}
	pass, err := ac.Password()
	if err != nil {
		return err
	}
	port := ac.Port
	if port == 0 {
		port = 143
		if ac.TLS {
			port = 993
		}
	}
	job := map[string]any{"kind": "senders", "mail": map[string]any{"host": ac.Host, "port": ac.Port, "tls": ac.TLS, "user": ac.User, "pass": pass,
		"mailbox": ac.Mailbox, "since": e.Now().AddDate(0, 0, -90)}}
	raw, sess, err := e.Runner.Run(ctx, job, "mail", []string{AllowEntry(ac.Host, port)}, true)
	if err != nil {
		return err
	}
	var res struct {
		OK      bool   `json:"ok"`
		Error   string `json:"error"`
		Senders []struct {
			Name    string `json:"name"`
			Flagged bool   `json:"flagged"`
		} `json:"senders"`
	}
	if json.Unmarshal(raw, &res) != nil || !res.OK {
		return nil
	}
	sc := &senderCache{at: time.Now(), names: map[string]int{}}
	for _, s := range res.Senders {
		if !s.Flagged && PlainName(s.Name) {
			sc.names[strings.TrimSpace(s.Name)]++
		}
	}
	e.mu.Lock()
	e.senders = sc
	e.mu.Unlock()
	e.audit("skill", "sender names read for speech recognition", map[string]any{"names": len(sc.names), "session": sess})
	return nil
}
