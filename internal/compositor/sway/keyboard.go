package sway

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/basalt-os/basalt-shell/internal/compositor"
)

// msgGetInputs is GET_INPUTS (sway-ipc(7)).
const msgGetInputs = 100

// evInput is the input event (0x80000015 without the event bit).
const evInput = 21

// KeyboardFile is the start-up file of the person's keyboard: the shipped
// sway config includes it (~/.config/basalt-shell/keyboard/*.conf) before
// the person's own additions, so a session starts with their layouts
// from the first key, and an input block of their own still wins.
func KeyboardFile() string {
	cfg := os.Getenv("XDG_CONFIG_HOME")
	if cfg == "" {
		home, _ := os.UserHomeDir()
		cfg = filepath.Join(home, ".config")
	}
	return filepath.Join(cfg, "basalt-shell", "keyboard", "sway.conf")
}

// quoted puts an XKB value in double quotes: sway splits commands on
// commas and semicolons outside quotes. Values come from the registry
// check; anything that could end the quote is refused.
func quoted(v string) (string, error) {
	if strings.ContainsAny(v, "\"'\\;\n\r\t{}") {
		return "", fmt.Errorf("sway: refusing the XKB value %q", v)
	}
	return `"` + v + `"`, nil
}

// keyboardCommands are the commands for a keyboard, in an order where
// every step compiles: the variant is cleared before the layouts change
// (a variant of the old layout would not compile with the new one).
func keyboardCommands(kb compositor.KeyboardConfig) ([]string, error) {
	const in = "input type:keyboard "
	var out []string
	add := func(key, v string) error {
		q, err := quoted(v)
		if err != nil {
			return err
		}
		out = append(out, in+key+" "+q)
		return nil
	}
	if kb.Layout == "" {
		return nil, errors.New("sway: a keyboard needs at least one layout")
	}
	if err := add("xkb_variant", ""); err != nil {
		return nil, err
	}
	if kb.Model != "" {
		if err := add("xkb_model", kb.Model); err != nil {
			return nil, err
		}
	}
	if err := add("xkb_layout", kb.Layout); err != nil {
		return nil, err
	}
	if kb.Variant != "" {
		if err := add("xkb_variant", kb.Variant); err != nil {
			return nil, err
		}
	}
	if err := add("xkb_options", kb.Options); err != nil {
		return nil, err
	}
	delay, rate := kb.RepeatDelay, kb.RepeatRate
	if delay <= 0 {
		delay = 600
	}
	if rate <= 0 {
		rate = 25
	}
	out = append(out, in+"repeat_delay "+strconv.Itoa(delay), in+"repeat_rate "+strconv.Itoa(rate))
	return out, nil
}

// KeyboardConf renders the start-up file. The system's layouts are not
// named there (the session starts with them anyway, from the variables
// basalt-session sets), so a later change of the system's layouts
// reaches the person who never chose their own.
func KeyboardConf(kb compositor.KeyboardConfig) (string, error) {
	var b strings.Builder
	b.WriteString("# Managed by basalt-shell (Settings, Keyboard): rewritten when the settings change.\n")
	b.WriteString("# Your own input blocks go in ~/.config/basalt-shell/sway.d/ and win over this one.\n")
	var lines []string
	line := func(key, v string) error {
		q, err := quoted(v)
		if err != nil {
			return err
		}
		lines = append(lines, "    "+key+" "+q)
		return nil
	}
	if !kb.SystemLayouts {
		if kb.Model != "" {
			if err := line("xkb_model", kb.Model); err != nil {
				return "", err
			}
		}
		if err := line("xkb_layout", kb.Layout); err != nil {
			return "", err
		}
		if kb.Variant != "" {
			if err := line("xkb_variant", kb.Variant); err != nil {
				return "", err
			}
		}
	}
	if kb.Options != "" {
		if err := line("xkb_options", kb.Options); err != nil {
			return "", err
		}
	}
	if kb.RepeatDelay > 0 {
		lines = append(lines, "    repeat_delay "+strconv.Itoa(kb.RepeatDelay))
	}
	if kb.RepeatRate > 0 {
		lines = append(lines, "    repeat_rate "+strconv.Itoa(kb.RepeatRate))
	}
	if len(lines) > 0 {
		b.WriteString("input type:keyboard {\n" + strings.Join(lines, "\n") + "\n}\n")
	}
	return b.String(), nil
}

// SetKeyboard sets the keyboard of the running session (every keyboard
// device, now and plugged in later) and writes the start-up file.
func (a *Adapter) SetKeyboard(_ context.Context, kb compositor.KeyboardConfig) error {
	cmds, err := keyboardCommands(kb)
	if err != nil {
		return err
	}
	body, err := KeyboardConf(kb)
	if err != nil {
		return err
	}
	for _, c := range cmds {
		if err := a.run(c); err != nil {
			return err
		}
	}
	return writeFile(KeyboardFile(), body)
}

// writeFile replaces a file through a temporary one, when it changed.
func writeFile(path, body string) error {
	if old, err := os.ReadFile(path); err == nil && string(old) == body {
		return nil
	}
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		return err
	}
	tmp := path + ".tmp"
	if err := os.WriteFile(tmp, []byte(body), 0o644); err != nil {
		return err
	}
	return os.Rename(tmp, path)
}

// input is the part of GET_INPUTS the shell reads.
type input struct {
	Identifier  string   `json:"identifier"`
	Name        string   `json:"name"`
	Type        string   `json:"type"`
	LayoutNames []string `json:"xkb_layout_names"`
	Active      int      `json:"xkb_active_layout_index"`
}

// virtualKeyboard: a client's keyboard (an agent's input, wtype), which
// keeps the keymap its client gave it.
func virtualKeyboard(in input) bool {
	return strings.Contains(in.Identifier, "virtual") || strings.Contains(in.Name, "virtual")
}

// pickKeyboard is the keyboard whose layouts the shell shows: the first
// real one with layouts.
func pickKeyboard(list []input) (compositor.KeyboardState, bool) {
	for _, in := range list {
		if in.Type != "keyboard" || virtualKeyboard(in) || len(in.LayoutNames) == 0 {
			continue
		}
		names := append([]string(nil), in.LayoutNames...)
		cur := in.Active
		if cur < 0 || cur >= len(names) {
			cur = 0
		}
		return compositor.KeyboardState{Names: names, Current: cur, Live: true}, true
	}
	return compositor.KeyboardState{}, false
}

// KeyboardState reads the layouts of the seat's keyboard.
func (a *Adapter) KeyboardState(context.Context) (compositor.KeyboardState, error) {
	var list []input
	if err := a.do(msgGetInputs, nil, &list); err != nil {
		return compositor.KeyboardState{}, err
	}
	st, _ := pickKeyboard(list)
	return st, nil
}

// SwitchLayout switches every keyboard to the next layout or to one.
func (a *Adapter) SwitchLayout(_ context.Context, index int) error {
	if index < 0 {
		return a.run("input type:keyboard xkb_switch_layout next")
	}
	return a.run("input type:keyboard xkb_switch_layout " + strconv.Itoa(index))
}

// keyboardEvent reports an input event about keyboard layouts.
func keyboardEvent(body []byte) bool {
	var ev struct {
		Change string `json:"change"`
		Input  input  `json:"input"`
	}
	if json.Unmarshal(body, &ev) != nil || ev.Input.Type != "keyboard" || virtualKeyboard(ev.Input) {
		return false
	}
	switch ev.Change {
	case "xkb_layout", "xkb_keymap", "added", "removed":
		return true
	}
	return false
}
