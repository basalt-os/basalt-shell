package shell

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basalt-os/basalt-shell/internal/skills"
)

// Agents can request desktop changes, never acting skills: sending mail,
// typing dictated text and moving files come only from the person.
func TestPersonActionsRefuseAgents(t *testing.T) {
	c, _, dir := newCore(t)
	home := filepath.Join(dir, "home")
	_ = os.MkdirAll(filepath.Join(home, "Documents"), 0o755)
	_ = os.WriteFile(filepath.Join(home, "Documents", "a.txt"), []byte("a"), 0o644)
	c.Skills = &skills.Engine{Store: skills.NewStore(), Home: home, Runner: &skills.Runner{NoScope: true},
		Config: skills.Config{Accounts: []skills.Account{{Name: "work", Host: "imap.example.org", Address: "alex@example.com", SMTPHost: "smtp.example.org"}}}}
	c.Skills.Store.Add(skills.GrantFolder, filepath.Join(home, "Documents"), "Documents", "ui", 3600e9)
	ctx := context.Background()
	calls := map[string]Call{
		"mail.send": {Action: "mail.send", Args: map[string]any{"account": "work", "to": "ana@example.org", "subject": "Re: x", "body": "hi"}},
		"files.move": {Action: "files.move", Args: map[string]any{"moves": []any{map[string]any{"from": filepath.Join(home, "Documents", "a.txt"),
			"to": filepath.Join(home, "Documents", "b.txt")}}}},
		"text.insert": {Action: "text.insert", Args: map[string]any{"text": "hello", "field": "1"}},
	}
	for name, call := range calls {
		for _, origin := range []string{"mcp", "ipc"} {
			if _, err := c.Propose(ctx, Meta{Origin: origin, Actor: "agent:test"}, []Call{call}); err == nil || !strings.Contains(err.Error(), "person") {
				t.Errorf("%s from %s: %v", name, origin, err)
			}
		}
	}
	// The person's own request is accepted (and previewed exactly).
	pr, err := c.Propose(ctx, Meta{Origin: "commandbar", Actor: "commandbar"}, []Call{calls["files.move"]})
	if err != nil {
		t.Fatal(err)
	}
	if len(pr.Previews) != 1 || pr.Previews[0]["kind"] != "files" {
		t.Errorf("preview %v", pr.Previews)
	}
	pr, err = c.Propose(ctx, Meta{Origin: "commandbar", Actor: "commandbar"}, []Call{calls["mail.send"]})
	if err != nil {
		t.Fatal(err)
	}
	if pr.Previews[0]["to"] != "ana@example.org" || pr.Previews[0]["body"] != "hi" {
		t.Errorf("mail preview %v", pr.Previews[0])
	}
	// The recipient is not editable on the confirmation; the text is.
	if err := c.applyEdits(ctx, c.proposals[pr.ID], map[string]any{"to": "eve@example.net"}, "ui"); err == nil {
		t.Error("recipient edited")
	}
	if err := c.applyEdits(ctx, c.proposals[pr.ID], map[string]any{"body": "Hi Ana, see you Thursday."}, "ui"); err != nil {
		t.Fatal(err)
	}
	if got := c.proposals[pr.ID].Previews[0]["body"]; got != "Hi Ana, see you Thursday." {
		t.Errorf("edited preview %v", got)
	}
}
