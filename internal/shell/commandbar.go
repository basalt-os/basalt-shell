package shell

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/openbasalt/basalt-shell/internal/assistant"
	"github.com/openbasalt/basalt-shell/internal/intent"
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
}

// actionInfos describes the action set for the model prompt.
func actionInfos() []intent.ActionInfo {
	out := make([]intent.ActionInfo, 0, len(Actions))
	for _, a := range Actions {
		var ps []string
		for _, p := range a.Params {
			s := p.Name + ":" + p.Type
			if len(p.Enum) > 0 {
				s += "[" + strings.Join(p.Enum, "|") + "]"
			}
			if !p.Required {
				s += "?"
			}
			ps = append(ps, s)
		}
		out = append(out, intent.ActionInfo{Name: a.Name, Description: a.Description, Params: strings.Join(ps, ", ")})
	}
	return out
}

// Ask understands a command-bar request: the language model when one is
// configured (falling back to rules when it fails), else the rules.
// Desktop actions become a proposal; system questions go to the
// assistant's read commands. Nothing is applied here.
func (c *Core) Ask(ctx context.Context, text string) AskResult {
	text = strings.TrimSpace(text)
	res := AskResult{Request: text}
	if text == "" || len(text) > 500 {
		res.Kind, res.Error = "error", "type a request (at most 500 characters)"
		return res
	}
	st := c.Theme()
	ictx := intent.Context{Tokens: st.Tokens, Themes: st.Themes}
	var r intent.Result
	if c.Translator != nil {
		mctx, cancel := context.WithTimeout(ctx, 30*time.Second)
		mr, err := c.Translator.Translate(mctx, text, ictx, actionInfos())
		cancel()
		if err == nil && (len(mr.Calls) > 0 || len(mr.System) > 0) {
			r = mr
		} else {
			r = intent.Rules(text, ictx)
			if err != nil {
				r.Explain = append(r.Explain, "model unavailable ("+err.Error()+"), used rules")
			}
		}
	} else {
		r = intent.Rules(text, ictx)
	}
	res.Explain, res.Unknown, res.Backend = r.Explain, r.Unknown, r.Backend
	_, _ = c.Audit.Append("ask", "commandbar", text, map[string]any{"backend": r.Backend, "calls": r.Calls, "system": r.System, "unknown": r.Unknown})

	if len(r.System) > 0 {
		res.Kind = "system"
		if c.Assistant == nil || !c.Assistant.Available() {
			res.Kind, res.Error = "error", "the system assistant (basalt) is not installed on this machine"
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
		res.Error = "I did not understand that. Try: \"make it darker with rounder corners\", \"open text editor\", \"arrange windows side by side\", \"why nginx\"."
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
