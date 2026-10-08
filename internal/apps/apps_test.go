package apps

import (
	"os"
	"path/filepath"
	"testing"
)

// Desktop entries as Fedora ships them (trimmed).
const (
	alacritty = `[Desktop Entry]
Type=Application
TryExec=alacritty
Exec=alacritty
Icon=Alacritty
Terminal=false
Categories=System;TerminalEmulator;
Name=Alacritty
GenericName=Terminal
Comment=A fast, cross-platform, OpenGL terminal emulator
StartupWMClass=Alacritty
Actions=New;

[Desktop Action New]
Name=New Terminal
Exec=alacritty
`
	mousepad = `[Desktop Entry]
Version=1.0
Exec=mousepad %U
Icon=org.xfce.mousepad
StartupNotify=true
Terminal=false
Type=Application
Categories=Utility;TextEditor;GTK;
MimeType=text/plain;
Name=Mousepad
Name[pt_BR]=Mousepad
GenericName=Text Editor
GenericName[pt]=Editor de texto
GenericName[pt_BR]=Editor de texto
Comment=Simple Text Editor
Comment[pt_BR]=Editor de texto simples
Keywords=text;editor;notepad;gtk;
Keywords[pt_BR]=texto;editor;bloco de notas;gtk;
`
	foot = `[Desktop Entry]
Type=Application
Exec=foot
Icon=foot
Terminal=false
Categories=System;TerminalEmulator;
Keywords=shell;prompt;command;commandline;
Name=Foot
GenericName=Terminal
Comment=A wayland native terminal emulator
`
	firefox = `[Desktop Entry]
Name=Firefox
GenericName=Web Browser
GenericName[pt_BR]=Navegador web
Comment=Browse the Web
Exec=firefox %u
Icon=firefox
Terminal=false
Type=Application
Categories=Network;WebBrowser;
Keywords=web;browser;internet;
`
	feather = `[Desktop Entry]
Name=FeatherPad
GenericName=Plain Text Editor
Comment=Lightweight Qt text editor
Exec=featherpad %U
Icon=featherpad
Terminal=false
Type=Application
Categories=Qt;Utility;TextEditor;
`
)

func entries(t *testing.T, lang string) []App {
	t.Helper()
	dir := t.TempDir()
	apps := filepath.Join(dir, "applications")
	if err := os.MkdirAll(apps, 0o755); err != nil {
		t.Fatal(err)
	}
	for name, body := range map[string]string{"Alacritty": alacritty, "org.xfce.mousepad": mousepad, "foot": foot,
		"firefox": firefox, "featherpad": feather} {
		if err := os.WriteFile(filepath.Join(apps, name+".desktop"), []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("XDG_DATA_HOME", dir)
	t.Setenv("XDG_DATA_DIRS", filepath.Join(dir, "none"))
	t.Setenv("HOME", dir)
	t.Setenv("LC_ALL", "")
	t.Setenv("LC_MESSAGES", "")
	t.Setenv("LANG", lang)
	return List()
}

func TestLocalizedNamesAndKinds(t *testing.T) {
	l := entries(t, "pt_BR.UTF-8")
	var m App
	for _, a := range l {
		if a.ID == "org.xfce.mousepad" {
			m = a
		}
	}
	if m.Name != "Mousepad" || m.Generic != "Editor de texto" || m.Comment != "Editor de texto simples" {
		t.Fatalf("localized fields: %+v", m)
	}
	// The untranslated generic name still finds it.
	found := false
	for _, a := range m.Aliases {
		found = found || a == "Text Editor"
	}
	if !found {
		t.Fatalf("aliases %v lack the untranslated generic name", m.Aliases)
	}
	if len(m.Categories) != 3 || m.Categories[1] != "TextEditor" {
		t.Fatalf("categories %v", m.Categories)
	}
	if len(m.Kinds) == 0 {
		t.Fatal("no kinds for TextEditor")
	}
}

func TestLocaleKeys(t *testing.T) {
	for in, want := range map[string]string{"pt_BR.UTF-8": "pt_BR pt", "sr_RS@latin": "sr_RS@latin sr_RS sr@latin sr", "en": "en", "C": ""} {
		got := ""
		for i, k := range localeKeys(in) {
			if i > 0 {
				got += " "
			}
			got += k
		}
		if got != want {
			t.Errorf("localeKeys(%q) = %q, want %q", in, got, want)
		}
	}
}

// The demo's bug: "text editor" opened Alacritty. A text editor must come
// first for its kind, in English and in Portuguese, and a terminal never
// before it.
func TestSearchRanksByKind(t *testing.T) {
	for _, lang := range []string{"en_US.UTF-8", "pt_BR.UTF-8"} {
		l := entries(t, lang)
		for q, want := range map[string]string{
			"text editor":               "org.xfce.mousepad",
			"Text Editor":               "org.xfce.mousepad",
			"terminal":                  "Alacritty",
			"mousepad":                  "org.xfce.mousepad",
			"org.xfce.mousepad.desktop": "org.xfce.mousepad",
			"browser":                   "firefox",
			"navegador":                 "firefox",
			"foot":                      "foot",
		} {
			r := Search(l, q)
			if len(r) == 0 || r[0].ID != want {
				ids := []string{}
				for _, a := range r {
					ids = append(ids, a.ID)
				}
				t.Errorf("%s: Search(%q) = %v, want %s first", lang, q, ids, want)
			}
			if a, ok := Find(l, q); !ok || a.ID != want {
				t.Errorf("%s: Find(%q) = %s", lang, q, a.ID)
			}
		}
		// Kind matches: a text editor first for "editor" and "editor de
		// texto" (both editors share the tier in English); no terminal for
		// "text editor" at all.
		for _, q := range []string{"editor", "editor de texto", "notepad", "bloco de notas"} {
			r := Search(l, q)
			if len(r) == 0 || (r[0].ID != "org.xfce.mousepad" && r[0].ID != "featherpad") {
				t.Errorf("%s: Search(%q) does not start with a text editor", lang, q)
			}
		}
		for _, a := range Search(l, "text editor") {
			if a.ID == "Alacritty" || a.ID == "foot" {
				t.Errorf("%s: a terminal matched \"text editor\"", lang)
			}
		}
		if _, ok := Find(l, "spreadsheet"); ok {
			t.Errorf("%s: an app matched a kind nobody has", lang)
		}
	}
}
