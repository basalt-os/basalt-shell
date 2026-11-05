package shell

import (
	"context"
	"strings"
	"testing"

	"github.com/basalt-os/basalt-shell/internal/i18n"
)

// A fixed-phrase proposal in a pt-BR session shows how it was understood
// and its steps in Portuguese, not the parser's English ("light mode",
// "Switch to light mode"; bugs.md 2026-10-22, release audit 2026-11).
func TestFixedPhraseProposalInPortuguese(t *testing.T) {
	t.Setenv("BASALT_SHELL_LOCALE_DIR", "../../locale")
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", "pt_BR.UTF-8")
	c, _, _ := newCore(t)
	i18n.Load("pt-BR")
	t.Cleanup(func() { i18n.Load("en") })
	for _, q := range []string{"modo claro", "modo escuro", "light mode"} {
		res := c.Ask(context.Background(), q)
		if res.Proposal == nil {
			t.Fatalf("%s: %+v", q, res)
		}
		lines := append([]string{res.Proposal.Explain}, res.Proposal.Steps...)
		for _, l := range lines {
			for _, en := range []string{"light mode", "dark mode", "Switch to"} {
				if strings.Contains(l, en) {
					t.Errorf("%s: English line %q", q, l)
				}
			}
		}
		if !strings.Contains(strings.Join(res.Proposal.Steps, " "), "Usar modo") {
			t.Errorf("%s: steps %q", q, res.Proposal.Steps)
		}
	}
}
