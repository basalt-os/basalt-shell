// Package agentio is the last-resort "computer use" layer of the shell:
// screen capture through the wlroots screencopy protocols (grim) and
// synthetic keyboard input through the virtual-keyboard protocol (wtype).
// Pointer input goes through the compositor adapter (sway's seat cursor
// commands). The shell daemon is the only process that runs these tools:
// agents ask the daemon, which applies the confirmation, control-session
// and audit rules first. Confined agents cannot reach the Wayland socket
// themselves (SELinux, see selinux/).
package agentio

import (
	"bytes"
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"os/exec"
	"regexp"
	"strings"
	"time"
	"unicode/utf8"
)

// Tools found on this system.
type Tools struct {
	Grim  string `json:"grim,omitempty"`
	Wtype string `json:"wtype,omitempty"`
}

// Find looks the tools up in PATH.
func Find() Tools {
	var t Tools
	t.Grim, _ = exec.LookPath("grim")
	t.Wtype, _ = exec.LookPath("wtype")
	return t
}

// Capture is one screenshot. PNG is never sent to the shell UI or
// written to the audit log; only the agent that asked receives it.
type Capture struct {
	PNG    []byte `json:"-"`
	Width  int    `json:"width"`
	Height int    `json:"height"`
	Bytes  int    `json:"bytes"`
	Target string `json:"target"`
	Method string `json:"method"` // output, toplevel, region
}

// CaptureSpec says what to capture.
type CaptureSpec struct {
	Output   string  // output name (grim -o)
	Toplevel string  // ext-foreign-toplevel identifier (grim -T)
	Region   string  // "x,y wxh" in layout coordinates (grim -g); with Toplevel, the fallback
	Scale    float64 // output scale factor, 0 = native
	Label    string  // what the person saw ("output HEADLESS-1", "window foot")
}

var reRegion = regexp.MustCompile(`^-?\d{1,6},-?\d{1,6} \d{1,6}x\d{1,6}$`)
var reName = regexp.MustCompile(`^[A-Za-z0-9._:-]{1,64}$`)

// Screenshot runs grim and returns the PNG. A window is captured alone
// through ext-image-copy-capture when the compositor offers it; else
// (sway 1.11 lists toplevels but cannot capture one) its area of the
// screen is captured, method "region".
func (t Tools) Screenshot(ctx context.Context, s CaptureSpec) (*Capture, error) {
	if s.Toplevel != "" && s.Region != "" {
		c, err := t.screenshot(ctx, CaptureSpec{Toplevel: s.Toplevel, Scale: s.Scale, Label: s.Label})
		if err == nil {
			return c, nil
		}
		s.Toplevel = ""
	}
	return t.screenshot(ctx, s)
}

func (t Tools) screenshot(ctx context.Context, s CaptureSpec) (*Capture, error) {
	if t.Grim == "" {
		return nil, errors.New("grim is not installed (screen capture needs it)")
	}
	args := []string{"-t", "png", "-l", "6"}
	method := "output"
	switch {
	case s.Toplevel != "":
		if !reName.MatchString(s.Toplevel) {
			return nil, errors.New("invalid toplevel identifier")
		}
		args, method = append(args, "-T", s.Toplevel), "toplevel"
	case s.Region != "":
		if !reRegion.MatchString(s.Region) {
			return nil, errors.New("invalid region")
		}
		args, method = append(args, "-g", s.Region), "region"
	case s.Output != "":
		if !reName.MatchString(s.Output) {
			return nil, errors.New("invalid output name")
		}
		args = append(args, "-o", s.Output)
	}
	if s.Scale > 0 && s.Scale < 1 {
		args = append(args, "-s", fmt.Sprintf("%.3f", s.Scale))
	}
	args = append(args, "-")
	ctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, t.Grim, args...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	if err := cmd.Run(); err != nil {
		return nil, fmt.Errorf("grim: %v: %s", err, strings.TrimSpace(errb.String()))
	}
	w, h, err := pngSize(out.Bytes())
	if err != nil {
		return nil, err
	}
	return &Capture{PNG: out.Bytes(), Width: w, Height: h, Bytes: out.Len(), Target: s.Label, Method: method}, nil
}

