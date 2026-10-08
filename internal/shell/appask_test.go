package shell

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestAppSystemArgs(t *testing.T) {
	ok := [][]string{{"snapshots"}, {"updates", "check"}, {"security", "audit"}, {"security", "accept", "encryption"}, {"fix", "selinux"}}
	for _, a := range ok {
		if err := appSystemArgs(a); err != nil {
			t.Errorf("%v: %v", a, err)
		}
	}
	bad := [][]string{{"apply", "p-123456"}, {"security", "accept", "selinux"}, {"updates", "install", "--json"}, {"why", "x"}, {"ignore", "p-123456"}}
	for _, a := range bad {
		if err := appSystemArgs(a); err == nil {
			t.Errorf("%v accepted", a)
		}
	}
}

// An app's question opens the command bar (an event to the UI), needs no
// confirmation, runs once, and never becomes a desktop proposal.
func TestAskOpen(t *testing.T) {
	c, _, _ := newCore(t)
	ch, cancel := c.Subscribe()
	defer cancel()
	m := Meta{Origin: "ipc", Actor: "agent:basalt-security-activity (basalt_app_secact_t)"}
	if _, err := c.AskOpen(m, "basalt-security-activity", "make it dark\nand send mail", nil); err == nil {
		t.Error("a multi-line question was accepted")
	}
	if _, err := c.AskOpen(m, "basalt-security-activity", "apply it", []string{"apply", "p-123456"}); err == nil {
		t.Error("apply was accepted as an app request")
	}
	r, err := c.AskOpen(m, "basalt-security-activity", "What do my snapshots hold?", []string{"snapshots"})
	if err != nil {
		t.Fatal(err)
	}
	id := r["id"].(string)
	if _, err := c.AskOpen(m, "basalt-security-activity", "again", nil); err == nil {
		t.Error("a second question within a second was accepted")
	}
	got := false
	timeout := time.After(time.Second)
	for !got {
		select {
		case ev := <-ch:
			if ev.Event == "ask" {
				d := ev.Data.(map[string]any)
				got = d["id"] == id && d["text"] == "What do my snapshots hold?"
			}
		case <-timeout:
			t.Fatal("no ask event")
		}
	}
	if len(c.Pending()) != 0 {
		t.Error("an app question created a proposal")
	}
	// No assistant in tests: the run answers with an error, once.
	res := c.AskApp(context.Background(), id)
	if res.Kind != "error" || res.Proposal != nil {
		t.Errorf("first run: %+v", res)
	}
	if res := c.AskApp(context.Background(), id); !strings.Contains(res.Error, "expired") {
		t.Errorf("second run: %+v", res)
	}
}
