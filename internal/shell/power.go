package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"time"

	"github.com/basalt-os/basalt-shell/internal/i18n"
	"github.com/basalt-os/basalt-shell/internal/intent"
)

// session.power: lock the screen, log out, suspend, restart or power off.
//
// It is a Person action: planned only from the person's own words (the
// command bar or push to talk: "restart the computer", "desligar"),
// shown for confirmation like every change, or run by the shell UI's
// power menu after its own confirmation (the UI flag). An agent can
// neither propose nor run it, and it is not an MCP tool.
//
// Everything goes through logind from the person's session, with the
// system's polkit rules: systemctl suspend, reboot and poweroff (an
// active local session may do them without a password when nobody else
// is logged in; otherwise polkit asks, through the shell's dialog), and
// loginctl terminate-session for logging out. Locking runs basalt-lock,
// as Super+L does.

func init() {
	Actions = append(Actions, &ActionDef{
		Name: "session.power", Title: "Lock, log out, suspend, restart or power off",
		Description: "Lock the screen, log out of the session, suspend, restart or power off the computer. Only from the person's own request or the shell's power menu.",
		Person:      true,
		UI:          true,
		Params:      []Param{{Name: "op", Type: "string", Required: true, Enum: intent.PowerOps, Description: "lock, logout, suspend, restart or poweroff"}},
		plan: func(ctx context.Context, p *planner, a map[string]any) (step, error) {
			op := argStr(a, "op")
			sum := powerSummary(op)
			if sum == "" {
				return step{}, fmt.Errorf("op must be one of %s", strings.Join(intent.PowerOps, ", "))
			}
			return step{Summary: sum, Preview: map[string]any{"kind": "power", "op": op}, run: func(ctx context.Context) (any, error) {
				return nil, p.c.Power(ctx, op)
			}}, nil
		},
	})
}

// powerSummary is what the confirmation says ("" for an unknown op).
func powerSummary(op string) string {
	switch op {
	case "lock":
		return i18n.G("Lock the screen")
	case "logout":
		return i18n.G("Log out (open apps are closed; save your work first)")
	case "suspend":
		return i18n.G("Suspend the computer")
	case "restart":
		return i18n.G("Restart the computer (open apps are closed; save your work first)")
	case "poweroff":
		return i18n.G("Power off the computer (open apps are closed; save your work first)")
	}
	return ""
}

// powerCommand is the program that does op.
func powerCommand(op, sessionID string) ([]string, error) {
	switch op {
	case "lock":
		return []string{"basalt-lock"}, nil
	case "logout":
		if sessionID == "" {
			// systemd 256 and later understand "self" (the caller's session).
			sessionID = "self"
		}
		return []string{"loginctl", "terminate-session", sessionID}, nil
	case "suspend":
		return []string{"systemctl", "suspend"}, nil
	case "restart":
		return []string{"systemctl", "reboot"}, nil
	case "poweroff":
		return []string{"systemctl", "poweroff"}, nil
	}
	return nil, fmt.Errorf("unknown power operation %q", op)
}

// Power does op. The audit record is written by the caller (the
// confirmed proposal or the UI's execute).
func (c *Core) Power(ctx context.Context, op string) error {
	argv, err := powerCommand(op, os.Getenv("XDG_SESSION_ID"))
	if err != nil {
		return err
	}
	run := c.PowerRun
	if run == nil {
		run = runPower
	}
	if op == "lock" {
		// The locker stays running: start it and do not wait for it.
		if c.Comp != nil {
			if err := c.Comp.Spawn(ctx, argv); err == nil {
				return nil
			}
		}
	}
	if err := run(ctx, argv); err != nil {
		return errors.New(i18n.G("Could not do it: %s", err.Error()))
	}
	return nil
}

func runPower(ctx context.Context, argv []string) error {
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		if len(msg) > 300 {
			msg = msg[:300]
		}
		return errors.New(msg)
	}
	return nil
}