func pngSize(b []byte) (int, int, error) {
	if len(b) < 24 || string(b[1:4]) != "PNG" || string(b[12:16]) != "IHDR" {
		return 0, 0, errors.New("grim: not a PNG image")
	}
	return int(binary.BigEndian.Uint32(b[16:20])), int(binary.BigEndian.Uint32(b[20:24])), nil
}

// MaxText is the most text one input call may type.
const MaxText = 2000

// TypeText types text with the virtual keyboard.
func (t Tools) TypeText(ctx context.Context, text string) error {
	if t.Wtype == "" {
		return errors.New("wtype is not installed (keyboard input needs it)")
	}
	if text == "" || len(text) > MaxText || !utf8.ValidString(text) {
		return fmt.Errorf("text must be 1 to %d bytes of UTF-8", MaxText)
	}
	return t.run(ctx, t.Wtype, "-d", "8", "--", text)
}

var modNames = map[string]string{
	"ctrl": "ctrl", "control": "ctrl", "shift": "shift", "alt": "alt",
	"super": "logo", "logo": "logo", "meta": "logo", "win": "logo", "altgr": "altgr",
}

var reKey = regexp.MustCompile(`^[A-Za-z0-9_]{1,32}$`)

// keyAliases maps friendly names to XKB keysym names.
var keyAliases = map[string]string{
	"enter": "Return", "return": "Return", "esc": "Escape", "escape": "Escape", "tab": "Tab",
	"space": "space", "backspace": "BackSpace", "delete": "Delete", "del": "Delete",
	"up": "Up", "down": "Down", "left": "Left", "right": "Right", "home": "Home", "end": "End",
	"pageup": "Prior", "pagedown": "Next", "insert": "Insert",
}

// ParseCombo turns "ctrl+shift+t" into modifiers and an XKB keysym name.
func ParseCombo(combo string) (mods []string, key string, err error) {
	parts := strings.Split(strings.TrimSpace(combo), "+")
	if len(parts) == 0 || len(parts) > 5 {
		return nil, "", errors.New("key: give KEY or MOD+...+KEY")
	}
	for _, p := range parts[:len(parts)-1] {
		m, ok := modNames[strings.ToLower(strings.TrimSpace(p))]
		if !ok {
			return nil, "", fmt.Errorf("unknown modifier %q (ctrl, shift, alt, super, altgr)", p)
		}
		mods = append(mods, m)
	}
	key = strings.TrimSpace(parts[len(parts)-1])
	if a, ok := keyAliases[strings.ToLower(key)]; ok {
		key = a
	}
	if !reKey.MatchString(key) {
		return nil, "", fmt.Errorf("invalid key name %q (an XKB keysym such as Return, Tab, a, F5)", key)
	}
	return mods, key, nil
}

// Key presses a key combination with the virtual keyboard.
func (t Tools) Key(ctx context.Context, combo string) error {
	if t.Wtype == "" {
		return errors.New("wtype is not installed (keyboard input needs it)")
	}
	mods, key, err := ParseCombo(combo)
	if err != nil {
		return err
	}
	var args []string
	for _, m := range mods {
		args = append(args, "-M", m)
	}
	args = append(args, "-k", key)
	for i := len(mods) - 1; i >= 0; i-- {
		args = append(args, "-m", mods[i])
	}
	return t.run(ctx, t.Wtype, args...)
}

func (t Tools) run(ctx context.Context, prog string, args ...string) error {
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, prog, args...)
	var errb bytes.Buffer
	cmd.Stderr = &errb
	if err := cmd.Run(); err != nil {
		return fmt.Errorf("%s: %v: %s", prog, err, strings.TrimSpace(errb.String()))
	}
	return nil
}
