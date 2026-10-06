package shell

import (
	"context"
	"strings"
	"sync"
	"testing"
)

type powerRec struct {
	mu   sync.Mutex
	runs []string
}

func (r *powerRec) run(_ context.Context, argv []string) error {
	r.mu.Lock()
	r.runs = append(r.runs, strings.Join(argv, " "))
	r.mu.Unlock()
	return nil
}

// TestPowerFromRequest: "restart the computer" (and the Portuguese
// requests) is a proposal; nothing runs before the person confirms.
func TestPowerFromRequest(t *testing.T) {
	c, _, _ := newCore(t)
	rec := &powerRec{}
	c.PowerRun = rec.run
	ctx := context.Background()
	for req, want := range map[string]string{
		"restart the computer":   "systemctl reboot",
		"desligar":               "systemctl poweroff",
		"sair da sessão":         "loginctl terminate-session",
		"suspender o computador": "systemctl suspend",
	} {
		res := c.Ask(ctx, req)
		if res.Kind != "proposal" || res.Proposal == nil || len(res.Proposal.Calls) != 1 || res.Proposal.Calls[0].Action != "session.power" {
			t.Fatalf("%q: %+v", req, res)
		}
		if len(rec.runs) != 0 {
			t.Fatalf("%q ran before the confirmation: %v", req, rec.runs)
		}
		if _, err := c.Decide(ctx, res.Proposal.ID, true, "ui"); err != nil {
			t.Fatalf("%q: %v", req, err)
		}
		if len(rec.runs) != 1 || !strings.HasPrefix(rec.runs[0], want) {
			t.Fatalf("%q ran %v, want %s", req, rec.runs, want)
		}
		rec.runs = nil
	}
	// Declined: nothing runs.
	res := c.Ask(ctx, "shut down")
	if _, err := c.Decide(ctx, res.Proposal.ID, false, "ui"); err != nil {
		t.Fatal(err)
	}
	if len(rec.runs) != 0 {
		t.Fatalf("declined, ran %v", rec.runs)
	}
}

// TestPowerMenuAndAgents: the shell UI's power menu runs it directly
// (after its own confirmation); an agent can never propose it.
func TestPowerMenuAndAgents(t *testing.T) {
	c, fk, _ := newCore(t)
	rec := &powerRec{}
	c.PowerRun = rec.run
	ctx := context.Background()
	t.Setenv("XDG_SESSION_ID", "7")
	if _, err := c.Execute(ctx, "ui", []Call{{Action: "session.power", Args: map[string]any{"op": "logout"}}}); err != nil {
		t.Fatal(err)
	}
	if len(rec.runs) != 1 || rec.runs[0] != "loginctl terminate-session 7" {
		t.Fatalf("logout ran %v", rec.runs)
	}
	// Lock: basalt-lock through the compositor, as Super+L.
	if _, err := c.Execute(ctx, "ui", []Call{{Action: "session.power", Args: map[string]any{"op": "lock"}}}); err != nil {
		t.Fatal(err)
	}
	locked := false
	for _, call := range fk.Calls {
		locked = locked || strings.Contains(call, "basalt-lock")
	}
	if !locked {
		t.Errorf("lock: compositor calls %v", fk.Calls)
	}
	for _, origin := range []string{"agent", "mcp", "ipc"} {
		_, err := c.Propose(ctx, Meta{Origin: origin, Actor: origin}, []Call{{Action: "session.power", Args: map[string]any{"op": "poweroff"}}})
		if err == nil {
			t.Errorf("%s proposed a power off", origin)
		}
	}
	if _, err := c.Execute(ctx, "ui", []Call{{Action: "session.power", Args: map[string]any{"op": "format-disk"}}}); err == nil {
		t.Error("an unknown operation was accepted")
	}
	if len(rec.runs) != 1 {
		t.Errorf("ran %v", rec.runs)
	}
}
