package shell

import (
	"bufio"
	"context"
	"encoding/json"
	"errors"
	"net"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/basalt-os/basalt-shell/internal/gateclient"
)

// fakeGate is a minimal approval gate: hello says what it enforces,
// propose answers with the decision set for the action (asked by default),
// wait blocks until the test decides, claim checks nothing but records,
// result, cancel and observe are recorded.
type fakeGate struct {
	mu       sync.Mutex
	enforce  []string
	decision map[string]string // action -> allowed, refused, asked
	reqs     []gateclient.Request
	waiting  map[string]chan string
	n        int
}

func newFakeGate(t *testing.T, enforce ...string) (*fakeGate, string) {
	t.Helper()
	f := &fakeGate{enforce: enforce, decision: map[string]string{}, waiting: map[string]chan string{}}
	sock := filepath.Join(t.TempDir(), "gate.sock")
	ln, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { ln.Close() })
	go func() {
		for {
			c, err := ln.Accept()
			if err != nil {
				return
			}
			go f.serve(c)
		}
	}()
	return f, sock
}

func (f *fakeGate) serve(c net.Conn) {
	defer c.Close()
	br := bufio.NewReader(c)
	for {
		line, err := br.ReadBytes('\n')
		if err != nil {
			return
		}
		var req gateclient.Request
		_ = json.Unmarshal(line, &req)
		f.mu.Lock()
		f.reqs = append(f.reqs, req)
		rep := gateclient.Reply{OK: true}
		var wait chan string
		switch req.Op {
		case "hello":
			rep.Protocol, rep.Enforce = gateclient.Protocol, f.enforce
		case "propose":
			f.n++
			rep.ID = "g-00000000000" + string(rune('0'+f.n))
			rep.Decision = gateclient.Asked
			if d, ok := f.decision[req.Calls[0].Action]; ok {
				rep.Decision = d
			}
			if rep.Decision == gateclient.Allowed {
				rep.By = "rule:r-test@abc"
			}
			f.waiting[rep.ID] = make(chan string, 1)
		case "status":
			rep.Request = &gateclient.View{ID: req.ID, Remember: "8h"}
		case "wait":
			wait = f.waiting[req.ID]
		case "claim", "result", "cancel", "observe":
		}
		f.mu.Unlock()
		if wait != nil {
			select {
			case d := <-wait:
				rep.ID, rep.Decision, rep.By = req.ID, d, "person:shell"
			case <-time.After(time.Duration(req.Timeout) * time.Second):
				rep.ID, rep.Decision, rep.TimedOut = req.ID, gateclient.Asked, true
			}
		}
		b, _ := json.Marshal(rep)
		if _, err := c.Write(append(b, '\n')); err != nil {
			return
		}
	}
}

// decide is the person's decision on the sheet, sent to the gate.
func (f *fakeGate) decide(id, decision string) {
	f.mu.Lock()
	ch := f.waiting[id]
	f.mu.Unlock()
	ch <- decision
}

func (f *fakeGate) find(op string) []gateclient.Request {
	f.mu.Lock()
	defer f.mu.Unlock()
	var out []gateclient.Request
	for _, r := range f.reqs {
		if r.Op == op {
			out = append(out, r)
		}
	}
	return out
}

func waitStatus(t *testing.T, c *Core, id string) Proposal {
	t.Helper()
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	p, err := c.Wait(ctx, id)
	if err != nil {
		t.Fatalf("wait %s: %v (%s)", id, err, p.Status)
	}
	return p
}

