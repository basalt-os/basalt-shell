package niri

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/basalt-os/basalt-shell/internal/compositor"
)

// KeyboardPath is the managed include with the person's keyboard:
// basalt-keyboard.kdl next to the theme include (the shipped config.kdl
// includes it after its own input section; basalt-session creates it
// empty). niri reloads it live when it changes.
func (a *Adapter) KeyboardPath() string {
	return filepath.Join(filepath.Dir(a.StylePath), "basalt-keyboard.kdl")
}

// KeyboardKDL renders the include. The system's layouts are not named
// (niri then takes them from the session, as at the login screen).
func KeyboardKDL(kb compositor.KeyboardConfig) (string, error) {
	for _, v := range []string{kb.Layout, kb.Variant, kb.Model, kb.Options} {
		if strings.ContainsAny(v, "\"\\\n\r") {
			return "", fmt.Errorf("niri: refusing the XKB value %q", v)
		}
	}
	var b strings.Builder
	b.WriteString("// Managed by basalt-shell (Settings, Keyboard): rewritten when the settings change.\n")
	b.WriteString("// Your own additions go in local.kdl.\n")
	var xkb []string
	if !kb.SystemLayouts {
		if kb.Layout != "" {
			xkb = append(xkb, "layout "+kdlString(kb.Layout))
		}
		if kb.Variant != "" {
			xkb = append(xkb, "variant "+kdlString(kb.Variant))
		}
		if kb.Model != "" {
			xkb = append(xkb, "model "+kdlString(kb.Model))
		}
	}
	if kb.Options != "" {
		xkb = append(xkb, "options "+kdlString(kb.Options))
	}
	var kbd []string
	if len(xkb) > 0 {
		kbd = append(kbd, "xkb {\n            "+strings.Join(xkb, "\n            ")+"\n        }")
	}
	if kb.RepeatDelay > 0 {
		kbd = append(kbd, fmt.Sprintf("repeat-delay %d", kb.RepeatDelay))
	}
	if kb.RepeatRate > 0 {
		kbd = append(kbd, fmt.Sprintf("repeat-rate %d", kb.RepeatRate))
	}
	if len(kbd) > 0 {
		b.WriteString("input {\n    keyboard {\n        " + strings.Join(kbd, "\n        ") + "\n    }\n}\n")
	}
	return b.String(), nil
}

// SetKeyboard rewrites the include; niri applies it at once, to every
// keyboard. The system's layouts, when they are the ones asked for, come
// back from the session's own setting as soon as the include stops
// naming others.
func (a *Adapter) SetKeyboard(_ context.Context, kb compositor.KeyboardConfig) error {
	body, err := KeyboardKDL(kb)
	if err != nil {
		return err
	}
	a.mu.Lock()
	defer a.mu.Unlock()
	path := a.KeyboardPath()
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

// KeyboardState asks niri for the layouts and the active one.
func (a *Adapter) KeyboardState(context.Context) (compositor.KeyboardState, error) {
	var v struct {
		KeyboardLayouts struct {
			Names      []string `json:"names"`
			CurrentIdx int      `json:"current_idx"`
		} `json:"KeyboardLayouts"`
	}
	if err := a.request("KeyboardLayouts", &v); err != nil {
		return compositor.KeyboardState{}, err
	}
	k := v.KeyboardLayouts
	if len(k.Names) == 0 {
		return compositor.KeyboardState{}, nil
	}
	cur := k.CurrentIdx
	if cur < 0 || cur >= len(k.Names) {
		cur = 0
	}
	return compositor.KeyboardState{Names: k.Names, Current: cur, Live: true}, nil
}

// SwitchLayout switches to the next layout or to one.
func (a *Adapter) SwitchLayout(_ context.Context, index int) error {
	if index < 0 {
		return a.action("SwitchLayout", map[string]any{"layout": "Next"})
	}
	return a.action("SwitchLayout", map[string]any{"layout": map[string]any{"Index": index}})
}
