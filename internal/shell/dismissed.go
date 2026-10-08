// SPDX-License-Identifier: Apache-2.0
package shell

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"sync"
	"time"

	"github.com/basalt-os/basalt-shell/internal/assistant"
	"github.com/basalt-os/basalt-shell/internal/paths"
)

// Dismissed reports. A report of the system assistant with nothing to
// apply (report_only: "Nothing will be changed") is only read; dismissing
// it is the person's own choice about their Activity list, not a change
// of the system, so it needs no administrator: the shell remembers the
// dismissed ids in the person's state directory and leaves the report out
// of what it shows. The assistant's own record (root's) is untouched, so
// `sudo basalt pending` still lists it until an administrator closes it.
// A proposal with changes is never dismissed this way: declining one
// still goes through `basalt ignore` with authentication.

const (
	dismissedFile = "dismissed-reports.json"
	// An id the assistant no longer lists is forgotten after this long
	// (ids are random; the file stays small).
	dismissedKeep = 90 * 24 * time.Hour
	dismissedMax  = 500
)

type dismissedStore struct {
	mu  sync.Mutex
	ids map[string]time.Time
	// loaded: read from disk once.
	loaded bool
}

func dismissedPath() string { return filepath.Join(paths.StateDir(), dismissedFile) }

func (d *dismissedStore) load() {
	if d.loaded {
		return
	}
	d.loaded = true
	d.ids = map[string]time.Time{}
	b, err := os.ReadFile(dismissedPath())
	if err != nil {
		return
	}
	var m map[string]time.Time
	if json.Unmarshal(b, &m) == nil {
		for id, t := range m {
			if reportID(id) {
				d.ids[id] = t
			}
		}
	}
}

func (d *dismissedStore) save() error {
	now := time.Now()
	for id, t := range d.ids {
		if now.Sub(t) > dismissedKeep {
			delete(d.ids, id)
		}
	}
	// Keep the newest dismissedMax ids.
	for len(d.ids) > dismissedMax {
		var oldID string
		var oldT time.Time
		for id, t := range d.ids {
			if oldID == "" || t.Before(oldT) {
				oldID, oldT = id, t
			}
		}
		delete(d.ids, oldID)
	}
	b, err := json.MarshalIndent(d.ids, "", "  ")
	if err != nil {
		return err
	}
	if err := os.MkdirAll(paths.StateDir(), 0o700); err != nil {
		return err
	}
	tmp := dismissedPath() + ".tmp"
	if err := os.WriteFile(tmp, b, 0o600); err != nil {
		return err
	}
	return os.Rename(tmp, dismissedPath())
}

func (d *dismissedStore) has(id string) bool {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.load()
	_, ok := d.ids[id]
	return ok
}

func (d *dismissedStore) add(id string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.load()
	d.ids[id] = time.Now().UTC()
	return d.save()
}

// reportID accepts the assistant's proposal ids (p-<hex>).
func reportID(id string) bool {
	if len(id) < 3 || len(id) > 40 || id[:2] != "p-" {
		return false
	}
	for _, r := range id[2:] {
		if !(r >= '0' && r <= '9' || r >= 'a' && r <= 'f') {
			return false
		}
	}
	return true
}

// withoutDismissed leaves out the reports the person dismissed; a
// proposal with changes always stays.
func withoutDismissed(ps []assistant.Proposal, dismissed func(string) bool) []assistant.Proposal {
	out := make([]assistant.Proposal, 0, len(ps))
	for _, p := range ps {
		if p.ReportOnly && dismissed(p.ID) {
			continue
		}
		out = append(out, p)
	}
	return out
}

// AssistantPending is the assistant's pending list as this person sees
// it: without the reports they dismissed.
func (c *Core) AssistantPending(ctx context.Context) ([]assistant.Proposal, error) {
	if c.Assistant == nil || !c.Assistant.Available() {
		return []assistant.Proposal{}, nil
	}
	ps, err := c.Assistant.Pending(ctx)
	if err != nil {
		return nil, err
	}
	return withoutDismissed(ps, c.dismissed.has), nil
}

// AssistantDismiss hides a report (nothing to apply) from this person's
// Activity list. No authentication: it changes only the person's own
// state. A proposal with changes is refused (decline it with Ignore).
func (c *Core) AssistantDismiss(ctx context.Context, id string) error {
	if !reportID(id) {
		return errors.New("invalid report id")
	}
	if c.Assistant == nil || !c.Assistant.Available() {
		return errors.New("the system assistant is not installed")
	}
	ps, err := c.Assistant.Pending(ctx)
	if err != nil {
		return err
	}
	return c.dismissIn(ps, id)
}

// dismissIn records the dismissal when id is a report in ps.
func (c *Core) dismissIn(ps []assistant.Proposal, id string) error {
	for _, p := range ps {
		if p.ID != id {
			continue
		}
		if !p.ReportOnly {
			return errors.New("this proposal changes the system: review it, then apply or ignore it")
		}
		if err := c.dismissed.add(id); err != nil {
			return err
		}
		_, _ = c.Audit.Append("decline", "ui", "dismissed report "+id+" from the Activity list", map[string]any{"assistant_proposal": id, "title": p.Title})
		return nil
	}
	// Already gone from the assistant's list: nothing to hide.
	return nil
}
