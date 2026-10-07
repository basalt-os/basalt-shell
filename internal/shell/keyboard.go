package shell

import (
	"context"
	"errors"
	"strings"
	"sync"

	"github.com/basalt-os/basalt-shell/internal/assistant"
	"github.com/basalt-os/basalt-shell/internal/compositor"
	"github.com/basalt-os/basalt-shell/internal/i18n"
	"github.com/basalt-os/basalt-shell/internal/keyboard"
)

// Settings, Keyboard: the person's layouts, the switch key, Caps Lock,
// the compose key and key repeat, kept in ~/.config/basalt/keyboard.conf
// and applied to the running session through the compositor adapter (sway
// IPC, niri's managed include) without a restart. A session starts with
// the system's keyboard (basalt-session) and the daemon applies the
// person's settings on top (the compositors also read a managed start-up
// file, so the first key already uses them). The system's keyboard, for
// the login screen and new accounts, is a proposal of the system
// assistant (keyboard.system), applied by its executor.

// kbdState is the keyboard part of the core.
type kbdState struct {
	mu       sync.Mutex
	loaded   bool
	settings keyboard.Settings
	problems []string
	reg      *keyboard.Registry
	regErr   error
	regDone  bool
	applyErr string
}

// keyboardAdapter is the compositor's keyboard, or nil.
func (c *Core) keyboardAdapter() compositor.Keyboard {
	k, _ := c.Comp.(compositor.Keyboard)
	return k
}

// kbdRegistry loads the system's XKB registry once.
func (c *Core) kbdRegistry() (*keyboard.Registry, error) {
	c.kbd.mu.Lock()
	defer c.kbd.mu.Unlock()
	if !c.kbd.regDone {
		c.kbd.reg, c.kbd.regErr = keyboard.LoadRegistry()
		c.kbd.regDone = true
	}
	return c.kbd.reg, c.kbd.regErr
}

func (c *Core) kbdPath() string { return keyboard.Path(c.ConfigDir) }

// kbdSettings are the person's settings (read once; written only here).
func (c *Core) kbdSettings() (keyboard.Settings, []string) {
	c.kbd.mu.Lock()
	defer c.kbd.mu.Unlock()
	if !c.kbd.loaded {
		s, problems, err := keyboard.Load(c.kbdPath())
		if err != nil {
			problems = append(problems, err.Error())
		}
		c.kbd.settings, c.kbd.problems, c.kbd.loaded = s, problems, true
	}
	return c.kbd.settings, c.kbd.problems
}

// kbdConfig is what the compositor gets for these settings.
func kbdConfig(s keyboard.Settings, sys keyboard.System) compositor.KeyboardConfig {
	x := s.Effective(sys.XKB)
	return compositor.KeyboardConfig{Layout: x.Layout(), Variant: x.Variant(), Model: x.Model, Options: x.Option(),
		RepeatDelay: s.RepeatDelay, RepeatRate: s.RepeatRate, SystemLayouts: len(s.Normalize().Layouts) == 0}
}

// ApplyKeyboard puts the person's settings on the session, at the start
// (the system's keyboard is already there: nothing to do when the person
// never changed anything).
func (c *Core) ApplyKeyboard(ctx context.Context) {
	k := c.keyboardAdapter()
	if k == nil {
		return
	}
	s, _ := c.kbdSettings()
	if s.IsDefault() {
		return
	}
	reg, _ := c.kbdRegistry()
	if err := s.Validate(reg); err != nil {
		c.kbd.mu.Lock()
		c.kbd.problems = append(c.kbd.problems, err.Error())
		c.kbd.mu.Unlock()
		return
	}
	err := k.SetKeyboard(ctx, kbdConfig(s, keyboard.ReadSystem()))
	c.kbd.mu.Lock()
	c.kbd.applyErr = ""
	if err != nil {
		c.kbd.applyErr = err.Error()
	}
	c.kbd.mu.Unlock()
}

// kbdChoiceInfo is a layout as the page shows it.
type kbdChoiceInfo struct {
	keyboard.Choice
	Name    string `json:"name"`
	English string `json:"english"`
	Short   string `json:"short"`
}

