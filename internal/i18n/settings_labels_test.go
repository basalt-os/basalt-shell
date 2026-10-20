package i18n

import (
	"os"
	"regexp"
	"strings"
	"testing"
)

var (
	// The sidebar list of the settings window: `readonly property var pages: [ ... ]`.
	reSettingsPages = regexp.MustCompile(`(?s)property var pages:\s*\[(.*?)\n\s*\]`)
	// One page entry: { id: "...", label: <expr>, icon: ... }.
	reSettingsPage = regexp.MustCompile(`\{\s*id:\s*"([^"]+)",\s*label:\s*(.+?),\s*icon:`)
	// A label that goes through the catalog.
	reTranslated = regexp.MustCompile(`^Tr\.t\("(?:[^"\\]|\\.)*"\)$`)
	// A person-facing property set to a plain string literal.
	reLiteralProp = regexp.MustCompile(`\b(text|title|label|placeholder):\s*"([^"]*[A-Za-z][^"]*)"`)
)

// TestSettingsLabelsTranslated: every page of the settings sidebar has a
// translated label (Tr.t("...")), and the settings window sets no visible
// text, title, label or placeholder to a plain string literal. A literal
// would show English in every other language.
func TestSettingsLabelsTranslated(t *testing.T) {
	b, err := os.ReadFile("../../shell/Settings.qml")
	if err != nil {
		t.Fatal(err)
	}
	src := string(b)
	m := reSettingsPages.FindStringSubmatch(src)
	if m == nil {
		t.Fatal("pages list not found in shell/Settings.qml: is the scan broken?")
	}
	pages := reSettingsPage.FindAllStringSubmatch(m[1], -1)
	if len(pages) < 5 {
		t.Fatalf("only %d sidebar pages found: is the scan broken?", len(pages))
	}
	for _, p := range pages {
		if label := strings.TrimSpace(p[2]); !reTranslated.MatchString(label) {
			t.Errorf("settings page %q: label %s is not wrapped in Tr.t(\"...\")", p[1], label)
		}
	}
	for _, l := range reLiteralProp.FindAllStringSubmatch(src, -1) {
		t.Errorf("shell/Settings.qml: %s: %q is a literal, use Tr.t(\"...\")", l[1], l[2])
	}
}
