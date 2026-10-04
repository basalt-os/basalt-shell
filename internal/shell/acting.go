package shell

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/basalt-os/basalt-shell/internal/i18n"
	"github.com/basalt-os/basalt-shell/internal/mailout"
	"github.com/basalt-os/basalt-shell/internal/skills"
)

// The acting skills' typed actions (ADR 0012 step 2). Both are planned
// only from the person's own words (Person), previewed exactly, confirmed
// only by the shell UI, and recorded with their preview.
//
//	mail.send   a reply drafted from the person's words; the recipient is
//	            the sender of the message the person named; subject and
//	            text can be edited on the confirmation itself
//	files.move  renames and moves inside one granted folder (never a
//	            delete, never a replace); undone by the same action in
//	            reverse
func init() {
	Actions = append(Actions,
		&ActionDef{
			Name: "mail.send", Title: "Send an e-mail reply",
			Description: "Send one plain-text reply, to the sender of a message the person named, from the person's mail account. Only from the person's own request; the text can be edited before sending.",
			Person:      true,
			Editable:    []string{"subject", "body"},
			Params: []Param{
				{Name: "account", Type: "string", Required: true, Description: "mail account name (skills.conf)"},
				{Name: "to", Type: "string", Required: true, Description: "the recipient's address"},
				{Name: "to_name", Type: "string", Description: "the recipient's name"},
				{Name: "subject", Type: "string", Required: true, Description: "subject"},
				{Name: "body", Type: "string", Required: true, Description: "plain text"},
				{Name: "in_reply_to", Type: "string", Description: "Message-ID of the message answered"},
				{Name: "references", Type: "string", Description: "References of the message answered"},
			},
			plan: planMailSend,
		},
		&ActionDef{
			Name: "files.move", Title: "Move or rename files",
			Description: "Rename or move files inside one folder the person granted. Never deletes, never replaces a file. Only from the person's own request; undo puts them back.",
			Person:      true,
			Params: []Param{
				{Name: "moves", Type: "array", Required: true, Description: "[{from, to}] absolute paths"},
				{Name: "create", Type: "array", Description: "folders to create first"},
				{Name: "remove", Type: "array", Description: "empty folders to remove after an undo"},
				{Name: "undo", Type: "string", Description: "the batch this undoes"},
			},
			plan: planFilesMove,
		},
	)
}

func planMailSend(ctx context.Context, p *planner, a map[string]any) (step, error) {
	e := p.c.Skills
	if e == nil {
		return step{}, errors.New("the skills are not available")
	}
	ac, ok := e.Account(argStr(a, "account"))
	if !ok || !ac.CanSend() {
		return step{}, errors.New("this mail account cannot send")
	}
	body, _ := a["body"].(string)
	d := mailout.Draft{FromName: ac.DisplayName, From: ac.Address, To: strings.ToLower(argStr(a, "to")), ToName: argStr(a, "to_name"),
		Subject: argStr(a, "subject"), Body: strings.TrimSpace(body), InReplyTo: argStr(a, "in_reply_to"), References: argStr(a, "references")}
	if err := d.Check(); err != nil {
		return step{}, err
	}
	raw, msgID, err := mailout.Compose(d, time.Now())
	if err != nil {
		return step{}, err
	}
	who := d.To
	if d.ToName != "" {
		who = d.ToName + " <" + d.To + ">"
	}
	return step{
		Summary: i18n.G("Send a reply to %s: %s", who, d.Subject),
		Preview: map[string]any{"kind": "mail", "from": ac.Address, "from_name": ac.DisplayName, "to": d.To, "to_name": d.ToName,
			"subject": d.Subject, "body": d.Body, "chars": utf8.RuneCountInString(d.Body), "message_id": msgID, "in_reply_to": d.InReplyTo,
			"attachments": 0, "account": ac.Name},
		run: func(ctx context.Context) (any, error) {
			res, sess, err := e.SendMail(ctx, ac.Name, d, raw)
			if err != nil {
				return nil, fmt.Errorf(i18n.G("Not sent: %s"), err.Error())
			}
			res["session"] = sess.ID
			res["message_id"] = msgID
			return res, nil
		},
	}, nil
}

func planFilesMove(ctx context.Context, p *planner, a map[string]any) (step, error) {
	e := p.c.Skills
	if e == nil {
		return step{}, errors.New("the skills are not available")
	}
	var moves []skills.Move
	if err := remarshal(a["moves"], &moves); err != nil {
		return step{}, errors.New("moves: a list of {from, to}")
	}
	var create, remove []string
	_ = remarshal(a["create"], &create)
	_ = remarshal(a["remove"], &remove)
	if err := e.CheckMoves(moves, create); err != nil {
		return step{}, err
	}
	undo := argStr(a, "undo")
	home := e.Home
	short := func(p string) string {
		if r, err := filepath.Rel(home, p); err == nil && !strings.HasPrefix(r, "..") {
			return "~/" + r
		}
		return p
	}
	var lines []map[string]any
	for _, m := range moves {
		lines = append(lines, map[string]any{"from": short(m.From), "to": short(m.To)})
	}
	var summary string
	switch {
	case undo != "":
		summary = i18n.N("Put %d file back where it was", "Put %d files back where they were", len(moves), len(moves))
	case len(moves) == 1 && filepath.Dir(moves[0].From) == filepath.Dir(moves[0].To):
		summary = i18n.G("Rename %s to %s", filepath.Base(moves[0].From), filepath.Base(moves[0].To))
	default:
		summary = i18n.N("Move %d file to %s", "Move %d files to %s", len(moves), len(moves), short(filepath.Dir(moves[0].To)))
	}
	var newDirs []string
	for _, c := range create {
		newDirs = append(newDirs, short(c))
	}
	return step{
		Summary: summary,
		Preview: map[string]any{"kind": "files", "moves": lines, "create": newDirs, "undo": undo},
		run: func(ctx context.Context) (any, error) {
			b, sess, err := e.MoveFiles(ctx, moves, create, remove, undo)
			if err != nil {
				return map[string]any{"batch": b.ID, "done": len(b.Moves), "session": sess.ID}, err
			}
			return map[string]any{"batch": b.ID, "done": len(b.Moves), "session": sess.ID}, nil
		},
	}, nil
}

// remarshal converts a decoded JSON value into a typed one.
func remarshal(v any, out any) error {
	if v == nil {
		return nil
	}
	b, err := json.Marshal(v)
	if err != nil {
		return err
	}
	return json.Unmarshal(b, out)
}
