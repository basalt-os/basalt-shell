package shell

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/basalt-os/basalt-shell/internal/assistant"
	"github.com/basalt-os/basalt-shell/internal/i18n"
	"github.com/basalt-os/basalt-shell/internal/intent"
	"github.com/basalt-os/basalt-shell/internal/skills"
)

// AskResult is what the command bar shows for a request.
type AskResult struct {
	Kind      string              `json:"kind"` // proposal, system, unknown, error
	Request   string              `json:"request"`
	Proposal  *Proposal           `json:"proposal,omitempty"`
	Text      string              `json:"text,omitempty"` // assistant output
	Assistant *assistant.Proposal `json:"assistant,omitempty"`
	Explain   []string            `json:"explain,omitempty"`
	Unknown   []string            `json:"unknown,omitempty"`
	Backend   string              `json:"backend"`
	Error     string              `json:"error,omitempty"`
	Clarify   []string            `json:"clarify,omitempty"`
	Model     *intent.ModelInfo   `json:"model,omitempty"`
	// Skill is a read-only skill's answer (files, e-mail, web page).
	Skill *skills.Answer `json:"skill,omitempty"`
	// Retry is the request to run again once the proposal (a grant the
	// skill needs) is applied.
	Retry string `json:"retry,omitempty"`
}