func (c *Core) kbdChoices(cs []keyboard.Choice, tr func(string) string) []kbdChoiceInfo {
	reg, _ := c.kbdRegistry()
	labels := keyboard.Labels(cs)
	out := make([]kbdChoiceInfo, len(cs))
	for i, ch := range cs {
		en := ch.String()
		if reg != nil {
			en = reg.Describe(ch)
		}
		name := en
		if tr != nil {
			name = tr(en)
		}
		out[i] = kbdChoiceInfo{Choice: ch, Name: name, English: en, Short: labels[i]}
	}
	return out
}

// KeyboardIndicator is the panel's layout indicator: the labels of the
// layouts in use and the active one.
func (c *Core) KeyboardIndicator(ctx context.Context) map[string]any {
	s, _ := c.kbdSettings()
	sys := keyboard.ReadSystem()
	eff := s.Effective(sys.XKB).Layouts
	labels := keyboard.Labels(eff)
	out := map[string]any{"labels": labels, "current": 0, "live": false, "names": []string{}}
	k := c.keyboardAdapter()
	if k == nil {
		return out
	}
	st, err := k.KeyboardState(ctx)
	if err != nil || !st.Live {
		return out
	}
	out["live"], out["names"], out["current"] = true, st.Names, st.Current
	if len(st.Names) != len(labels) {
		// Layouts the shell did not set (an input block of the person's
		// own): the compositor's names, shortened.
		short := make([]string, len(st.Names))
		for i, n := range st.Names {
			short[i] = strings.ToUpper(firstWord(n))
		}
		out["labels"] = short
	}
	return out
}

func firstWord(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.IndexAny(s, " (,"); i > 0 {
		s = s[:i]
	}
	if len(s) > 3 {
		s = s[:3]
	}
	return s
}

// KeyboardInfo is what Settings, Keyboard shows.
func (c *Core) KeyboardInfo(ctx context.Context) map[string]any {
	s, problems := c.kbdSettings()
	sys := keyboard.ReadSystem()
	tr := keyboard.Names(i18n.Locale())
	_, regErr := c.kbdRegistry()
	c.kbd.mu.Lock()
	applyErr := c.kbd.applyErr
	c.kbd.mu.Unlock()
	eff := s.Effective(sys.XKB)
	info := map[string]any{
		"settings":  s.Normalize(),
		"own":       len(s.Normalize().Layouts) > 0,
		"effective": c.kbdChoices(eff.Layouts, tr),
		"options":   eff.Options,
		"system": map[string]any{
			"layouts": c.kbdChoices(sys.XKB.Layouts, tr), "set": sys.Set, "console": sys.Console,
			"model": sys.XKB.Model, "options": sys.XKB.Options,
		},
		"same_as_system": keyboard.SameLayouts(eff.Layouts, sys.XKB.Layouts),
		"indicator":      c.KeyboardIndicator(ctx),
		"live":           c.keyboardAdapter() != nil,
		"compositor":     c.Comp.Name(),
		"assistant":      c.Assistant != nil && c.Assistant.Available(),
		"limits": map[string]any{
			"layouts": keyboard.MaxLayouts, "delay_min": keyboard.MinRepeatDelay, "delay_max": keyboard.MaxRepeatDelay,
			"rate_min": keyboard.MinRepeatRate, "rate_max": keyboard.MaxRepeatRate,
			"delay_default": keyboard.DefaultRepeatDelay, "rate_default": keyboard.DefaultRepeatRate,
		},
		"problems": problems,
	}
	if regErr != nil {
		info["registry_error"] = regErr.Error()
	}
	if applyErr != "" {
		info["apply_error"] = applyErr
	}
	return info
}

// KeyboardLayouts is the picker's list: every layout and variant the
// system knows, named in the session's language where xkeyboard-config
// translates it.
func (c *Core) KeyboardLayouts() ([]keyboard.Entry, error) {
	reg, err := c.kbdRegistry()
	if err != nil {
		return nil, err
	}
	return reg.Entries(keyboard.Names(i18n.Locale())), nil
}

