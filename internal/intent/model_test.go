package intent

import (
	"context"
	"encoding/json"
	"net"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/basalt-os/basalt-shell/internal/theme"
)

func ctxT() Context {
	return Context{Tokens: tokens(), Themes: []theme.Meta{{ID: "lichen", Name: "Lichen"}, {ID: "tide", Name: "Tide"}}}
}

func TestGround(t *testing.T) {
	c := ctxT()
	cases := []struct {
		text    string
		in      []ModelIntent
		action  string // first call, "" for none
		clarify bool
	}{
		{"deixa mais escuro com cantos arredondados", []ModelIntent{{Intent: "darker"}, {Intent: "rounder_corners"}}, "theme.set_tokens", false},
		{"abre o editor de texto", []ModelIntent{{Intent: "open_app", Target: "editor de texto"}}, "app.launch", false},
		// An application the person did not mention.
		{"abre o editor", []ModelIntent{{Intent: "open_app", Target: "firefox"}}, "", true},
		{"put firefox on desktop two", []ModelIntent{{Intent: "move_window", Target: "firefox", Number: 2}}, "window.to_workspace", false},
		// A workspace number the person did not say.
		{"put firefox on another desktop", []ModelIntent{{Intent: "move_window", Target: "firefox", Number: 3}}, "", true},
		{"usa o tema tide", []ModelIntent{{Intent: "use_theme", Target: "tide"}}, "theme.switch", false},
		{"use the ocean theme", []ModelIntent{{Intent: "use_theme", Target: "ocean"}}, "", true},
		{"fecha esta janela", []ModelIntent{{Intent: "close_window", Target: "focused"}}, "window.close", false},
		{"organiza lado a lado", []ModelIntent{{Intent: "arrange", Target: "side_by_side"}}, "windows.arrange", false},
		{"cor de destaque azul", []ModelIntent{{Intent: "accent", Target: "blue"}}, "theme.set_tokens", false},
	}
	for _, cs := range cases {
		r := Ground(cs.text, cs.in, c)
		got := ""
		if len(r.Calls) > 0 {
			got = r.Calls[0].Action
			if cs.action == "theme.set_tokens" && got == "theme.switch" && len(r.Calls) > 1 {
				got = r.Calls[1].Action
			}
		}
		if got != cs.action || (len(r.Clarify) > 0) != cs.clarify {
			t.Errorf("%q: got %q clarify %v (%+v)", cs.text, got, r.Clarify, r)
		}
	}
	if r := Ground("why did nginx fail", []ModelIntent{{Intent: "system"}}, c); !r.AskSystem || len(r.Calls) > 0 {
		t.Errorf("system: %+v", r)
	}
}

func TestTranslateDirectAndHelper(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "llm.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	answer := `{"intents":[{"intent":"light_mode","target":"","number":0},{"intent":"accent","target":"green","number":0}]}`
	reply := func() []byte {
		b, _ := json.Marshal(map[string]any{"model": "basalt-translator-0.6b-q8_0", "choices": []any{map[string]any{"message": map[string]any{"content": answer}}}})
		return b
	}
	var got map[string]any
	go http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_ = json.NewDecoder(r.Body).Decode(&got)
		w.Write(reply())
	}))
	m := &Model{Endpoint: "unix:" + sock, Timeout: 5e9}
	r, err := m.Translate(context.Background(), "tema claro com destaque verde", ctxT())
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Calls) != 2 || r.Calls[0].Action != "theme.switch" || r.Model == nil || r.Model.Via != "direct" {
		t.Fatalf("direct: %+v", r)
	}
	if _, ok := got["response_format"]; !ok {
		t.Fatal("no schema constraint in the request")
	}
	// A socket this user may not open: the helper carries the request.
	called := false
	m2 := &Model{Endpoint: "unix:" + filepath.Join(dir, "missing.sock"), Timeout: 5e9,
		Helper: func(ctx context.Context, body []byte) ([]byte, error) { called = true; return reply(), nil }}
	r, err = m2.Translate(context.Background(), "tema claro com destaque verde", ctxT())
	if err != nil || !called || r.Model.Via != "helper" || len(r.Calls) != 2 {
		t.Fatalf("helper: %v %+v", err, r)
	}
}

