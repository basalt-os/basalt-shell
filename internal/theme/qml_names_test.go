package theme

import (
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"testing"
)

// TestQMLThemeNames: every Theme.<name> the shell's QML reads is a
// property, function or signal of shell/Theme.qml. QML reads an unknown
// name as undefined without an error at load: a size computed from one is
// NaN and the item is drawn 0 px wide (the command bar's check lines
// showed their icon and no text, from a Theme.s8 that does not exist).
func TestQMLThemeNames(t *testing.T) {
	src, err := os.ReadFile("../../shell/Theme.qml")
	if err != nil {
		t.Fatal(err)
	}
	defined := map[string]bool{}
	for _, re := range []*regexp.Regexp{
		regexp.MustCompile(`(?m)^\s*(?:readonly\s+)?property\s+\S+\s+(\w+)`),
		regexp.MustCompile(`(?m)^\s*function\s+(\w+)`),
		regexp.MustCompile(`(?m)^\s*signal\s+(\w+)`),
	} {
		for _, m := range re.FindAllStringSubmatch(string(src), -1) {
			defined[m[1]] = true
		}
	}
	if len(defined) < 20 {
		t.Fatalf("only %d names found in Theme.qml: the patterns no longer match it", len(defined))
	}
	files, err := filepath.Glob("../../shell/*.qml")
	if err != nil || len(files) == 0 {
		t.Fatalf("no QML files: %v", err)
	}
	use := regexp.MustCompile(`\bTheme\.(\w+)`)
	var bad []string
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		for _, m := range use.FindAllStringSubmatch(string(b), -1) {
			if !defined[m[1]] {
				bad = append(bad, filepath.Base(f)+": Theme."+m[1])
			}
		}
	}
	sort.Strings(bad)
	for _, b := range bad {
		t.Errorf("%s is not defined in Theme.qml", b)
	}
}
