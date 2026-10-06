package skills

import (
	"strings"
	"testing"
)

// The summaries are written in the person's answer language: their system
// prompt keeps its English instructions and gets one line with the
// language and the rule that machine-facing text stays English.
func TestAnswerPromptLanguage(t *testing.T) {
	e := &Engine{}
	base := summarizerPrompt("a web page")
	if e.answerPrompt(base) != base {
		t.Error("no language set: the prompt must be the reference one")
	}
	e.SetModel(nil, "en-GB")
	if e.answerPrompt(base) != base {
		t.Error("English: the prompt must be the reference one")
	}
	e.SetModel(nil, "pt-BR")
	p := e.answerPrompt(base)
	if !strings.HasPrefix(p, base) || !strings.Contains(p, "Answer the person in Brazilian Portuguese (pt-BR)") ||
		!strings.Contains(p, "JSON keys") {
		t.Errorf("prompt:\n%s", p)
	}
	if e.Lang() != "pt-BR" || e.model() != nil {
		t.Error("SetModel")
	}
}

func TestClipKeepsCharacters(t *testing.T) {
	if got := clip("ação", 2); got != "a..." {
		t.Errorf("%q", got)
	}
}