func eventually(t *testing.T, what string, f func() bool) {
	t.Helper()
	for i := 0; i < 300; i++ {
		if f() {
			return
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal(what)
}

// No gate: the shell decides as it always did.
func TestGateAbsentNothingChanges(t *testing.T) {
	c, _, _ := newCore(t)
	res := c.Ask(context.Background(), "dark mode")
	if res.Proposal == nil || res.Proposal.GateMode != "" || res.Proposal.Gate != nil {
		t.Fatalf("%+v", res.Proposal)
	}
	if p, err := c.Decide(context.Background(), res.Proposal.ID, true, "ui"); err != nil || p.Status != StatusApplied {
		t.Fatalf("%v %s", err, p.Status)
	}
}

// Shadow mode: the sheet decides as before; the gate is told.
func TestGateObserves(t *testing.T) {
	c, _, _ := newCore(t)
	f, sock := newFakeGate(t, "skills")
	c.GateSocket = sock
	res := c.Ask(context.Background(), "dark mode")
	if res.Proposal == nil || res.Proposal.GateMode != gateObserve {
		t.Fatalf("%+v", res.Proposal)
	}
	if p, err := c.Decide(context.Background(), res.Proposal.ID, true, "ui"); err != nil || p.Status != StatusApplied {
		t.Fatalf("%v %s", err, p.Status)
	}
	eventually(t, "no observe record", func() bool { return len(f.find("observe")) == 1 })
	o := f.find("observe")[0]
	if o.Outcome != "approved" || o.OnBehalf == nil || o.OnBehalf.Kind != "person" || len(f.find("propose")) != 0 {
		t.Fatalf("%+v", o)
	}
}

// Enforced: the daemon cannot decide; the person's decision at the gate
// runs it, after a claim with exactly the calls that were proposed.
func TestGateDecidesAnAgentsRequest(t *testing.T) {
	c, _, _ := newCore(t)
	f, sock := newFakeGate(t, "shell")
	c.GateSocket = sock
	ctx := context.Background()
	calls := []Call{{Action: "theme.set_tokens", Args: map[string]any{"tokens": map[string]any{"window.dimInactive": 0.25, "radius.window": 14.0}}}}
	pr, err := c.Propose(ctx, Meta{Origin: "mcp", Actor: "agent:mcp-claude (basalt_agent_mcp_t)"}, calls)
	if err != nil {
		t.Fatal(err)
	}
	if pr.Gate == nil || pr.Gate.ID == "" || pr.Gate.Decision != gateclient.Asked || pr.Gate.Remember != "8h" {
		t.Fatalf("gate info: %+v", pr.Gate)
	}
	prop := f.find("propose")[0]
	if prop.OnBehalf == nil || prop.OnBehalf.Kind != "agent" || prop.OnBehalf.Name != "mcp-claude" || prop.Preview == nil {
		t.Fatalf("request: %+v", prop)
	}
	// Whole numbers stay numbers, fractions travel as text (canonical JSON).
	toks := prop.Calls[0].Args["tokens"].(map[string]any)
	if toks["window.dimInactive"] != "0.25" || toks["radius.window"] != float64(14) {
		t.Fatalf("args: %#v", toks)
	}
	// The daemon refuses a decision for it: only the UI, at the gate.
	if _, err := c.Decide(ctx, pr.ID, true, "ui"); !errors.Is(err, errGateDecides) {
		t.Fatalf("daemon decided: %v", err)
	}
	f.decide(pr.Gate.ID, gateclient.Allowed)
	p := waitStatus(t, c, pr.ID)
	if p.Status != StatusApplied || p.Decided != "gate:person:shell" {
		t.Fatalf("%s by %s: %s", p.Status, p.Decided, p.Error)
	}
	claims := f.find("claim")
	if len(claims) != 1 || claims[0].ID != pr.Gate.ID || claims[0].Executor != "basalt-shell" {
		t.Fatalf("claims: %+v", claims)
	}
	if b1, _ := json.Marshal(claims[0].Calls); string(b1) != mustJSON(prop.Calls) {
		t.Errorf("claimed %s, proposed %s", b1, mustJSON(prop.Calls))
	}
	eventually(t, "no result reported", func() bool { return len(f.find("result")) == 1 })
}

func mustJSON(v any) string { b, _ := json.Marshal(v); return string(b) }

// Declined at the gate: nothing runs.
func TestGateDecline(t *testing.T) {
	c, _, _ := newCore(t)
	f, sock := newFakeGate(t, "shell")
	c.GateSocket = sock
	before := c.Theme().Tokens.Str("color.bg")
	res := c.Ask(context.Background(), "dark mode")
	f.decide(res.Proposal.Gate.ID, gateclient.Declined)
	if p := waitStatus(t, c, res.Proposal.ID); p.Status != StatusDeclined {
		t.Fatalf("%s", p.Status)
	}
	if c.Theme().Tokens.Str("color.bg") != before || len(f.find("claim")) != 0 {
		t.Fatal("something ran")
	}
}

// A rule allows it at once (no sheet); a refusal ends it with the reason.
func TestGateRuleAndRefusal(t *testing.T) {
	c, _, _ := newCore(t)
	f, sock := newFakeGate(t, "all")
	c.GateSocket = sock
	f.decision["window.focus"] = gateclient.Allowed
	f.decision["session.power"] = gateclient.Refused
	pr, err := c.Propose(context.Background(), Meta{Origin: "mcp", Actor: "agent:x"}, []Call{{Action: "window.focus", Args: map[string]any{"window": "focused"}}})
	if err != nil {
		t.Fatal(err)
	}
	if p := waitStatus(t, c, pr.ID); p.Status != StatusApplied || !strings.HasPrefix(p.Decided, "gate:rule:") {
		t.Fatalf("%s %s %s", p.Status, p.Decided, p.Error)
	}
	// The person's own words: on behalf of the person.
	res := c.Ask(context.Background(), "lock the screen")
	if res.Proposal == nil {
		t.Fatalf("%+v", res)
	}
	if p := waitStatus(t, c, res.Proposal.ID); p.Status != StatusRefused {
		t.Fatalf("%s", p.Status)
	}
	last := f.find("propose")
	if ob := last[len(last)-1].OnBehalf; ob == nil || ob.Kind != "person" {
		t.Fatalf("%+v", ob)
	}
}

// Agent control sessions stay with the shell in this version (observed).
func TestGatePathOfCalls(t *testing.T) {
	for calls, want := range map[string]string{"agent.control": "control", "screen.capture": "control", "theme.switch": "shell", "session.power": "shell"} {
		if got := gatePath([]Call{{Action: calls}}); got != want {
			t.Errorf("%s: %s, want %s", calls, got, want)
		}
	}
}
