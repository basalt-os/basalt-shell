package i18n

import (
	"encoding/json"
	"go/ast"
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"

	"github.com/basalt-os/basalt-shell/internal/theme"
)

// msgids collects the English messages of the sources: i18n.G and i18n.N
// calls in Go (literal strings only), Tr.t and Tr.n calls in the QML.
func msgids(t *testing.T, root string) (single map[string]string, plural map[string]string) {
	single, plural = map[string]string{}, map[string]string{}
	fs := token.NewFileSet()
	err := filepath.Walk(root, func(p string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		if info.IsDir() && (info.Name() == "lab" || info.Name() == "media" || info.Name() == "build") {
			return filepath.SkipDir
		}
		switch {
		case strings.HasSuffix(p, ".go") && !strings.HasSuffix(p, "_test.go"):
			f, err := parser.ParseFile(fs, p, nil, 0)
			if err != nil {
				return err
			}
			ast.Inspect(f, func(n ast.Node) bool {
				c, ok := n.(*ast.CallExpr)
				if !ok {
					return true
				}
				// i18n.G and i18n.N, or G and N inside this package.
				var fn string
				switch f := c.Fun.(type) {
				case *ast.SelectorExpr:
					if id, ok := f.X.(*ast.Ident); ok && id.Name == "i18n" {
						fn = f.Sel.Name
					}
				case *ast.Ident:
					if f.Name == "G" || f.Name == "N" {
						if filepath.Base(filepath.Dir(p)) == "i18n" {
							fn = f.Name
						}
					}
				}
				if (fn != "G" && fn != "N") || len(c.Args) == 0 {
					return true
				}
				lit := func(i int) string {
					bl, ok := c.Args[i].(*ast.BasicLit)
					if !ok {
						t.Errorf("%s: i18n.%s needs a literal message (whole sentences, never assembled)", fs.Position(c.Pos()), fn)
						return ""
					}
					s, _ := strconv.Unquote(bl.Value)
					return s
				}
				if fn == "G" {
					single[lit(0)] = fs.Position(c.Pos()).String()
				} else if len(c.Args) >= 2 {
					plural[lit(0)] = lit(1)
				}
				return true
			})
		case strings.HasSuffix(p, ".qml"):
			b, err := os.ReadFile(p)
			if err != nil {
				return err
			}
			for _, m := range reQMLT.FindAllStringSubmatch(string(b), -1) {
				single[qmlUnquote(m[1])] = p
			}
			for _, m := range reQMLN.FindAllStringSubmatch(string(b), -1) {
				plural[qmlUnquote(m[1])] = qmlUnquote(m[2])
			}
		}
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	for id, where := range dataMsgids(t, root) {
		single[id] = where
	}
	return single, plural
}

// dataMsgids collects the messages the UI reads from data and translates
// with Tr.t(value): the label of every design token and the words of its
// choices (the easing curves keep their names), and the name and
// description of every shipped theme (themes/*.json).
func dataMsgids(t *testing.T, root string) map[string]string {
	ids := map[string]string{}
	for _, s := range theme.Specs {
		ids[s.Label] = "internal/theme/tokens.go: " + s.Key
		if s.Kind == theme.Enum && s.Key != "motion.easing" {
			for _, o := range s.Options {
				ids[o] = "internal/theme/tokens.go: " + s.Key
			}
		}
	}
	files, _ := filepath.Glob(filepath.Join(root, "themes", "*.json"))
	if len(files) == 0 {
		t.Fatal("no shipped themes in themes/")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var m struct{ Name, Description string }
		if err := json.Unmarshal(b, &m); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		if m.Name == "" || m.Description == "" {
			t.Fatalf("%s: a shipped theme needs a name and a description", f)
		}
		ids[m.Name] = f
		ids[m.Description] = f
	}
	for id, where := range actionMsgids(t, filepath.Join(root, "internal", "shell")) {
		ids[id] = where
	}
	return ids
}

// actionMsgids collects the title and the description of every typed
// action (the ActionDef literals of internal/shell): the daemon's catalog
// keeps them in English for agents and the MCP schema, and Settings,
// Assistant and AI shows them with Tr.t(value). Read from the source (the
// shell package imports this one, a test import would be a cycle); the
// descriptions of parameters are for agents only and are not collected.
func actionMsgids(t *testing.T, dir string) map[string]string {
	ids := map[string]string{}
	fs := token.NewFileSet()
	files, _ := filepath.Glob(filepath.Join(dir, "*.go"))
	isDef := func(e ast.Expr) bool {
		if s, ok := e.(*ast.StarExpr); ok {
			e = s.X
		}
		id, ok := e.(*ast.Ident)
		return ok && id.Name == "ActionDef"
	}
	defs := 0
	collect := func(cl *ast.CompositeLit) {
		defs++
		for _, el := range cl.Elts {
			kv, ok := el.(*ast.KeyValueExpr)
			if !ok {
				continue
			}
			k, ok := kv.Key.(*ast.Ident)
			if !ok || (k.Name != "Title" && k.Name != "Description") {
				continue
			}
			bl, ok := kv.Value.(*ast.BasicLit)
			if !ok || bl.Kind != token.STRING {
				t.Errorf("%s: an action's %s needs a literal string (the UI translates it)", fs.Position(kv.Pos()), k.Name)
				continue
			}
			s, _ := strconv.Unquote(bl.Value)
			ids[s] = fs.Position(kv.Pos()).String()
		}
	}
	for _, p := range files {
		if strings.HasSuffix(p, "_test.go") {
			continue
		}
		f, err := parser.ParseFile(fs, p, nil, 0)
		if err != nil {
			t.Fatal(err)
		}
		ast.Inspect(f, func(n ast.Node) bool {
			cl, ok := n.(*ast.CompositeLit)
			if !ok {
				return true
			}
			switch typ := cl.Type.(type) {
			case *ast.Ident:
				if isDef(typ) {
					collect(cl)
				}
			case *ast.ArrayType:
				// []*ActionDef{{...}, {...}}: the elements leave the type out.
				if isDef(typ.Elt) {
					for _, el := range cl.Elts {
						if e, ok := el.(*ast.CompositeLit); ok && e.Type == nil {
							collect(e)
						}
					}
				}
			}
			return true
		})
	}
	if defs < 20 {
		t.Fatalf("only %d action definitions found in %s: is the scan broken?", defs, dir)
	}
	return ids
}

var (
	reQMLT = regexp.MustCompile(`Tr\.t\("((?:[^"\\]|\\.)*)"\)`)
	reQMLN = regexp.MustCompile(`Tr\.n\("((?:[^"\\]|\\.)*)",\s*"((?:[^"\\]|\\.)*)"`)
	reVerb = regexp.MustCompile(`%(?:\d+|[-+# 0]*[sdvqfx])`)
)

func qmlUnquote(s string) string {
	u, err := strconv.Unquote(`"` + s + `"`)
	if err != nil {
		return s
	}
	return u
}

func verbs(s string) string {
	v := reVerb.FindAllString(s, -1)
	sort.Strings(v)
	return strings.Join(v, " ")
}

// TestCatalogs: every message of the sources has a translation in every
// shipped catalog, with the same placeholders, and plural messages have
// both forms (ADR 0014: a missing translation is a review blocker).
func TestCatalogs(t *testing.T) {
	root := "../.."
	single, plural := msgids(t, root)
	if len(single) < 50 {
		t.Fatalf("only %d messages found: is the scan broken?", len(single))
	}
	files, _ := filepath.Glob(filepath.Join(root, "locale", "*.json"))
	if len(files) == 0 {
		t.Fatal("no catalogs in locale/")
	}
	for _, f := range files {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var cat map[string][]string
		if err := json.Unmarshal(b, &cat); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		name := filepath.Base(f)
		for id, where := range single {
			tr := cat[id]
			if len(tr) == 0 || tr[0] == "" {
				t.Errorf("%s: missing translation of %q (%s)", name, id, where)
				continue
			}
			if verbs(tr[0]) != verbs(id) {
				t.Errorf("%s: placeholders differ: %q -> %q", name, id, tr[0])
			}
		}
		for one, many := range plural {
			tr := cat[one]
			if len(tr) < 2 || tr[0] == "" || tr[1] == "" {
				t.Errorf("%s: missing plural forms of %q", name, one)
				continue
			}
			if verbs(tr[0]) != verbs(one) || verbs(tr[1]) != verbs(many) {
				t.Errorf("%s: placeholders differ: %q -> %q", name, one, tr)
			}
		}
		// No stale entries.
		for id := range cat {
			if _, ok := single[id]; ok {
				continue
			}
			if _, ok := plural[id]; ok {
				continue
			}
			t.Errorf("%s: %q is not used anywhere", name, id)
		}
	}
}

func TestTags(t *testing.T) {
	for in, want := range map[string]string{"pt_BR.UTF-8": "pt-BR", "pt-br": "pt-BR", "PT_BR": "pt-BR", "en": "en", "es-419": "es-419",
		"C": "", "POSIX": "", "auto": "", "": "", "português": ""} {
		if got := Tag(in); got != want {
			t.Errorf("Tag(%q) = %q, want %q", in, got, want)
		}
	}
	if Base("pt-BR") != "pt" || LocaleName("pt-BR") != "pt_BR" || !IsEnglish("en-GB") || IsEnglish("pt") {
		t.Error("helpers")
	}
	if EnglishName("pt-BR") != "Brazilian Portuguese (pt-BR)" || EnglishName("nl-NL") != "Dutch (nl-NL)" {
		t.Error(EnglishName("pt-BR"), EnglishName("nl-NL"))
	}
}

// The daemon's language loads by tag or locale name, from a directory.
func TestLoadByTag(t *testing.T) {
	dir := t.TempDir()
	_ = os.WriteFile(filepath.Join(dir, "pt_BR.json"), []byte(`{"Please confirm on the screen.":["Confirme na tela."]}`), 0o644)
	t.Setenv("BASALT_SHELL_LOCALE_DIR", dir)
	defer Load("en")
	Load("pt-BR")
	if Lang() != "pt_BR" || G("Please confirm on the screen.") != "Confirme na tela." {
		t.Errorf("%q %q", Lang(), G("Please confirm on the screen."))
	}
	if c := Catalog("pt-BR"); c == nil || Catalog("en-US") != nil {
		t.Error("Catalog")
	}
	Load("en-US")
	if Translated() || G("Please confirm on the screen.") != "Please confirm on the screen." {
		t.Error("English")
	}
}

// TestGreeterCatalogs: the login screen has its own catalogs
// (greeter/locale/<lang>.json, installed in /usr/share/basalt-greeter):
// every I18n.t("...") of its QML and every N_("...") of its JavaScript has
// a translation with the same placeholders, and no entry is unused.
func TestGreeterCatalogs(t *testing.T) {
	root := filepath.Join("..", "..", "greeter")
	reT := regexp.MustCompile(`I18n\.t\("((?:[^"\\]|\\.)*)"\)`)
	reN := regexp.MustCompile(`N_\("((?:[^"\\]|\\.)*)"\)`)
	ids := map[string]string{}
	files, _ := filepath.Glob(filepath.Join(root, "*.qml"))
	js, _ := filepath.Glob(filepath.Join(root, "*.js"))
	for _, p := range append(files, js...) {
		b, err := os.ReadFile(p)
		if err != nil {
			t.Fatal(err)
		}
		for _, re := range []*regexp.Regexp{reT, reN} {
			for _, m := range re.FindAllStringSubmatch(string(b), -1) {
				ids[qmlUnquote(m[1])] = p
			}
		}
	}
	if len(ids) < 20 {
		t.Fatalf("only %d messages found: is the scan broken?", len(ids))
	}
	cats, _ := filepath.Glob(filepath.Join(root, "locale", "*.json"))
	if len(cats) == 0 {
		t.Fatal("no catalogs in greeter/locale/")
	}
	for _, f := range cats {
		b, err := os.ReadFile(f)
		if err != nil {
			t.Fatal(err)
		}
		var cat map[string][]string
		if err := json.Unmarshal(b, &cat); err != nil {
			t.Fatalf("%s: %v", f, err)
		}
		name := filepath.Base(f)
		for id, where := range ids {
			tr := cat[id]
			if len(tr) == 0 || tr[0] == "" {
				t.Errorf("%s: missing translation of %q (%s)", name, id, where)
				continue
			}
			if verbs(tr[0]) != verbs(id) {
				t.Errorf("%s: placeholders differ: %q -> %q", name, id, tr[0])
			}
		}
		for id := range cat {
			if _, ok := ids[id]; !ok {
				t.Errorf("%s: %q is not used anywhere", name, id)
			}
		}
	}
}
