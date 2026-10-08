package shell

// Questions from apps (Security and Activity's "Ask the assistant", "Run
// audit", "Check now"): an app asks the shell to open the command bar with
// a question and run it. Opening the command bar changes nothing, so it
// needs no confirmation; what the question may do is narrow:
//
//   - a fixed system request (appSystemArgs: the assistant's read commands,
//     storing one of its proposals, or a check for updates), or else
//   - the person's words through the system assistant's own translator,
//     which only ever runs read commands.
//
// It is never planned as the person's own words: no desktop action, no
// read-only skill, no Person action (mail, typing, moving files) can come
// from an app's text. A proposal the assistant stores still needs the
// person's Apply and the approval gate, exactly as from the command bar.
// When the person edits the text before running it, it is their own
// request again (the ordinary "ask").

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"regexp"
	"strings"
	"time"
	"unicode"

	"github.com/basalt-os/basalt-shell/internal/assistant"
	"github.com/basalt-os/basalt-shell/internal/i18n"
)

// appAsk is a question an app asked, waiting for the command bar to run it.
type appAsk struct {
	ID      string
	Text    string
	System  []string
	From    string // the app's name, for the command bar
	Actor   string
	Created time.Time
}

// appAskTTL is how long the command bar may take to run a question.
const appAskTTL = 2 * time.Minute

// appAskGap is the least time between two questions of the same app.
const appAskGap = time.Second

var reRiskItem = regexp.MustCompile(`^(encryption|secure_boot|tpm|audit|ledger)$`)

// appSystemArgs checks the fixed system requests an app may ask for.
func appSystemArgs(args []string) error {
	j := strings.Join(args, " ")
	switch j {
	case "status", "snapshots", "disk", "pending", "fix selinux", "updates", "updates check",
		"security risks", "security audit":
		return nil
	}
	if len(args) == 3 && args[0] == "security" && (args[1] == "accept" || args[1] == "review") && reRiskItem.MatchString(args[2]) {
		return nil
	}
	return fmt.Errorf("not a request an app may ask for: %s", j)
}

// cleanQuestion keeps an app's question to one short printable line.
func cleanQuestion(s string) (string, error) {
	s = strings.TrimSpace(s)
	if s == "" || len(s) > 300 {
		return "", errors.New("the question must have 1 to 300 characters")
	}
	for _, r := range s {
		if !unicode.IsPrint(r) {
			return "", errors.New("the question must be one line of printable text")
		}
	}
	return s, nil
}

// appName is the name people know an app by: its desktop entry's, found
// by the client name it gave (the program name). Only a label; the
// activity log keeps the kernel's facts (pid, SELinux domain).
func (c *Core) appName(client string) string {
	if client != "" {
		for _, a := range c.Apps() {
			f := strings.Fields(a.Exec)
			for _, w := range f {
				if filepath.Base(w) == client {
					return a.Name
				}
			}
			if strings.TrimSuffix(a.ID, ".desktop") == client {
				return a.Name
			}
		}
	}
	return i18n.G("An app")
}

// requester says who asks for a proposal in plain words: a Basalt app
// (its SELinux domain basalt_app_*_t, or a client name of an installed
// app) by its name, an agent by the name it gave. The command bar and
// the UI are the person and need no name.
func (c *Core) requester(m Meta) (from, kind string) {
	switch m.Origin {
	case "commandbar", "voice", "ui":
		return "", ""
	}
	if strings.HasPrefix(m.Domain, "basalt_app_") {
		return c.appName(m.Client), "app"
	}
	if m.Client != "" {
		if n := c.appName(m.Client); n != i18n.G("An app") {
			return n, "app"
		}
		return m.Client, "agent"
	}
	return i18n.G("an AI agent"), "agent"
}

// AskOpen stores an app's question and asks the shell UI to open the
// command bar with it. Nothing runs until the command bar runs it.
func (c *Core) AskOpen(m Meta, client, text string, system []string) (map[string]any, error) {
	q, err := cleanQuestion(text)
	if err != nil {
		return nil, err
	}
	if len(system) > 0 {
		if err := appSystemArgs(system); err != nil {
			_, _ = c.Audit.Append("refuse", m.Actor, "app question refused: "+err.Error(), map[string]any{"system": system, "pid": m.PID, "domain": m.Domain})
			return nil, err
		}
	}
	if c.Locked() {
		return nil, errors.New("the screen is locked")
	}
	now := time.Now()
	c.mu.Lock()
	if c.appAsks == nil {
		c.appAsks = map[string]*appAsk{}
	}
	for id, a := range c.appAsks {
		if now.Sub(a.Created) > appAskTTL {
			delete(c.appAsks, id)
			continue
		}
		if a.Actor == m.Actor && now.Sub(a.Created) < appAskGap {
			c.mu.Unlock()
			return nil, errors.New("one question at a time; ask again in a moment")
		}
	}
	a := &appAsk{ID: newID(), Text: q, System: append([]string(nil), system...), Actor: m.Actor, Created: now}
	c.appAsks[a.ID] = a
	c.mu.Unlock()
	a.From = c.appName(client)
	_, _ = c.Audit.Append("ask", m.Actor, "asks the command bar: "+q, map[string]any{"app_ask": a.ID, "system": system, "from": a.From, "pid": m.PID, "domain": m.Domain})
	c.Broadcast("ask", map[string]any{"id": a.ID, "text": q, "from": a.From})
	return map[string]any{"id": a.ID}, nil
}

