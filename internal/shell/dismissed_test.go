// SPDX-License-Identifier: Apache-2.0
package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basalt-os/basalt-shell/internal/assistant"
)

// Dismissing a report (nothing to apply) is the person's own state: no
// pkexec, remembered across restarts, and a proposal with changes is
// never hidden this way.
func TestDismissReportIsPersonalState(t *testing.T) {
	state := t.TempDir()
	t.Setenv("BASALT_SHELL_STATE_DIR", state)
	c, _, dir := newCore(t)
	ps := []assistant.Proposal{
		{ID: "p-e1725f", Title: "SELinux blocked something", ReportOnly: true},
		{ID: "p-0a0b0c", Title: "Turn a channel on", ReportOnly: false},
	}
	if err := c.dismissIn(ps, "p-0a0b0c"); err == nil {
		t.Fatal("a proposal with changes was dismissed")
	}
	if err := c.dismissIn(ps, "p-e1725f"); err != nil {
		t.Fatal(err)
	}
	got := withoutDismissed(ps, c.dismissed.has)
	if len(got) != 1 || got[0].ID != "p-0a0b0c" {
		t.Fatalf("after dismiss: %+v", got)
	}
	b, err := os.ReadFile(filepath.Join(state, dismissedFile))
	if err != nil || !strings.Contains(string(b), "p-e1725f") {
		t.Fatalf("not saved: %v %q", err, b)
	}
	// A new daemon (restart) still leaves it out.
	var fresh dismissedStore
	if !fresh.has("p-e1725f") || fresh.has("p-0a0b0c") {
		t.Fatal("dismissed ids not read back")
	}
	// The activity log records it as the person's decision.
	log, _ := os.ReadFile(filepath.Join(dir, "audit.jsonl"))
	if !strings.Contains(string(log), "dismissed report p-e1725f") {
		t.Fatalf("not audited: %s", log)
	}
	// Even if a proposal id were in the file, a proposal stays listed.
	if err := fresh.add("p-0a0b0c"); err != nil {
		t.Fatal(err)
	}
	if got := withoutDismissed(ps, fresh.has); len(got) != 1 || got[0].ID != "p-0a0b0c" {
		t.Fatalf("proposal hidden: %+v", got)
	}
}

func TestReportID(t *testing.T) {
	for id, want := range map[string]bool{"p-e1725f": true, "p-8bd6c8": true, "": false, "p-": false, "x-123": false, "p-../x": false, "p-ABC": false} {
		if reportID(id) != want {
			t.Errorf("reportID(%q) = %v", id, !want)
		}
	}
}
