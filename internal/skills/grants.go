// Package skills runs the shell's read-only skills: find files, read
// and summarize e-mail, read and summarize a web page. It is driven only
// by the person's own request (typed in the command bar or spoken); the
// request is planned before any content is read, the content is read by
// a confined worker in a session whose network is limited to what the
// person granted, and the model that summarizes it has no tools. Content
// can make the summary wrong; it cannot make the shell do anything.
package skills

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/basalt-os/basalt-shell/internal/i18n"
)

// Grant kinds.
const (
	GrantFolder  = "folder"  // read files below a folder
	GrantMailbox = "mailbox" // read one mail account's mailbox
	GrantSite    = "site"    // read pages of one web site
)

// Grant is a scope the person gave the assistant, for a limited time.
type Grant struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Target  string    `json:"target"` // folder path, account name, or host
	Label   string    `json:"label"`  // for people: "Documents", "lab mail (INBOX)", "news.lab.test"
	Created time.Time `json:"created"`
	Expires time.Time `json:"expires"`
	By      string    `json:"by"` // who confirmed (ui)
	Uses    int       `json:"uses"`
}

// Store keeps the grants in memory: they end with the session, or at
// their expiry, whichever comes first.
type Store struct {
	mu     sync.Mutex
	grants []Grant
	Now    func() time.Time
	// OnExpire is called (outside the lock) for each grant that expired.
	OnExpire func(Grant)
	// OnChange is called after any change.
	OnChange func([]Grant)
}

// NewStore returns an empty store.
func NewStore() *Store { return &Store{Now: time.Now} }

func gid() string {
	b := make([]byte, 4)
	_, _ = rand.Read(b)
	return "g-" + hex.EncodeToString(b)
}

// Durations people can pick, and the limits.
const (
	MinGrant     = 10 * time.Second
	MaxGrant     = 7 * 24 * time.Hour
	DefaultGrant = time.Hour
)

// ParseDuration reads "15m", "1h", "8h", "1d", "30s".
func ParseDuration(s string) (time.Duration, error) {
	s = strings.TrimSpace(strings.ToLower(s))
	if s == "" {
		return DefaultGrant, nil
	}
	var d time.Duration
	var err error
	if strings.HasSuffix(s, "d") {
		var n int
		_, err = fmt.Sscanf(s, "%dd", &n)
		d = time.Duration(n) * 24 * time.Hour
	} else {
		d, err = time.ParseDuration(s)
	}
	if err != nil {
		return 0, fmt.Errorf("duration %q: use 30s, 15m, 1h, 8h or 1d", s)
	}
	if d < MinGrant || d > MaxGrant {
		return 0, fmt.Errorf("duration %s is out of range (10s to 7 days)", d)
	}
	return d, nil
}

// Human says a duration in words.
func Human(d time.Duration) string {
	switch {
	case d >= 24*time.Hour && d%(24*time.Hour) == 0:
		n := int(d / (24 * time.Hour))
		return i18n.N("%d day", "%d days", n, n)
	case d >= time.Hour && d%time.Hour == 0:
		n := int(d / time.Hour)
		return i18n.N("%d hour", "%d hours", n, n)
	case d >= time.Minute && d%time.Minute == 0:
		n := int(d / time.Minute)
		return i18n.N("%d minute", "%d minutes", n, n)
	}
	n := int(d / time.Second)
	return i18n.N("%d second", "%d seconds", n, n)
}

// Add stores a grant (replacing one with the same kind and target).
func (s *Store) Add(kind, target, label, by string, d time.Duration) Grant {
	now := s.Now()
	g := Grant{ID: gid(), Kind: kind, Target: target, Label: label, Created: now.UTC(), Expires: now.Add(d).UTC(), By: by}
	s.mu.Lock()
	var keep []Grant
	for _, o := range s.grants {
		if !(o.Kind == kind && o.Target == target) {
			keep = append(keep, o)
		}
	}
	s.grants = append(keep, g)
	list := s.copyLocked()
	s.mu.Unlock()
	if s.OnChange != nil {
		s.OnChange(list)
	}
	return g
}

func (s *Store) copyLocked() []Grant {
	out := make([]Grant, len(s.grants))
	copy(out, s.grants)
	return out
}

// Sweep removes expired grants and reports them.
func (s *Store) Sweep() {
	now := s.Now()
	s.mu.Lock()
	var keep, gone []Grant
	for _, g := range s.grants {
		if now.Before(g.Expires) {
			keep = append(keep, g)
		} else {
			gone = append(gone, g)
		}
	}
	s.grants = keep
	list := s.copyLocked()
	s.mu.Unlock()
	for _, g := range gone {
		if s.OnExpire != nil {
			s.OnExpire(g)
		}
	}
	if len(gone) > 0 && s.OnChange != nil {
		s.OnChange(list)
	}
}

// Active returns the unexpired grants of a kind.
func (s *Store) Active(kind string) []Grant {
	s.Sweep()
	s.mu.Lock()
	defer s.mu.Unlock()
	var out []Grant
	for _, g := range s.grants {
		if kind == "" || g.Kind == kind {
			out = append(out, g)
		}
	}
	sort.Slice(out, func(i, j int) bool { return out[i].Created.Before(out[j].Created) })
	return out
}

// Use counts a use of a grant.
func (s *Store) Use(id string) {
	s.mu.Lock()
	for i := range s.grants {
		if s.grants[i].ID == id {
			s.grants[i].Uses++
		}
	}
	s.mu.Unlock()
}

// Revoke removes grants by id ("" removes all); returns how many.
func (s *Store) Revoke(id string) int {
	s.mu.Lock()
	var keep []Grant
	n := 0
	for _, g := range s.grants {
		if id == "" || g.ID == id {
			n++
			continue
		}
		keep = append(keep, g)
	}
	s.grants = keep
	list := s.copyLocked()
	s.mu.Unlock()
	if n > 0 && s.OnChange != nil {
		s.OnChange(list)
	}
	return n
}

// FolderFor returns the active folder grant covering a path.
func (s *Store) FolderFor(p string) (Grant, bool) {
	p = filepath.Clean(p)
	for _, g := range s.Active(GrantFolder) {
		if p == g.Target || strings.HasPrefix(p, g.Target+"/") {
			return g, true
		}
	}
	return Grant{}, false
}

// Site returns the active grant of a host.
func (s *Store) Site(host string) (Grant, bool) {
	host = strings.ToLower(host)
	for _, g := range s.Active(GrantSite) {
		if g.Target == host {
			return g, true
		}
	}
	return Grant{}, false
}

// Mailbox returns the active grant of an account.
func (s *Store) Mailbox(account string) (Grant, bool) {
	for _, g := range s.Active(GrantMailbox) {
		if g.Target == account {
			return g, true
		}
	}
	return Grant{}, false
}