// A pt-BR person: the system prompt carries the answer-language line, the
// schema and its enums stay English, and only English identifiers in the
// model's structured answer become actions. A model that translates an
// identifier ("escuro", "líquen") gets nothing done.
func TestTranslatePortugueseKeepsEnglishIdentifiers(t *testing.T) {
	dir := t.TempDir()
	sock := filepath.Join(dir, "llm.sock")
	l, err := net.Listen("unix", sock)
	if err != nil {
		t.Fatal(err)
	}
	defer l.Close()
	var answer string
	var got map[string]any
	go http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		got = nil
		_ = json.NewDecoder(r.Body).Decode(&got)
		b, _ := json.Marshal(map[string]any{"model": "qwen3-1.7b-q8_0", "choices": []any{map[string]any{"message": map[string]any{"content": answer}}}})
		w.Write(b)
	}))
	m := &Model{Endpoint: "unix:" + sock, Timeout: 5e9}
	c := ctxT()
	c.Lang = "pt-BR"

	answer = `{"intents":[{"intent":"darker"},{"intent":"use_theme","theme":"lichen"}]}`
	r, err := m.Translate(context.Background(), "deixe mais escuro e use o tema lichen", c)
	if err != nil {
		t.Fatal(err)
	}
	msgs, _ := got["messages"].([]any)
	sys, _ := msgs[0].(map[string]any)["content"].(string)
	if !strings.Contains(sys, "Answer the person in Brazilian Portuguese (pt-BR)") || !strings.Contains(sys, "in English and unchanged") {
		t.Fatalf("no answer-language line in the system prompt:\n%s", sys)
	}
	// The schema's enums are the English identifiers.
	schemaJSON, _ := json.Marshal(got["response_format"])
	for _, id := range []string{`"dark_mode"`, `"use_theme"`, `"lichen"`, `"side_by_side"`} {
		if !strings.Contains(string(schemaJSON), id) {
			t.Errorf("schema lacks %s", id)
		}
	}
	switched, darker := false, false
	for _, k := range r.Calls {
		switched = switched || (k.Action == "theme.switch" && k.Args["theme"] == "lichen")
		darker = darker || k.Action == "theme.set_tokens"
	}
	if !switched || !darker || len(r.Clarify) > 0 {
		t.Errorf("english identifiers: %+v", r)
	}

	// Translated identifiers are not identifiers: nothing is proposed.
	answer = `{"intents":[{"intent":"escuro"},{"intent":"use_theme","theme":"líquen"}]}`
	r, err = m.Translate(context.Background(), "use o tema líquen", c)
	if err != nil {
		t.Fatal(err)
	}
	if len(r.Calls) != 0 || len(r.Clarify) == 0 {
		t.Errorf("translated identifiers were accepted: %+v", r)
	}

	// English: no extra line, the prompt is the reference one.
	c.Lang = "en-US"
	answer = `{"intents":[{"intent":"darker"}]}`
	if _, err := m.Translate(context.Background(), "make it darker", c); err != nil {
		t.Fatal(err)
	}
	msgs, _ = got["messages"].([]any)
	sys, _ = msgs[0].(map[string]any)["content"].(string)
	if strings.Contains(sys, "Answer the person in") {
		t.Errorf("English prompt changed")
	}
}

func TestLanguageRule(t *testing.T) {
	if LanguageRule("en") != "" || LanguageRule("") != "" || LanguageRule("auto") != "" {
		t.Error("English and empty get no rule")
	}
	if r := LanguageRule("es-MX"); !strings.Contains(r, "Mexican Spanish (es-MX)") {
		t.Error(r)
	}
}

// A remote endpoint with a key: the key goes in the Authorization header
// only, and only from a private file.
func TestAPIKeyFile(t *testing.T) {
	dir := t.TempDir()
	key := filepath.Join(dir, "k")
	_ = os.WriteFile(key, []byte("s3cret\n"), 0o644)
	m := &Model{Endpoint: "http://127.0.0.1:1/v1", APIKeyFile: key, Timeout: 5e8}
	if _, _, err := m.post(context.Background(), []byte("{}")); err == nil || !strings.Contains(err.Error(), "private") {
		t.Errorf("a world-readable key was used: %v", err)
	}
	_ = os.Chmod(key, 0o600)
	var auth string
	l, _ := net.Listen("tcp", "127.0.0.1:0")
	defer l.Close()
	go http.Serve(l, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		w.Write([]byte(`{"choices":[{"message":{"content":"{}"}}]}`))
	}))
	m.Endpoint = "http://" + l.Addr().String() + "/v1"
	if _, _, err := m.post(context.Background(), []byte("{}")); err != nil || auth != "Bearer s3cret" {
		t.Errorf("auth %q err %v", auth, err)
	}
}