// SetKeyboard checks, saves and applies the person's settings (the shell
// UI only, from Settings).
func (c *Core) SetKeyboard(ctx context.Context, s keyboard.Settings) error {
	reg, err := c.kbdRegistry()
	if err != nil {
		return errors.New(i18n.G("The keyboard layouts of this system could not be read: %s", err.Error()))
	}
	s = s.Normalize()
	if err := s.Validate(reg); err != nil {
		_, _ = c.Audit.Append("refuse", "ui", "keyboard settings refused", map[string]any{"error": err.Error()})
		return errors.New(i18n.G("These keyboard settings cannot be used: %s", err.Error()))
	}
	if err := keyboard.Save(c.kbdPath(), s); err != nil {
		return err
	}
	c.kbd.mu.Lock()
	c.kbd.settings, c.kbd.problems, c.kbd.loaded = s, nil, true
	c.kbd.mu.Unlock()
	sys := keyboard.ReadSystem()
	cfg := kbdConfig(s, sys)
	data := map[string]any{"layouts": cfg.Layout, "variants": cfg.Variant, "options": cfg.Options,
		"repeat_delay": s.RepeatDelay, "repeat_rate": s.RepeatRate, "system_layouts": cfg.SystemLayouts}
	var applyErr error
	if k := c.keyboardAdapter(); k != nil {
		applyErr = k.SetKeyboard(ctx, cfg)
	}
	c.kbd.mu.Lock()
	c.kbd.applyErr = ""
	if applyErr != nil {
		c.kbd.applyErr = applyErr.Error()
	}
	c.kbd.mu.Unlock()
	if applyErr != nil {
		data["error"] = applyErr.Error()
		_, _ = c.Audit.Append("fail", "ui", "keyboard settings saved, not applied", data)
		return errors.New(i18n.G("Saved, but the compositor did not take them: %s", applyErr.Error()))
	}
	_, _ = c.Audit.Append("apply", "ui", "keyboard settings", data)
	c.Broadcast("keyboard", c.KeyboardIndicator(ctx))
	return nil
}

// SwitchKeyboardLayout goes to the next layout (index < 0) or to one.
func (c *Core) SwitchKeyboardLayout(ctx context.Context, index int) error {
	k := c.keyboardAdapter()
	if k == nil {
		return compositor.ErrUnsupported
	}
	if err := k.SwitchLayout(ctx, index); err != nil {
		return err
	}
	c.Broadcast("keyboard", c.KeyboardIndicator(ctx))
	return nil
}

// KeyboardSystemProposal stores the assistant's keyboard.system proposal
// for the layouts and options the person uses now (their own, or the
// given ones): the login screen, the console and new accounts.
func (c *Core) KeyboardSystemProposal(ctx context.Context) (assistant.Proposal, error) {
	if c.Assistant == nil || !c.Assistant.Available() {
		return assistant.Proposal{}, errors.New(i18n.G("The system assistant (basalt) is not installed: changing the keyboard of the login screen needs it."))
	}
	s, _ := c.kbdSettings()
	reg, err := c.kbdRegistry()
	if err != nil {
		return assistant.Proposal{}, err
	}
	if err := s.Validate(reg); err != nil {
		return assistant.Proposal{}, err
	}
	x := s.Effective(keyboard.ReadSystem().XKB)
	ls := make([]string, len(x.Layouts))
	for i, ch := range x.Layouts {
		ls[i] = ch.String()
	}
	layouts := strings.Join(ls, ",")
	p, err := c.Assistant.KeyboardPropose(ctx, layouts, x.Option())
	if err != nil && strings.Contains(err.Error(), `unknown command "keyboard"`) {
		// An assistant older than basalt keyboard (0.12.2).
		err = errors.New(i18n.G("The system assistant on this computer is older than this page. A coming update of Basalt OS brings it."))
	}
	data := map[string]any{"assistant_proposal": p.ID, "layouts": layouts, "options": x.Option()}
	if err != nil {
		data["error"] = err.Error()
	}
	_, _ = c.Audit.Append("propose", "ui", "keyboard.system", data)
	return p, err
}

// keyboardChanged follows a compositor event: the layouts or the active
// layout changed.
func (c *Core) keyboardChanged(ctx context.Context) {
	c.Broadcast("keyboard", c.KeyboardIndicator(ctx))
}
