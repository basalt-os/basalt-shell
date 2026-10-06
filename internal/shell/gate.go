package shell

// The approval gate (basalt-gate on Basalt OS, ADR 0020): the one place
// where a request for a side effect becomes a decision. When it is
// installed, the shell's proposals become gate requests:
//
//   - the daemon (basalt_shell_t, a trusted relay) proposes on behalf of
//     who asked: the person for the command bar and push to talk, an
//     agent for MCP and IPC clients; the preview is the shell's own plan;
//   - the person decides on the shell's sheets, but the decision goes from
//     the shell UI (basalt_shell_ui_t, the gate's desktop decider) straight
//     to the gate, never through this daemon; a rule may decide instead;
//   - the daemon waits for the decision, claims it with what it is about
//     to run (the gate checks it is what was approved), runs it and
//     reports the result.
//
// Which approval paths the gate decides is the gate's configuration
// (enforce); on the others the shell decides as before and tells the gate
// what was decided (shadow mode). Without a gate nothing changes.
// Agent control sessions and screenshots stay with the shell for now
// (only observed).

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/basalt-os/basalt-shell/internal/gateclient"
)

// Gate modes of an approval path.
const (
	gateAbsent  = ""
	gateObserve = "observe"
	gateEnforce = "enforce"
)

// GateInfo is a proposal's request in the approval gate.
type GateInfo struct {
	ID       string `json:"id"`
	Decision string `json:"decision"`
	By       string `json:"by,omitempty"`
	Class    string `json:"class,omitempty"`
	Reason   string `json:"reason,omitempty"`
	// Remember: the gate offers "Approve and remember" for this long.
	Remember string `json:"remember,omitempty"`
}

// gateState caches what hello said (the gate may be installed, started or
// reconfigured while the shell runs).
type gateState struct {
	mu      sync.Mutex
	at      time.Time
	present bool
	enforce []string
}

// GateStatus is what the UI needs to know about the gate.
type GateStatus struct {
	Present bool     `json:"present"`
	Enforce []string `json:"enforce,omitempty"`
	Socket  string   `json:"socket,omitempty"`
}

func (c *Core) gateSocket() string {
	if c.GateSocket != "" {
		return c.GateSocket
	}
	return gateclient.Socket()
}

// gateDial connects as a requester (and executor of the shell's actions).
func (c *Core) gateDial() (*gateclient.Client, error) {
	if c.GateOff || gateDisabled {
		return nil, gateclient.ErrAbsent
	}
	return gateclient.Detect(c.gateSocket(), "basalt-shell/"+Version, "requester")
}

// Gate reports whether the gate is present and which paths it decides
// (hello, cached for 10 seconds).
func (c *Core) Gate() GateStatus {
	g := &c.gate
	g.mu.Lock()
	defer g.mu.Unlock()
	if time.Since(g.at) > 10*time.Second {
		g.at = time.Now()
		g.present, g.enforce = false, nil
		if cl, err := c.gateDial(); err == nil {
			g.present, g.enforce = true, cl.Hello.Enforce
			cl.Close()
		}
	}
	st := GateStatus{Present: g.present, Enforce: append([]string(nil), g.enforce...)}
	if g.present {
		st.Socket = c.gateSocket()
	}
	return st
}

// gateMode is the mode of an approval path now.
func (c *Core) gateMode(path string) string {
	st := c.Gate()
	if !st.Present {
		return gateAbsent
	}
	for _, p := range st.Enforce {
		if p == path || p == "all" {
			return gateEnforce
		}
	}
	return gateObserve
}

// gatePath names the approval path of a proposal's calls (ADR 0020:
// shell for desktop and person actions, control for agent control and
// screenshots, which stay with the shell in this version).
func gatePath(calls []Call) string {
	for _, call := range calls {
		if call.Action == "agent.control" || call.Action == "screen.capture" {
			return "control"
		}
	}
	return "shell"
}

// personOrigin: the person's own words (the command bar, push to talk).
func personOrigin(origin string) bool {
	return origin == "commandbar" || origin == "voice" || origin == "ui"
}

// gateRequest is the proposal as a gate request.
func (c *Core) gateRequest(pr *Proposal) gateclient.Proposal {
	ob := &gateclient.OnBehalf{Kind: "person"}
	if !personOrigin(pr.Origin) {
		ob = &gateclient.OnBehalf{Kind: "agent", Name: agentName(pr.Actor)}
	}
	pv := pr.gatePreview()
	return gateclient.Proposal{Calls: gateCalls(pr.Calls), Preview: &pv, OnBehalf: ob}
}

// agentName is the client's own label from the actor ("agent:NAME (type)").
func agentName(actor string) string {
	s := strings.TrimPrefix(actor, "agent:")
	if i := strings.IndexByte(s, ' '); i >= 0 {
		s = s[:i]
	}
	return clipStr(s, 60)
}

