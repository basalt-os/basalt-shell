package i18n

import (
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"testing"
)

var (
	// A person-facing property of a QML item, up to the end of its line.
	reQMLLabelProp = regexp.MustCompile(`(?:^|[\s;{])(text|title|label|sub|placeholder|accessibleName|accessibleDescription|Accessible\.name|Accessible\.description):`)
	// The literals the catalog reads: the message of Tr.t, both forms of Tr.n.
	reQMLTrCall = regexp.MustCompile(`Tr\.t\(\s*"(?:[^"\\]|\\.)*"\s*\)|Tr\.n\(\s*"(?:[^"\\]|\\.)*"\s*,\s*"(?:[^"\\]|\\.)*"`)
	// A person-facing property set from JavaScript (status = "...").
	reQMLAssign = regexp.MustCompile(`(?:^|[\s;{(.])(status|text|title|label|error|message)\s*=([^=]|$)`)
	// Words a person reads: two letters in a row.
	reQMLWords = regexp.MustCompile(`[A-Za-z]{2}`)
	// A key combination ("Super+Shift+Esc"): key names stay as they are.
	reQMLKeys = regexp.MustCompile(`^(?:(?:Super|Ctrl|Shift|Alt)\+)+[A-Za-z0-9]+$`)
)

// qmlShown returns the string literals a property's value can show: the
// value runs to the ";", "," or "}" that ends it on its line; literals that
// are arguments of a call or an index (Qt.formatTime(t, "HH:mm"),
// Bus.overridden("x")) or compared (x === "selinux") are not shown.
func qmlShown(s string) []string {
	var out []string
	var stack []byte // 'c' a call or an index, 'g' a grouping, '{' a block
	inCall := func() bool {
		for _, b := range stack {
			if b == 'c' {
				return true
			}
		}
		return false
	}
	prev := func(i int) string { return strings.TrimRight(s[:i], " \t") }
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\'' || c == '`':
			j := i + 1
			for j < len(s) && s[j] != c {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			if j >= len(s) {
				return out
			}
			lit := s[i+1 : j]
			before, after := prev(i), strings.TrimLeft(s[j+1:], " \t")
			compared := strings.HasSuffix(before, "==") || strings.HasSuffix(before, "!=") ||
				strings.HasPrefix(after, "==") || strings.HasPrefix(after, "!=")
			if !inCall() && !compared {
				out = append(out, lit)
			}
			i = j
		case c == '(' || c == '[':
			b := prev(i)
			if b != "" && (b[len(b)-1] == ')' || b[len(b)-1] == ']' || b[len(b)-1] == '_' ||
				(b[len(b)-1] >= 'a' && b[len(b)-1] <= 'z') || (b[len(b)-1] >= 'A' && b[len(b)-1] <= 'Z') || (b[len(b)-1] >= '0' && b[len(b)-1] <= '9')) {
				stack = append(stack, 'c')
			} else {
				stack = append(stack, 'g')
			}
		case c == '{':
			stack = append(stack, '{')
		case c == ')' || c == ']' || c == '}':
			if len(stack) == 0 {
				return out
			}
			stack = stack[:len(stack)-1]
		case (c == ';' || c == ',') && len(stack) == 0:
			return out
		}
	}
	return out
}

// qmlLabelAllowed marks a line whose literal is not text to translate (a
// code snippet, a key name): the comment says why.
const qmlLabelAllowed = "// i18n: not text"

// TestQMLLabelsTranslated: no QML file of the shell shows English that
// skips the catalog. A person-facing property (text, title, label, sub,
// placeholder, the accessible name and description, or a status, text,
// title, label, error or message set from JavaScript) holds no string
// literal with words outside Tr.t("...") or Tr.n("...", "..."), also in a
// ternary or a concatenation, and qsTr is not used anywhere: the shell
// loads no Qt translations, so qsTr("...") always shows English.
func TestQMLLabelsTranslated(t *testing.T) {
	files, err := filepath.Glob("../../shell/*.qml")
	if err != nil || len(files) < 20 {
		t.Fatalf("only %d QML files found in shell/: is the scan broken?", len(files))
	}
	checked := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(f)
		for i, line := range strings.Split(string(b), "\n") {
			code := line
			if j := strings.Index(code, "//"); j >= 0 && !strings.Contains(code[:j], `"`) && !strings.Contains(code[:j], `'`) {
				code = code[:j] // a whole-line or trailing comment, outside any string
			}
			if strings.Contains(code, "qsTr(") {
				t.Errorf("%s:%d: qsTr shows English in every language, use Tr.t(\"...\") or Tr.n(...)", name, i+1)
			}
			if strings.Contains(line, qmlLabelAllowed) {
				continue
			}
			// Each person-facing property on the line (several items can
			// share one line): its value runs to the next property.
			code = reQMLTrCall.ReplaceAllString(code, "Tr()")
			var found [][]int
			for _, m := range reQMLLabelProp.FindAllStringSubmatchIndex(code, -1) {
				found = append(found, []int{m[2], m[3], m[1]})
			}
			for _, m := range reQMLAssign.FindAllStringSubmatchIndex(code, -1) {
				found = append(found, []int{m[2], m[3], m[4]})
			}
			for _, m := range found {
				checked++
				prop := code[m[0]:m[1]]
				for _, lit := range qmlShown(code[m[2]:]) {
					if reQMLWords.MatchString(lit) && !reQMLKeys.MatchString(lit) {
						t.Errorf("%s:%d: %s shows %q untranslated, use Tr.t(\"...\")", name, i+1, prop, lit)
					}
				}
			}
		}
	}
	if checked < 200 {
		t.Fatalf("only %d person-facing properties checked: is the scan broken?", checked)
	}
	if models := checkQMLModels(t, files); models < 2 {
		t.Fatalf("only %d string-array models checked: is the scan broken?", models)
	}
}