// Ask understands a command-bar request: the fixed phrases (rules) when
// they understand all of it, else the local language model when one is
// configured (constrained to the closed set of desktop intents and
// grounded in the request), else what the rules understood. Desktop actions become a
// proposal; system questions go to the system assistant (its own
// translator, `basalt ask`, then its read commands). Nothing is applied
// here.
func (c *Core) Ask(ctx context.Context, text string) AskResult {
	text = strings.TrimSpace(text)
	res := AskResult{Request: text}
	// The person's answer language and model choice (their settings).
	eff := c.refreshPrefs()
	if text == "" || len(text) > 500 {
		res.Kind, res.Error = "error", i18n.G("Type a request (at most 500 characters).")
		return res
	}
	// The read-only skills first: requests to find files, read mail or
	// read a page (and grants, "open result N"). They are recognized by
	// fixed rules on the person's words, before anything is read.
	if r, ok := c.skillAsk(ctx, text); ok {
		return r
	}
	st := c.Theme()
	ictx := intent.Context{Tokens: st.Tokens, Themes: st.Themes, Lang: eff.AnswerLang}
	// The fixed phrases first: when they understand the whole request
	// they are exact and instant. Anything they do not fully understand
	// goes to the local model (if any); when the model is unavailable or
	// unsure, whatever the fixed phrases understood is used.
	// (Measured on the lab set: fixed phrases 69%, the 0.6B model alone
	// 75%, both in this order 91%; docs/command-bar.md.)
	rr := intent.Rules(text, ictx)
	rulesComplete := (len(rr.Calls) > 0 && len(rr.Unknown) == 0) || len(rr.System) > 0
	r := rr
	if tr := c.translator(); !rulesComplete && tr != nil {
		mctx, cancel := context.WithTimeout(ctx, 45*time.Second)
		mr, err := tr.Translate(mctx, text, ictx)
		cancel()
		switch {
		case err != nil:
			r.Explain = append(r.Explain, "local model unavailable ("+err.Error()+")")
		case len(mr.Calls) > 0 || mr.AskSystem:
			r = mr
		case len(rr.Calls) > 0:
			r.Model = mr.Model
			r.Explain = append(r.Explain, "the local model was unsure; used what the fixed phrases understood")
		default:
			r = mr
		}
	}
	res.Explain, res.Unknown, res.Backend, res.Clarify, res.Model = r.Explain, r.Unknown, r.Backend, r.Clarify, r.Model
	_, _ = c.Audit.Append("ask", "commandbar", text, map[string]any{"backend": r.Backend, "calls": r.Calls, "system": r.System,
		"ask_system": r.AskSystem, "unknown": r.Unknown, "clarify": r.Clarify, "model": r.Model, "phrases": r.Phrases})

	if r.AskSystem && len(r.Calls) == 0 {
		if c.Assistant == nil || !c.Assistant.Available() {
			res.Kind, res.Error = "error", i18n.G("The system assistant (basalt) is not installed on this computer.")
			return res
		}
		// The assistant's own translator (basalt ask) picks the command.
		out, err := c.Assistant.Ask(ctx, text)
		if args := assistant.Understood(out); len(args) > 0 && err == nil {
			r.System = args
			res.Explain = append(res.Explain, "system assistant: basalt "+strings.Join(args, " "))
		} else if rr := intent.Rules(text, ictx); len(rr.System) > 0 {
			r.System = rr.System
			res.Explain = append(res.Explain, "system request (fixed phrases): basalt "+strings.Join(rr.System, " "))
		} else {
			res.Kind, res.Text = "system", strings.TrimSpace(out)
			if res.Text == "" && err != nil {
				res.Kind, res.Error = "error", err.Error()
			}
			return res
		}
	}

	if len(r.System) > 0 {
		res.Kind = "system"
		if c.Assistant == nil || !c.Assistant.Available() {
			res.Kind, res.Error = "error", i18n.G("The system assistant (basalt) is not installed on this computer.")
			return res
		}
		out, err := c.Assistant.Read(ctx, r.System)
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
	if len(r.Calls) == 0 {
		res.Kind = "unknown"
		res.Error = i18n.G("I did not understand that. Try: \"make it darker with rounder corners\", \"open text editor\", \"arrange windows side by side\", \"why nginx\".")
		if r.None {
			res.Error = i18n.G("That is not something the desktop does. Try: \"dark mode\", \"open text editor\", \"arrange windows side by side\", \"why nginx\".")
		} else if len(r.Clarify) > 0 {
			res.Error = i18n.G("Please say which one: %s", strings.Join(r.Clarify, "; "))
		}
		return res
	}
	calls := make([]Call, len(r.Calls))
	for i, k := range r.Calls {
		calls[i] = Call{Action: k.Action, Args: k.Args}
	}
	pr, err := c.Propose(ctx, Meta{Origin: "commandbar", Actor: "commandbar", Request: text,
		Explain: strings.Join(r.Explain, "; "), Backend: r.Backend}, calls)
	if err != nil {
		res.Kind, res.Error = "error", err.Error()
		return res
	}
	cp := pr.public()
	res.Kind, res.Proposal = "proposal", &cp
	return res
}

// AssistantApply runs an assistant proposal the person accepted.
func (c *Core) AssistantApply(ctx context.Context, id, code string) (string, error) {
	if c.Assistant == nil {
		return "", fmt.Errorf("the system assistant is not installed")
	}
	_, _ = c.Audit.Append("confirm", "ui", "apply assistant proposal "+id, map[string]any{"assistant_proposal": id, "code": code})
	out, err := c.Assistant.Apply(ctx, id, code)
	typ := "apply"
	data := map[string]any{"assistant_proposal": id, "output": tail(out, 4000)}
	if err != nil {
		typ = "fail"
		data["error"] = err.Error()
	}
	_, _ = c.Audit.Append(typ, "assistant", "basalt apply "+id, data)
	return out, err
}

// AssistantSubmit queues an assistant proposal in the approval gate
// (where the gate decides the system assistant's proposals).
func (c *Core) AssistantSubmit(ctx context.Context, id string) (assistant.Submitted, error) {
	if c.Assistant == nil {
		return assistant.Submitted{}, fmt.Errorf("the system assistant is not installed")
	}
	s, err := c.Assistant.Submit(ctx, id)
	data := map[string]any{"assistant_proposal": id, "gate_id": s.ID, "decision": s.Decision}
	if err != nil {
		data["error"] = err.Error()
	}
	_, _ = c.Audit.Append("propose", "ui", "queue assistant proposal "+id+" in the approval gate", data)
	return s, err
}

// AssistantIgnore closes an assistant proposal.
func (c *Core) AssistantIgnore(ctx context.Context, id string) (string, error) {
	if c.Assistant == nil {
		return "", fmt.Errorf("the system assistant is not installed")
	}
	out, err := c.Assistant.Ignore(ctx, id)
	data := map[string]any{"assistant_proposal": id, "output": tail(out, 2000)}
	if err != nil {
		data["error"] = err.Error()
	}
	_, _ = c.Audit.Append("decline", "ui", "ignore assistant proposal "+id, data)
	return out, err
}

func tail(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[len(s)-n:]
}
