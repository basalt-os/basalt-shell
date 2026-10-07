package shell

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/basalt-os/basalt-shell/internal/keyboard"
)

// kbdCore is a core with the test registry and a system keyboard of br.
func kbdCore(t *testing.T) (*Core, string) {
	t.Helper()
	c, _, dir := newCore(t)
	oldPaths, oldX11, oldVC := keyboard.RegistryPaths, keyboard.X11Conf, keyboard.VConsoleConf
	keyboard.RegistryPaths = []string{"../keyboard/testdata/evdev.xml"}
	keyboard.X11Conf, keyboard.VConsoleConf = filepath.Join(dir, "00-keyboard.conf"), filepath.Join(dir, "vconsole.conf")
	t.Cleanup(func() { keyboard.RegistryPaths, keyboard.X11Conf, keyboard.VConsoleConf = oldPaths, oldX11, oldVC })
	_ = os.WriteFile(keyboard.X11Conf, []byte("Section \"InputClass\"\n  Option \"XkbLayout\" \"br\"\n  Option \"XkbModel\" \"pc105\"\nEndSection\n"), 0o644)
	return c, dir
}

// Settings, Keyboard: the person's layouts are checked, kept in their
// file, applied to the running session and logged; a new session (a new
// daemon) applies them again.
func TestSetKeyboard(t *testing.T) {
	c, dir := kbdCore(t)
	fk := c.Comp.(interface{ SwitchLayout(context.Context, int) error })
	ctx := context.Background()
	s := keyboard.Settings{Layouts: []keyboard.Choice{{Layout: "br"}, {Layout: "us", Variant: "intl"}}, Switch: keyboard.SwitchAltShift, Compose: keyboard.ComposeRAlt}
	if err := c.SetKeyboard(ctx, s); err != nil {
		t.Fatal(err)
	}
	b, err := os.ReadFile(filepath.Join(dir, "xdg", "basalt", "keyboard.conf"))
	if err != nil || !strings.Contains(string(b), "layouts = br, us(intl)") {
		t.Fatalf("file: %s %v", b, err)
	}
	ind := c.KeyboardIndicator(ctx)
	if ind["live"] != true || strings.Join(ind["labels"].([]string), ",") != "BR,US" || ind["current"] != 0 {
		t.Errorf("indicator: %v", ind)
	}
	_ = fk.SwitchLayout(ctx, -1)
	if ind := c.KeyboardIndicator(ctx); ind["current"] != 1 {
		t.Errorf("after switching: %v", ind)
	}
	if !strings.Contains(c.Audit.Tail(5)[len(c.Audit.Tail(5))-1].Text, "keyboard settings") {
		t.Error("not logged")
	}
	// A layout the registry does not know is refused, nothing written.
	if err := c.SetKeyboard(ctx, keyboard.Settings{Layouts: []keyboard.Choice{{Layout: "br", Variant: "abnt2"}}}); err == nil {
		t.Error("br(abnt2) accepted")
	}
	if b2, _ := os.ReadFile(filepath.Join(dir, "xdg", "basalt", "keyboard.conf")); string(b2) != string(b) {
		t.Error("the file changed after a refused change")
	}
	// The next session: a new core reads the file and applies it.
	c2, _, _ := newCore(t)
	c2.ConfigDir = c.ConfigDir
	c2.ApplyKeyboard(ctx)
	info := c2.KeyboardInfo(ctx)
	if info["own"] != true {
		t.Errorf("info: %v", info)
	}
	if got := c2.KeyboardIndicator(ctx)["labels"].([]string); strings.Join(got, ",") != "BR,US" {
		t.Errorf("next session: %v", got)
	}
}

// Back to the system's layouts: the start-up files stop naming layouts.
func TestKeyboardSystemLayouts(t *testing.T) {
	c, _ := kbdCore(t)
	ctx := context.Background()
	if err := c.SetKeyboard(ctx, keyboard.Settings{Caps: keyboard.CapsCtrl}); err != nil {
		t.Fatal(err)
	}
	info := c.KeyboardInfo(ctx)
	if info["own"] != false || info["same_as_system"] != true {
		t.Errorf("%v", info)
	}
	eff := info["effective"].([]kbdChoiceInfo)
	if len(eff) != 1 || eff[0].Layout != "br" || eff[0].English != "Portuguese (Brazil)" {
		t.Errorf("effective: %+v", eff)
	}
}

// The system's keyboard is a proposal of the assistant: without it, the
// page says so; the shell itself never sets it.
func TestKeyboardSystemNeedsTheAssistant(t *testing.T) {
	c, _ := kbdCore(t)
	if _, err := c.KeyboardSystemProposal(context.Background()); err == nil || !strings.Contains(err.Error(), "assistant") {
		t.Errorf("%v", err)
	}
}

// Agents read the keyboard but never change it or ask for the system's.
func TestKeyboardIPCAgents(t *testing.T) {
	c, dir := kbdCore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sock := filepath.Join(dir, "k.sock")
	srv := &Server{Core: c, Path: sock, UI: UICheck{Mode: UICheckExe, Executables: []string{"no-such-ui"}}}
	go srv.Listen(ctx)
	time.Sleep(100 * time.Millisecond)
	agent := dialT(t, sock, "agent")
	if r := agent.call("keyboard.state", map[string]any{}); !r.OK {
		t.Fatal(r.Error)
	}
	if r := agent.call("keyboard.layouts", map[string]any{}); !r.OK {
		t.Fatal(r.Error)
	}
	for _, op := range []string{"keyboard.set", "keyboard.switch", "keyboard.system"} {
		if r := agent.call(op, map[string]any{"layouts": []map[string]string{{"layout": "de"}}}); r.OK {
			t.Errorf("an agent ran %s", op)
		}
	}
	if _, err := os.Stat(filepath.Join(c.ConfigDir, "basalt", "keyboard.conf")); err == nil {
		t.Error("an agent wrote the keyboard settings")
	}
}
