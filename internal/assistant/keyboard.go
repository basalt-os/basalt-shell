package assistant

import (
	"context"
	"fmt"
	"regexp"
	"strings"
)

// Settings, Keyboard: "Use for the login screen and new accounts too".
// The system's keyboard (systemd-localed's X11 and console keymaps) is a
// system change, so the shell never sets it: it asks the assistant to
// store a keyboard.system proposal through the read helper (`basalt
// keyboard set LAYOUTS [--options OPTIONS] --json`, which changes nothing)
// and the person applies it like every other proposal (the approval gate,
// or basalt-apply@ID_CODE.service and an administrator's password). The
// assistant's executor runs localectl; the shell's domains never do (a
// test in policy_test.go refuses it).

var (
	// reKbdLayouts: one to four XKB layouts, each with an optional
	// variant in parentheses: "br,us(intl)".
	reKbdLayouts = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,31}(\([A-Za-z0-9][A-Za-z0-9_.+-]{0,47}\))?(,[a-z][a-z0-9_-]{0,31}(\([A-Za-z0-9][A-Za-z0-9_.+-]{0,47}\))?){0,3}$`)
	// reKbdOptions: up to sixteen XKB options: "grp:alt_shift_toggle,compose:ralt".
	reKbdOptions = regexp.MustCompile(`^[a-z][a-z0-9_]{0,31}:[A-Za-z0-9_+.-]{1,48}(,[a-z][a-z0-9_]{0,31}:[A-Za-z0-9_+.-]{1,48}){0,15}$`)
)

// keyboardArgs validates `basalt keyboard` requests: the report, or
// storing the keyboard.system proposal.
func keyboardArgs(rest []string) error {
	bad := fmt.Errorf("not a keyboard request: keyboard %s", strings.Join(rest, " "))
	switch {
	case len(rest) == 1 && rest[0] == "--json":
		return nil
	case len(rest) == 3 && rest[0] == "set" && reKbdLayouts.MatchString(rest[1]) && rest[2] == "--json":
		return nil
	case len(rest) == 5 && rest[0] == "set" && reKbdLayouts.MatchString(rest[1]) && rest[2] == "--options" &&
		reKbdOptions.MatchString(rest[3]) && rest[4] == "--json":
		return nil
	}
	return bad
}

// KeyboardArgs is the request that stores the proposal (checked again by
// keyboardArgs and by the read helper).
func KeyboardArgs(layouts, options string) ([]string, error) {
	args := []string{"keyboard", "set", layouts}
	if options != "" {
		args = append(args, "--options", options)
	}
	args = append(args, "--json")
	if err := keyboardArgs(args[1:]); err != nil {
		return nil, err
	}
	return args, nil
}

// KeyboardPropose stores the keyboard.system proposal: these layouts (and
// options) for the login screen, the console and new accounts.
func (b *Bridge) KeyboardPropose(ctx context.Context, layouts, options string) (Proposal, error) {
	args, err := KeyboardArgs(layouts, options)
	if err != nil {
		return Proposal{}, err
	}
	return b.stored(ctx, args)
}
