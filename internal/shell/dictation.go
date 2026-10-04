package shell

import (
	"context"
	"errors"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/basalt-os/basalt-shell/internal/i18n"
	"github.com/basalt-os/basalt-shell/internal/wlime"
)

// Dictation (ADR 0012, "Push to talk and where the words go").
//
// The shell owns the microphone and decides, when the key goes down,
// where the words will go, and shows it while the person speaks:
//
//   - a text field has focus (the compositor activated the shell's input
//     method for it): the words are dictation into that field;
//   - no text field has focus, or the utterance starts with "assistant":
//     the words are a request to the assistant (the command bar);
//   - the focused field is for a secret (password, PIN): no dictation;
//     the words go to the assistant, and the card says so.
//
// Dictated text never arrives as synthetic key presses. It is shown in
// the field as the input method's pre-edit text (underlined, not part of
// the field's text yet) and typed only when the person confirms on the
// card (Insert, or Super+Return); Discard, a new press or a timeout
// clear it. If the field lost focus in between, nothing is typed. This
// is a typed action (text.insert) like every other change: planned only
// from the person's own voice, confirmed only by the shell UI, recorded.

// MaxDictation is the longest text typed at once (bytes).
const MaxDictation = 2000

// voiceRoute is where the words of a press go.
type voiceRoute struct {
	Mode   string // "dictation" or "assistant"
	Target string // the app that has the text field
	Gen    uint64 // the field's activation (wlime.State.Gen)
	Note   string // why the words go to the assistant although a field has focus
}

// WatchInputMethod holds the seat's input method for the whole session
// (reconnecting after errors); without it, every utterance goes to the
// assistant.
func (c *Core) WatchInputMethod(ctx context.Context) {
	for ctx.Err() == nil {
		path, err := wlime.Socket()
		if err != nil {
			return
		}
		im, err := wlime.Open(path)
		if err != nil {
			log.Printf("dictation off: %v", err)
			select {
			case <-ctx.Done():
				return
			case <-time.After(30 * time.Second):
			}
			continue
		}
		c.mu.Lock()
		c.im = im
		c.mu.Unlock()
		log.Printf("dictation: input method ready")
		for ctx.Err() == nil && im.Err() == nil {
			select {
			case <-ctx.Done():
			case <-time.After(2 * time.Second):
			}
		}
		if err := im.Err(); err != nil {
			log.Printf("dictation: %v", err)
		}
		c.mu.Lock()
		c.im = nil
		c.mu.Unlock()
		_ = im.Close()
		select {
		case <-ctx.Done():
			return
		case <-time.After(10 * time.Second):
		}
	}
}

func (c *Core) inputMethod() *wlime.IM {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.im
}

// routeVoice decides where the words of this press go.
func (c *Core) routeVoice() voiceRoute {
	im := c.inputMethod()
	if im == nil {
		return voiceRoute{Mode: "assistant"}
	}
	st := im.State()
	if !st.Active {
		return voiceRoute{Mode: "assistant"}
	}
	app := c.focusedApp()
	if st.Sensitive() {
		return voiceRoute{Mode: "assistant", Target: app, Note: i18n.G("The focused field is for a password or code: it gets no dictation.")}
	}
	return voiceRoute{Mode: "dictation", Target: app, Gen: st.Gen}
}

// focusedApp names the focused window's app for the person.
func (c *Core) focusedApp() string {
	for _, w := range c.DesktopState().Windows {
		if !w.Focused {
			continue
		}
		for _, a := range c.Apps() {
			if strings.EqualFold(a.ID, w.AppID) || strings.EqualFold(strings.TrimSuffix(a.ID, ".desktop"), w.AppID) {
				return a.Name
			}
		}
		if w.AppID != "" {
			return w.AppID
		}
		return w.Title
	}
	return i18n.G("the focused app")
}

// reAssistantPrefix: an utterance addressed to the assistant while a text
// field has focus ("assistant, find the PDF from the bank").
var reAssistantPrefix = regexp.MustCompile(`(?i)^\s*(?:hey\s+|ok\s+|okay\s+)?(?:assistant|assistente|basalt)\b[\s,.:!]*`)

// AssistantPrefix strips a leading "assistant" and reports whether it was there.
func AssistantPrefix(text string) (string, bool) {
	if loc := reAssistantPrefix.FindStringIndex(text); loc != nil {
		return strings.TrimSpace(text[loc[1]:]), true
	}
	return text, false
}

func init() {
	Actions = append(Actions, &ActionDef{
		Name:        "text.insert",
		Title:       "Type dictated text",
		Description: "Type what the person dictated into the text field that had focus when they spoke (as an input method, not as key presses). Only from the person's own voice.",
		Person:      true,
		Params: []Param{
			{Name: "text", Type: "string", Required: true, Description: "the dictated text"},
			{Name: "field", Type: "string", Required: true, Description: "the field's activation, set by the shell"},
			{Name: "app", Type: "string", Description: "the app of the field, for the person"},
		},
		plan: planTextInsert,
	})
}

func planTextInsert(ctx context.Context, p *planner, a map[string]any) (step, error) {
	if p.meta.Origin != "voice" {
		return step{}, errors.New("dictation comes only from push to talk")
	}
	im := p.c.inputMethod()
	if im == nil {
		return step{}, errors.New("dictation is not available (no input method)")
	}
	text, _ := a["text"].(string)
	text = strings.TrimSpace(text)
	if text == "" || len(text) > MaxDictation || !utf8.ValidString(text) {
		return step{}, errors.New("the dictated text is empty or too long")
	}
	gen, err := strconv.ParseUint(argStr(a, "field"), 10, 64)
	if err != nil {
		return step{}, errors.New("bad field")
	}
	app := argStr(a, "app")
	if err := im.Preedit(gen, text); err != nil {
		return step{}, err
	}
	n := utf8.RuneCountInString(text)
	return step{
		Summary: i18n.N("Type the dictated text (%d character) into %s", "Type the dictated text (%d characters) into %s", n, n, app),
		Preview: map[string]any{"kind": "dictation", "app": app, "text": text},
		run: func(ctx context.Context) (any, error) {
			if err := im.Commit(gen, text); err != nil {
				if errors.Is(err, wlime.ErrFieldChanged) {
					return nil, errors.New(i18n.G("The text field lost focus, so nothing was typed. Click in the field and dictate again."))
				}
				return nil, err
			}
			return map[string]any{"typed": n, "app": app}, nil
		},
		end: func(string) { im.Clear(gen) },
	}, nil
}

// dictate shows the dictated text in the field and asks the person to
// confirm it on the voice card.
func (c *Core) dictate(ctx context.Context, rt voiceRoute, text string) (*Proposal, error) {
	if len(text) > MaxDictation {
		text = text[:MaxDictation]
	}
	return c.Propose(ctx, Meta{Origin: "voice", Actor: "voice", Request: text, Backend: "voice",
		Explain: i18n.G("Dictation into %s", rt.Target)},
		[]Call{{Action: "text.insert", Args: map[string]any{"text": text, "field": fmt.Sprint(rt.Gen), "app": rt.Target}}})
}
