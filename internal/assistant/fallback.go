package assistant

import (
	"context"
	"fmt"
	"strings"
)

// The GNOME fallback's login screen (release audit 2026-11, S15). GNOME
// has no screen lock without GDM running, so while the person uses GNOME
// as the fallback, GDM should be the display manager, and greetd (the
// Basalt login screen) again afterwards. Which display manager starts is
// a system change: the shell never enables or disables a unit. It asks
// the assistant to store a desktop.fallback proposal through the read
// helper (`basalt fallback gnome --json` or `basalt fallback basalt
// --json`, which change nothing), and the person applies it like every
// other proposal (the approval gate, or basalt-apply@ID_CODE.service and
// an administrator's password). The assistant's executor switches the
// display-manager units; the switch takes effect at the next restart.

// FallbackTargets are the display managers the switch can choose:
// gnome (GDM, while GNOME is used) and basalt (greetd and the Basalt
// login screen).
var FallbackTargets = []string{"gnome", "basalt"}

// fallbackArgs validates `basalt fallback` requests: the report, or
// storing the proposal for one target.
func fallbackArgs(rest []string) error {
	switch {
	case len(rest) == 1 && rest[0] == "--json":
		return nil
	case len(rest) == 2 && (rest[0] == "gnome" || rest[0] == "basalt") && rest[1] == "--json":
		return nil
	}
	return fmt.Errorf("not a fallback request: fallback %s", strings.Join(rest, " "))
}

// FallbackArgs is the request that stores the proposal for target.
func FallbackArgs(target string) ([]string, error) {
	args := []string{"fallback", target, "--json"}
	if err := fallbackArgs(args[1:]); err != nil {
		return nil, err
	}
	return args, nil
}

// FallbackPropose stores the desktop.fallback proposal: GDM as the
// display manager (target gnome) or greetd again (target basalt).
func (b *Bridge) FallbackPropose(ctx context.Context, target string) (Proposal, error) {
	args, err := FallbackArgs(target)
	if err != nil {
		return Proposal{}, err
	}
	return b.stored(ctx, args)
}