// takeAppAsk returns a stored question once.
func (c *Core) takeAppAsk(id string) (*appAsk, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	a, ok := c.appAsks[id]
	if !ok || time.Since(a.Created) > appAskTTL {
		delete(c.appAsks, id)
		return nil, false
	}
	delete(c.appAsks, id)
	return a, true
}

// AskApp runs an app's question in the command bar (the shell UI calls it
// when the person did not change the text).
func (c *Core) AskApp(ctx context.Context, id string) AskResult {
	a, ok := c.takeAppAsk(id)
	if !ok {
		return AskResult{Kind: "error", Error: i18n.G("This question expired. Ask it again from the app.")}
	}
	res := AskResult{Request: a.Text, Backend: "app"}
	_, _ = c.Audit.Append("ask", "commandbar", a.Text, map[string]any{"app_ask": a.ID, "from": a.From, "actor": a.Actor, "system": a.System})
	if c.Assistant == nil || !c.Assistant.Available() {
		res.Kind, res.Error = "error", i18n.G("The system assistant (basalt) is not installed on this computer.")
		return res
	}
	sys := a.System
	if len(sys) == 0 {
		// The assistant's own translator picks a read command.
		out, err := c.Assistant.Ask(ctx, a.Text)
		if args := assistant.Understood(out); len(args) > 0 && err == nil {
			sys = args
			res.Explain = append(res.Explain, "system assistant: basalt "+strings.Join(args, " "))
		} else {
			res.Kind, res.Text = "system", strings.TrimSpace(out)
			if res.Text == "" && err != nil {
				res.Kind, res.Error = "error", err.Error()
			}
			return res
		}
	}
	if strings.Join(sys, " ") == "updates check" {
		return c.appUpdatesCheck(ctx, res)
	}
	return c.systemAnswer(ctx, res, sys)
}

// systemAnswer runs a read-only assistant request and attaches the
// proposal it stored, if any (the command bar offers Apply for it).
func (c *Core) systemAnswer(ctx context.Context, res AskResult, args []string) AskResult {
	res.Kind = "system"
	out, err := c.Assistant.Read(ctx, args)
	res.Text = out
	if err != nil && strings.TrimSpace(out) == "" {
		res.Kind, res.Error = "error", err.Error()
		return res
	}
	if id, code := assistant.FindProposal(out); id != "" {
		p := assistant.Proposal{ID: id, Code: code, Report: out}
		if full, err := c.Assistant.Show(ctx, id); err == nil {
			p = full
			if p.Code == "" {
				p.Code = code
			}
		}
		res.Assistant = &p
	}
	return res
}

// appUpdatesCheck is "Check now": update.check refreshes the package lists
// (nothing is installed; the same check as Settings, Updates).
func (c *Core) appUpdatesCheck(ctx context.Context, res AskResult) AskResult {
	res.Kind = "system"
	raw, err := c.Assistant.UpdatesCheck(ctx)
	data := map[string]any{"action": "update.check", "from": "app"}
	if err != nil {
		data["error"] = err.Error()
	}
	_, _ = c.Audit.Append("ask", "commandbar", "check for updates", data)
	if err != nil {
		res.Kind, res.Error = "error", err.Error()
		return res
	}
	var r struct {
		Counts map[string]int `json:"counts"`
		Errors []string       `json:"errors"`
	}
	_ = json.Unmarshal(raw, &r)
	n, sec := r.Counts["total"], r.Counts["security"]
	switch {
	case len(r.Errors) > 0:
		res.Text = i18n.G("The check for updates could not reach every repository: %s", strings.Join(r.Errors, "; "))
	case n == 0:
		res.Text = i18n.G("Checked: the system is up to date.")
	default:
		res.Text = i18n.N("Checked: %d update is waiting (%d for security). Install it from Settings, Updates.",
			"Checked: %d updates are waiting (%d for security). Install them from Settings, Updates.", n, n, sec)
	}
	return res
}
