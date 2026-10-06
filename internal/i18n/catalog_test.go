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
	return single, plural
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