// gateCalls are the calls in the gate's form: numbers that are not whole
// become strings (the gate's canonical JSON has integers only), and a few
// shell actions have a gate name of their own (gateActionOf).
func gateCalls(calls []Call) []gateclient.Call {
	out := make([]gateclient.Call, 0, len(calls))
	for _, call := range calls {
		args, _ := gateValue(call.Args).(map[string]any)
		if args == nil {
			args = map[string]any{}
		}
		action, args := gateActionOf(call.Action, args)
		out = append(out, gateclient.Call{Action: action, Args: args})
	}
	return out
}

// gateActionOf maps a shell action to its gate action (identity unless a
// registry splits it).
var gateActionOf = func(action string, args map[string]any) (string, map[string]any) { return action, args }

// gateValue converts a decoded JSON value to the gate's canonical subset.
func gateValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		m := make(map[string]any, len(x))
		for k, e := range x {
			m[k] = gateValue(e)
		}
		return m
	case []any:
		l := make([]any, len(x))
		for i, e := range x {
			l[i] = gateValue(e)
		}
		return l
	case []string:
		l := make([]any, len(x))
		for i, e := range x {
			l[i] = e
		}
		return l
	case float64:
		if x == math.Trunc(x) && math.Abs(x) < 1<<53 {
			return int64(x)
		}
		return strconv.FormatFloat(x, 'g', -1, 64)
	case float32:
		return gateValue(float64(x))
	case int:
		return int64(x)
	case json.Number:
		if _, err := x.Int64(); err == nil {
			return x
		}
		return x.String()
	case nil, bool, string, int64:
		return x
	}
	// Anything else (a typed slice or struct): through JSON.
	b, err := json.Marshal(v)
	if err != nil {
		return fmt.Sprint(v)
	}
	var w any
	if json.Unmarshal(b, &w) != nil {
		return string(b)
	}
	return gateValue(w)
}

func clipStr(s string, n int) string {
	if len(s) > n {
		return s[:n]
	}
	return s
}

// gatePreview is the shell's plan as the gate keeps it: the summary, one
// line per step, the exact previews of acting steps (long texts clipped:
// the calls carry them whole) and the token diff of theme changes.
func (pr *Proposal) gatePreview() gateclient.Preview {
	pv := gateclient.Preview{TitleKey: "shell.proposal.title",
		TitleArgs: map[string]any{"summary": clipStr(pr.summary(), 300), "origin": pr.Origin}}
	for _, s := range pr.Steps {
		pv.Lines = append(pv.Lines, gateclient.Line{Key: "shell.step", Args: map[string]any{"text": clipStr(s, 500)}})
	}
	for _, p := range pr.Previews {
		args, _ := gateValue(clipValues(p)).(map[string]any)
		pv.Lines = append(pv.Lines, gateclient.Line{Key: "shell.preview", Args: args})
	}
	if len(pr.Diff) > 0 {
		if b, err := json.Marshal(pr.Diff); err == nil {
			pv.Diff = clipStr(string(b), 8000)
		}
	}
	return pv
}

// clipValues clips long strings in a preview map.
func clipValues(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		if s, ok := v.(string); ok {
			v = clipStr(s, 2000)
		}
		out[k] = v
	}
	return out
}

// gateSubmit sends a stored proposal to the gate (enforce mode) and keeps
// the answer in pr.Gate; gateAct then acts on it (after the proposal was
// shown, so the UI sees it pending before it ends).
func (c *Core) gateSubmit(pr *Proposal) error {
	cl, err := c.gateDial()
	if err != nil {
		return fmt.Errorf("the approval gate did not answer: %w", err)
	}
	defer cl.Close()
	rep, err := cl.Propose(c.gateRequest(pr))
	if err != nil {
		return fmt.Errorf("the approval gate: %w", err)
	}
	switch rep.Decision {
	case gateclient.Refused, gateclient.Allowed, gateclient.Asked:
	default:
		return fmt.Errorf("the approval gate answered %q", rep.Decision)
	}
	info := &GateInfo{ID: rep.ID, Decision: rep.Decision, By: rep.By, Class: rep.Class, Reason: rep.Reason}
	if rep.Decision == gateclient.Asked {
		if st, err := cl.Status(rep.ID); err == nil && st.Request != nil {
			info.Remember = st.Request.Remember
		}
	}
	c.mu.Lock()
	pr.Gate = info
	c.mu.Unlock()
	_, _ = c.Audit.Append("gate", pr.Actor, pr.summary(), map[string]any{"proposal": pr.ID, "gate_id": rep.ID,
		"decision": rep.Decision, "by": rep.By, "class": rep.Class, "reason": rep.Reason})
	return nil
}

// gateAct acts on the gate's first answer: refused ends the proposal,
// allowed (a rule) runs it, asked waits for the person in the background.
func (c *Core) gateAct(pr *Proposal) {
	c.mu.Lock()
	g := *pr.Gate
	c.mu.Unlock()
	switch g.Decision {
	case gateclient.Refused:
		c.finish(pr, StatusRefused, gateBy(g.By), g.Reason)
	case gateclient.Allowed:
		go c.gateRun(pr, g.ID, g.By)
	case gateclient.Asked:
		go c.gateWait(pr, g.ID)
	}
}

