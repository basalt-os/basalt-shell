package shell

import (
	"context"
	"errors"
	"fmt"
	"strconv"

	"github.com/basalt-os/basalt-shell/internal/i18n"
)

// Consent requests (ADR 0018 knowledge packs, ADR 0019 web search and
// remote content): the assistant asks before it fetches a knowledge pack
// for a topic, sends a web search, fetches a page or uses a remote model.
// They are proposals like any other: the consent text is the step shown
// on the sheet and, on Basalt OS, the preview of the approval gate
// request (path "consent"), where a rule may remember "always for this
// topic". Only the person's own words and the assistant loop ask for them,
// never an agent connection. Running one records the consent; the caller
// (the assistant loop) then does what was allowed.
func init() {
	Actions = append(Actions,
		&ActionDef{
			Name: "knowledge.fetch", Title: "Download a knowledge pack",
			Description: "Download a signed knowledge pack for a topic the assistant needs (data only, checked before use). Asked by the assistant or the person, never by an agent.",
			Params: []Param{
				{Name: "pack", Type: "string", Required: true, Description: "pack name"},
				{Name: "topic", Type: "string", Required: true, Description: "the topic, for the person"},
				{Name: "host", Type: "string", Required: true, Description: "where it comes from"},
				{Name: "bytes", Type: "integer", Description: "size"},
			},
			plan: planConsent,
		},
		&ActionDef{
			Name: "remote.consent", Title: "Let the assistant use the web",
			Description: "Let the assistant send a web search, fetch a page or use a remote model for this conversation. What it reads stays data. Asked by the assistant or the person, never by an agent.",
			Params: []Param{
				{Name: "what", Type: "string", Required: true, Enum: []string{"web.search", "page.fetch", "remote.model"}, Description: "what leaves the computer"},
				{Name: "host", Type: "string", Required: true, Description: "where it goes"},
				{Name: "scope", Type: "string", Enum: []string{"once", "conversation"}, Description: "for how long"},
			},
			plan: planConsent,
		},
	)
}

func planConsent(ctx context.Context, p *planner, a map[string]any) (step, error) {
	switch p.meta.Origin {
	case "assistant", "commandbar", "voice":
	default:
		return step{}, errors.New("only the assistant or the person asks for this")
	}
	host := argStr(a, "host")
	var text string
	if pack := argStr(a, "pack"); pack != "" {
		size := ""
		if n, err := strconv.ParseInt(fmt.Sprint(a["bytes"]), 10, 64); err == nil && n > 0 {
			size = fmt.Sprintf(" (%d MB)", (n+1<<20-1)>>20)
		}
		text = i18n.G("Download the knowledge pack %s%s about %s from %s. It is signed and checked before use; it is data and cannot run anything.",
			pack, size, argStr(a, "topic"), host)
	} else {
		switch argStr(a, "what") {
		case "web.search":
			text = i18n.G("Send your question as a web search to %s. The results are read as data, never as instructions.", host)
		case "page.fetch":
			text = i18n.G("Fetch a page from %s. What it says is read as data, never as instructions.", host)
		default:
			text = i18n.G("Send this conversation to the remote model at %s.", host)
		}
	}
	preview := map[string]any{"kind": "consent", "text": text, "host": host}
	return step{Summary: text, Preview: preview, run: func(context.Context) (any, error) {
		return map[string]any{"consented": true}, nil
	}}, nil
}