// A model given as an array literal ("model: [", up to its "]", over
// several lines too).
var reQMLModel = regexp.MustCompile(`(?:^|[\s;{])model:\s*\[`)

// checkQMLModels: a Repeater or a list whose model is an array of strings
// usually shows each string as is (text: modelData), so every element with
// words must be Tr.t("...") too ("light mode" stayed English in the command
// bar's suggestions that way). Arrays of arrays or objects (a value and its
// Tr.t label) are left to the label check; an array of values that the
// delegate turns into labels itself carries the "// i18n: not text" mark on
// its model line. It returns how many flat string arrays it checked.
func checkQMLModels(t *testing.T, files []string) int {
	checked := 0
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		name := filepath.Base(f)
		lines := strings.Split(string(b), "\n")
		// Comments out (outside strings), Tr calls reduced to Tr(), one
		// text with line starts to report positions.
		var code strings.Builder
		starts := make([]int, len(lines))
		for i, line := range lines {
			starts[i] = code.Len()
			c := line
			if j := strings.Index(c, "//"); j >= 0 && !strings.Contains(c[:j], `"`) && !strings.Contains(c[:j], `'`) {
				c = c[:j]
			}
			if strings.Contains(line, qmlLabelAllowed) {
				c = "" // the whole line is marked
			}
			code.WriteString(reQMLTrCall.ReplaceAllString(c, "Tr()"))
			code.WriteByte('\n')
		}
		src := code.String()
		lineOf := func(off int) int {
			n := 0
			for n+1 < len(starts) && starts[n+1] <= off {
				n++
			}
			return n + 1
		}
		for _, m := range reQMLModel.FindAllStringIndex(src, -1) {
			body, flat := qmlArrayBody(src[m[1]:])
			if !flat {
				continue
			}
			checked++
			for _, el := range qmlSplitTop(body) {
				for _, lit := range qmlShown(el) {
					if reQMLWords.MatchString(lit) && !reQMLKeys.MatchString(lit) {
						t.Errorf("%s:%d: model shows %q untranslated, use Tr.t(\"...\")", name, lineOf(m[0]), lit)
					}
				}
			}
		}
	}
	return checked
}

// qmlArrayBody returns the text of an array literal up to its closing "]"
// (s starts right after the "["), and whether it is flat: no nested array
// or object literal among its elements (calls and groupings are fine).
func qmlArrayBody(s string) (string, bool) {
	depth, flat := 0, true
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\'' || c == '`':
			j := i + 1
			for j < len(s) && s[j] != c {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			i = j
		case c == '(':
			depth++
		case c == ')':
			depth--
		case (c == '[' || c == '{') && depth == 0:
			flat = false
			depth++
		case (c == ']' || c == '}') && depth > 0:
			depth--
		case c == ']':
			return s[:i], flat
		}
	}
	return s, false
}

// qmlSplitTop splits an array's text at its top-level commas.
func qmlSplitTop(s string) []string {
	var out []string
	depth, start := 0, 0
	for i := 0; i < len(s); i++ {
		c := s[i]
		switch {
		case c == '"' || c == '\'' || c == '`':
			j := i + 1
			for j < len(s) && s[j] != c {
				if s[j] == '\\' {
					j++
				}
				j++
			}
			i = j
		case c == '(' || c == '[' || c == '{':
			depth++
		case c == ')' || c == ']' || c == '}':
			depth--
		case c == ',' && depth == 0:
			out = append(out, s[start:i])
			start = i + 1
		}
	}
	return append(out, s[start:])
}
