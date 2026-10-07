package sway

import (
	"strings"
	"testing"

	"github.com/basalt-os/basalt-shell/internal/compositor"
)

// The commands keep every step compilable (the variant is cleared before
// the layouts change) and quote the lists (sway splits commands on
// commas outside quotes).
func TestKeyboardCommands(t *testing.T) {
	cmds, err := keyboardCommands(compositor.KeyboardConfig{Layout: "br,us", Variant: ",intl", Model: "pc105",
		Options: "grp:alt_shift_toggle,compose:ralt", RepeatDelay: 300})
	if err != nil {
		t.Fatal(err)
	}
	want := []string{
		`input type:keyboard xkb_variant ""`,
		`input type:keyboard xkb_model "pc105"`,
		`input type:keyboard xkb_layout "br,us"`,
		`input type:keyboard xkb_variant ",intl"`,
		`input type:keyboard xkb_options "grp:alt_shift_toggle,compose:ralt"`,
		`input type:keyboard repeat_delay 300`,
		`input type:keyboard repeat_rate 25`,
	}
	if strings.Join(cmds, "\n") != strings.Join(want, "\n") {
		t.Errorf("got\n%s", strings.Join(cmds, "\n"))
	}
	for _, bad := range []compositor.KeyboardConfig{{Layout: `br"; exec foot; "`}, {Layout: "us", Options: "a;b"}, {Layout: ""}, {Layout: "us", Variant: "x}"}} {
		if _, err := keyboardCommands(bad); err == nil {
			t.Errorf("%+v accepted", bad)
		}
		if _, err := KeyboardConf(bad); err == nil && bad.Layout != "" {
			t.Errorf("%+v written", bad)
		}
	}
}

func TestKeyboardConf(t *testing.T) {
	own, _ := KeyboardConf(compositor.KeyboardConfig{Layout: "us,br", Variant: "intl,", Options: "caps:escape", RepeatRate: 40})
	if !strings.Contains(own, "input type:keyboard {\n    xkb_layout \"us,br\"\n    xkb_variant \"intl,\"\n    xkb_options \"caps:escape\"\n    repeat_rate 40\n}\n") {
		t.Errorf("own layouts:\n%s", own)
	}
	// The system's layouts are not written: the next session follows them.
	sys, _ := KeyboardConf(compositor.KeyboardConfig{Layout: "br", Options: "compose:ralt", SystemLayouts: true})
	if strings.Contains(sys, "xkb_layout") || !strings.Contains(sys, `xkb_options "compose:ralt"`) {
		t.Errorf("system layouts:\n%s", sys)
	}
	if none, _ := KeyboardConf(compositor.KeyboardConfig{Layout: "br", SystemLayouts: true}); strings.Contains(none, "input type:keyboard") {
		t.Errorf("nothing to set:\n%s", none)
	}
}

// Virtual keyboards keep their client's keymap: the indicator reads a
// real keyboard, and a session with only virtual ones reports none.
func TestPickKeyboard(t *testing.T) {
	list := []input{
		{Identifier: "0:0:wlr_virtual_keyboard_v1", Name: "wlr_virtual_keyboard_v1", Type: "keyboard", LayoutNames: []string{"English (US)"}},
		{Identifier: "1267:12345:Mouse", Type: "pointer"},
		{Identifier: "1:1:AT_Translated_Set_2_keyboard", Type: "keyboard", LayoutNames: []string{"Portuguese (Brazil)", "English (US, intl., with dead keys)"}, Active: 1},
	}
	st, ok := pickKeyboard(list)
	if !ok || !st.Live || st.Current != 1 || len(st.Names) != 2 {
		t.Errorf("%+v", st)
	}
	if _, ok := pickKeyboard(list[:2]); ok {
		t.Error("a virtual keyboard was taken")
	}
	if !keyboardEvent([]byte(`{"change":"xkb_layout","input":{"identifier":"1:1:kbd","type":"keyboard"}}`)) ||
		keyboardEvent([]byte(`{"change":"xkb_layout","input":{"identifier":"0:0:wlr_virtual_keyboard_v1","type":"keyboard"}}`)) ||
		keyboardEvent([]byte(`{"change":"libinput_config","input":{"identifier":"1:1:kbd","type":"keyboard"}}`)) {
		t.Error("input events")
	}
}
