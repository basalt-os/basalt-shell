package keyboard

import (
	"bytes"
	"encoding/binary"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func testRegistry(t *testing.T) *Registry {
	t.Helper()
	r, err := LoadRegistry("testdata/evdev.xml", "testdata/missing-extras.xml")
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestRegistryParse(t *testing.T) {
	r := testRegistry(t)
	if len(r.Layouts) != 3 || r.Layouts[0].Name != "br" || r.Layouts[2].Name != "us" {
		t.Fatalf("layouts: %+v", r.Layouts)
	}
	if br := r.Layout("br"); br == nil || br.Short != "pt" || br.Description != "Portuguese (Brazil)" || len(br.Variants) != 2 {
		t.Errorf("br: %+v", br)
	}
	if !r.HasOption("grp:alt_shift_toggle") || !r.HasOption("compose:ralt") || r.HasOption("compose:caps") || r.HasOption("grp") {
		t.Error("options")
	}
	if got := r.Describe(Choice{Layout: "us", Variant: "intl"}); got != "English (US, intl., with dead keys)" {
		t.Errorf("describe: %q", got)
	}
	if _, err := LoadRegistry("testdata/none.xml"); err == nil {
		t.Error("no registry at all must be an error")
	}
	es := r.Entries(nil)
	if len(es) != 3+5 {
		t.Fatalf("entries: %d", len(es))
	}
	if es[0].Name != "English (US)" || es[0].Short != "US" {
		t.Errorf("first entry: %+v", es[0])
	}
}

// The registry the system ships, when there is one: the layouts Basalt
// OS's installer offers are in it.
func TestSystemRegistry(t *testing.T) {
	r, err := LoadRegistry()
	if err != nil {
		t.Skip("no XKB registry on this machine:", err)
	}
	for _, c := range []Choice{{Layout: "br"}, {Layout: "us", Variant: "intl"}, {Layout: "pt"}, {Layout: "de", Variant: "nodeadkeys"}} {
		if err := r.Check(c); err != nil {
			t.Error(err)
		}
	}
	for _, o := range []string{"grp:alt_shift_toggle", "grp:ctrl_shift_toggle", "grp:alts_toggle", "ctrl:nocaps", "caps:escape",
		"caps:swapescape", "caps:none", "compose:ralt", "compose:menu", "compose:rctrl", "compose:caps"} {
		if !r.HasOption(o) {
			t.Errorf("option %s missing", o)
		}
	}
}

func TestParseChoice(t *testing.T) {
	for in, want := range map[string]Choice{"br": {Layout: "br"}, " us(intl) ": {Layout: "us", Variant: "intl"}, "us(alt-intl)": {Layout: "us", Variant: "alt-intl"}} {
		if got, err := ParseChoice(in); err != nil || got != want {
			t.Errorf("%q: %+v %v", in, got, err)
		}
	}
	for _, bad := range []string{"", "BR", "br;reboot", "us(intl", "us()", "us(intl)(x)", "us intl", "$(id)", "../us", "us,br", `us("x")`} {
		if _, err := ParseChoice(bad); err == nil {
			t.Errorf("%q accepted", bad)
		}
	}
}

// Arbitrary XKB strings never pass: only what the registry lists.
func TestValidate(t *testing.T) {
	r := testRegistry(t)
	good := Settings{Layouts: []Choice{{Layout: "br"}, {Layout: "us", Variant: "intl"}}, Switch: SwitchAltShift, Caps: CapsCtrl,
		Compose: ComposeRAlt, RepeatDelay: 300, RepeatRate: 40}
	if err := good.Validate(r); err != nil {
		t.Fatal(err)
	}
	if err := Defaults().Validate(r); err != nil {
		t.Fatal(err)
	}
	bad := map[string]Settings{
		"unknown layout":     {Layouts: []Choice{{Layout: "xx"}}},
		"unknown variant":    {Layouts: []Choice{{Layout: "br", Variant: "abnt2"}}},
		"variant elsewhere":  {Layouts: []Choice{{Layout: "us", Variant: "nodeadkeys"}}},
		"injection":          {Layouts: []Choice{{Layout: "br\"; exec foot"}}},
		"variant injection":  {Layouts: []Choice{{Layout: "us", Variant: "intl,de"}}},
		"twice":              {Layouts: []Choice{{Layout: "br"}, {Layout: "br"}}},
		"five layouts":       {Layouts: []Choice{{Layout: "br"}, {Layout: "us"}, {Layout: "de"}, {Layout: "us", Variant: "intl"}, {Layout: "br", Variant: "thinkpad"}}},
		"switch":             {Switch: "grp:shifts_toggle"},
		"caps":               {Caps: "ctrl:swapcaps"},
		"compose":            {Compose: "compose:ralt"},
		"compose and caps":   {Compose: ComposeCaps, Caps: CapsEscape},
		"option not on this": {Compose: ComposeCaps},
		"delay low":          {RepeatDelay: 20},
		"rate high":          {RepeatRate: 500},
	}
	for name, s := range bad {
		if err := s.Validate(r); err == nil {
			t.Errorf("%s: accepted", name)
		}
	}
}

func TestEffective(t *testing.T) {
	sys := XKB{Layouts: []Choice{{Layout: "br"}}, Model: "pc105", Options: []string{"terminate:ctrl_alt_bksp", "grp:ctrl_shift_toggle", "caps:none"}}
	// Only Caps Lock changed: the system's layouts, its own options other
	// than the ones these settings own, then theirs.
	x := Settings{Caps: CapsEscape}.Effective(sys)
	if x.Layout() != "br" || x.Variant() != "" || x.Option() != "terminate:ctrl_alt_bksp,caps:escape" || x.Model != "pc105" {
		t.Errorf("%+v", x)
	}
	x = Settings{Layouts: []Choice{{Layout: "us", Variant: "intl"}, {Layout: "br"}}, Switch: SwitchAltShift}.Effective(sys)
	if x.Layout() != "us,br" || x.Variant() != "intl," || x.Option() != "terminate:ctrl_alt_bksp,grp:alt_shift_toggle" {
		t.Errorf("%+v", x)
	}
	if x := Defaults().Effective(XKB{}); x.Layout() != "us" {
		t.Errorf("nothing set anywhere: %+v", x)
	}
}

func TestLabels(t *testing.T) {
	got := Labels([]Choice{{Layout: "us"}, {Layout: "br"}, {Layout: "us", Variant: "intl"}})
	if !reflect.DeepEqual(got, []string{"US1", "BR", "US2"}) {
		t.Errorf("%v", got)
	}
}

func TestSaveLoad(t *testing.T) {
	path := filepath.Join(t.TempDir(), "basalt", FileName)
	if s, problems, err := Load(path); err != nil || len(problems) != 0 || !s.IsDefault() {
		t.Fatalf("missing file: %+v %v %v", s, problems, err)
	}
	in := Settings{Layouts: []Choice{{Layout: "br"}, {Layout: "us", Variant: "intl"}}, Switch: SwitchCtrlShift, Caps: CapsSwapEscape,
		Compose: ComposeMenu, RepeatDelay: 250, RepeatRate: 50}
	if err := Save(path, in); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if !strings.Contains(string(b), "layouts = br, us(intl)\n") {
		t.Errorf("file:\n%s", b)
	}
	out, problems, err := Load(path)
	if err != nil || len(problems) != 0 || !reflect.DeepEqual(out, in) {
		t.Errorf("round trip: %+v %v %v", out, problems, err)
	}
	// Back to the system's layouts.
	if err := Save(path, Settings{}); err != nil {
		t.Fatal(err)
	}
	if out, _, _ := Load(path); !out.IsDefault() {
		t.Errorf("defaults: %+v", out)
	}
	// Saving refuses what could not be read back.
	if err := Save(path, Settings{Layouts: []Choice{{Layout: "us\nswitch = x"}}}); err == nil {
		t.Error("a newline was written")
	}
	// A hand-edited file: what cannot be read is reported, the rest kept.
	_ = os.WriteFile(path, []byte("[keyboard]\nlayouts = br, us(intl, de\nrepeat_rate = fast\ncompose = right-alt # mine\n"), 0o644)
	out, problems, _ = Load(path)
	if len(problems) != 2 || !reflect.DeepEqual(out.Layouts, []Choice{{Layout: "br"}, {Layout: "de"}}) || out.Compose != ComposeRAlt {
		t.Errorf("hand edited: %+v %v", out, problems)
	}
}

func TestReadSystem(t *testing.T) {
	dir := t.TempDir()
	defer func(x, v string) { X11Conf, VConsoleConf = x, v }(X11Conf, VConsoleConf)
	X11Conf, VConsoleConf = filepath.Join(dir, "00-keyboard.conf"), filepath.Join(dir, "vconsole.conf")
	if s := ReadSystem(); s.Set || s.XKB.Layout() != "us" {
		t.Errorf("nothing: %+v", s)
	}
	_ = os.WriteFile(VConsoleConf, []byte("KEYMAP=\"br-abnt2\"\nFONT=eurlatgr\n"), 0o644)
	if s := ReadSystem(); !s.Set || s.XKB.Layout() != "br" || s.Console != "br-abnt2" {
		t.Errorf("console only: %+v", s)
	}
	_ = os.WriteFile(X11Conf, []byte(`# Written by systemd-localed(8)
Section "InputClass"
        Identifier "system-keyboard"
        MatchIsKeyboard "on"
        Option "XkbLayout" "br,us"
        Option "XkbModel" "pc105"
        Option "XkbVariant" ",intl"
        Option "XkbOptions" "grp:alt_shift_toggle,terminate:ctrl_alt_bksp"
EndSection
`), 0o644)
	s := ReadSystem()
	want := []Choice{{Layout: "br"}, {Layout: "us", Variant: "intl"}}
	if !reflect.DeepEqual(s.XKB.Layouts, want) || s.XKB.Model != "pc105" || s.XKB.Option() != "grp:alt_shift_toggle,terminate:ctrl_alt_bksp" {
		t.Errorf("x11: %+v", s)
	}
}

// A small .mo catalog, as msgfmt writes it.
func writeMO(t *testing.T, path string, msgs map[string]string) {
	t.Helper()
	var keys []string
	for k := range msgs {
		keys = append(keys, k)
	}
	n := len(keys)
	var ids, strs bytes.Buffer
	head := 28
	origTab, transTab := head, head+n*8
	data := transTab + n*8
	var otab, ttab []uint32
	for _, k := range keys {
		otab = append(otab, uint32(len(k)), uint32(data+ids.Len()))
		ids.WriteString(k)
		ids.WriteByte(0)
	}
	for _, k := range keys {
		v := msgs[k]
		ttab = append(ttab, uint32(len(v)), uint32(data+ids.Len()+strs.Len()))
		strs.WriteString(v)
		strs.WriteByte(0)
	}
	var b bytes.Buffer
	for _, v := range []uint32{0x950412de, 0, uint32(n), uint32(origTab), uint32(transTab), 0, 0} {
		_ = binary.Write(&b, binary.LittleEndian, v)
	}
	_ = binary.Write(&b, binary.LittleEndian, otab)
	_ = binary.Write(&b, binary.LittleEndian, ttab)
	b.Write(ids.Bytes())
	b.Write(strs.Bytes())
	_ = os.MkdirAll(filepath.Dir(path), 0o755)
	if err := os.WriteFile(path, b.Bytes(), 0o644); err != nil {
		t.Fatal(err)
	}
}

func TestNames(t *testing.T) {
	dir := t.TempDir()
	defer func(d string) { LocaleDir = d }(LocaleDir)
	LocaleDir = dir
	writeMO(t, filepath.Join(dir, "pt_BR", "LC_MESSAGES", "xkeyboard-config.mo"), map[string]string{
		"": "Content-Type: text/plain; charset=UTF-8\n", "German": "Alemão", "English (US)": "Inglês (EUA)"})
	tr := Names("pt_BR.UTF-8")
	if tr == nil || tr("German") != "Alemão" || tr("Portuguese (Brazil)") != "Portuguese (Brazil)" {
		t.Fatal("translations")
	}
	if Names("en_US.UTF-8") != nil || Names("C") != nil || Names("fr_FR") != nil {
		t.Error("no catalog: English")
	}
	es := testRegistry(t).Entries(tr)
	found := false
	for _, e := range es {
		if e.Layout == "de" && e.Variant == "" {
			found = e.Name == "Alemão" && e.English == "German"
		}
	}
	if !found {
		t.Error("entries are not translated")
	}
}