// gateBy names who decided at the gate for the activity log.
func gateBy(by string) string {
	if by == "" {
		return "gate"
	}
	return "gate:" + by
}

// gateWait follows a request until it is decided, then runs or ends the
// proposal.
func (c *Core) gateWait(pr *Proposal, id string) {
	for {
		select {
		case <-pr.done:
			return
		default:
		}
		cl, err := c.gateDial()
		if err != nil {
			c.finish(pr, StatusFailed, "gate", "the approval gate went away before a decision")
			return
		}
		rep, err := cl.Wait(id, 60)
		cl.Close()
		if err != nil {
			c.finish(pr, StatusFailed, "gate", "the approval gate: "+err.Error())
			return
		}
		c.mu.Lock()
		current := pr.Gate != nil && pr.Gate.ID == id
		if current {
			g := *pr.Gate
			g.Decision, g.By = rep.Decision, rep.By
			pr.Gate = &g
		}
		c.mu.Unlock()
		if !current {
			return // replaced (an edit, a confirmation refused after agent input)
		}
		switch rep.Decision {
		case gateclient.Asked:
			continue
		case gateclient.Allowed:
			c.gateRun(pr, id, rep.By)
		case gateclient.Declined:
			c.finish(pr, StatusDeclined, gateBy(rep.By), "")
		case gateclient.Expired:
			c.finish(pr, StatusExpired, gateBy(rep.By), "nobody confirmed it in time")
		case gateclient.Cancelled:
			c.finish(pr, StatusDeclined, gateBy(rep.By), "withdrawn")
		default:
			c.finish(pr, StatusRefused, gateBy(rep.By), rep.Reason)
		}
		return
	}
}

// gateRun runs a proposal the gate allowed: a person's approval right
// after synthetic input is not trusted (the request is asked again); the
// claim proves that what runs is what was approved; the result goes back.
func (c *Core) gateRun(pr *Proposal, id, by string) {
	if strings.HasPrefix(by, "person:") && c.recentInput() {
		_, _ = c.Audit.Append("refuse", gateBy(by), "confirmation right after synthetic input ignored", map[string]any{"proposal": pr.ID, "gate_id": id})
		if err := c.gateSubmit(pr); err != nil {
			c.finish(pr, StatusFailed, "gate", err.Error())
			return
		}
		c.Broadcast("proposal", pr.public())
		c.gateAct(pr)
		return
	}
	cl, err := c.gateDial()
	if err != nil {
		c.finish(pr, StatusFailed, "gate", "the approval gate went away before the claim")
		return
	}
	defer cl.Close()
	gr := c.gateRequest(pr)
	if _, err := cl.ClaimCalls(id, gr.Calls, gr.Preview, "basalt-shell"); err != nil {
		c.finish(pr, StatusStale, gateBy(by), "the approval gate refused the claim: "+err.Error())
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	_, runErr := c.execute(ctx, pr, gateBy(by))
	ok, exit, detail := runErr == nil, 0, "done"
	if runErr != nil {
		exit, detail = 1, runErr.Error()
	}
	if _, err := cl.Result(id, ok, exit, clipStr(detail, 400)); err != nil {
		_, _ = c.Audit.Append("fail", "gate", "result not reported to the approval gate: "+err.Error(), map[string]any{"proposal": pr.ID})
	}
}

// gateCancel withdraws the request of a proposal that ended here.
func (c *Core) gateCancel(id string) {
	if id == "" {
		return
	}
	go func() {
		if cl, err := c.gateDial(); err == nil {
			_, _ = cl.Cancel(id)
			cl.Close()
		}
	}()
}

// gateObserve tells the gate what the shell decided by itself (shadow
// mode). Best effort, in the background.
func (c *Core) gateObserve(pr *Proposal, status, by string) {
	outcome := map[string]string{StatusApplied: "approved", StatusFailed: "approved", StatusDeclined: "declined",
		StatusExpired: "expired", StatusStale: "refused", StatusRefused: "refused"}[status]
	if outcome == "" {
		return
	}
	req := c.gateRequest(pr)
	go func() {
		if cl, err := c.gateDial(); err == nil {
			_, _ = cl.Observe(req, outcome, "the shell ("+orNone(by)+")")
			cl.Close()
		}
	}()
}

// errGateDecides: a proposal the gate decides is not confirmed through
// the daemon: the shell UI decides at the gate.
var errGateDecides = errors.New("this request is decided in the approval gate: the shell UI sends the decision there")

// BASALT_SHELL_GATE=off makes the shell ignore the gate (development).
func init() {
	if os.Getenv("BASALT_SHELL_GATE") == "off" {
		gateDisabled = true
	}
}

var gateDisabled bool
