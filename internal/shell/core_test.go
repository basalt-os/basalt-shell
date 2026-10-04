package shell

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openbasalt/basalt-shell/internal/audit"
	"github.com/openbasalt/basalt-shell/internal/compositor/fake"
	"github.com/openbasalt/basalt-shell/internal/hw"
	"github.com/openbasalt/basalt-shell/internal/theme"
)

func newCore(t *testing.T) (*Core, *fake.Adapter, string) {
	t.Helper()
	dir := t.TempDir()
	themes, _ := filepath.Abs("../../themes")
	st := theme.NewStore([]string{themes}, filepath.Join(dir, "cfg"))
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	if errs := st.LoadErrors(); len(errs) > 0 {
		t.Fatalf("theme errors: %v", errs)
	}
	lg, err := audit.Open(filepath.Join(dir, "audit.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	fk := fake.New()
	c := New(fk, st, lg, hw.Report{}, filepath.Join(dir, "xdg"))
	c.ApplyApps = false
	return c, fk, dir
}

func TestCommandBarDarkerRounder(t *testing.T) {
	c, fk, dir := newCore(t)
	ctx := context.Background()
	before := c.Theme().Tokens
	res := c.Ask(ctx, "make it darker with rounder corners")
	if res.Kind != "proposal" {
		t.Fatalf("kind %s: %s %v", res.Kind, res.Error, res.Unknown)
	}
	pr := res.Proposal
	keys := map[string]theme.Change{}
	for _, ch := range pr.Diff {
		keys[ch.Key] = ch
	}
	for _, k := range []string{"color.bg", "color.surface", "radius.md", "radius.window"} {
		if _, ok := keys[k]; !ok {
			t.Errorf("diff lacks %s: %+v", k, pr.Diff)
		}
	}
	if keys["radius.md"].To.(float64) <= keys["radius.md"].From.(float64) {
		t.Errorf("radius not rounder: %+v", keys["radius.md"])
	}
	// Nothing changed before the confirmation.
	if theme.Diff(before, c.Theme().Tokens) != nil {
		t.Fatal("theme changed before confirmation")
	}
	if _, err := c.Decide(ctx, pr.ID, true, "ui"); err != nil {
		t.Fatal(err)
	}
	after := c.Theme().Tokens
	if after.Num("radius.window") != keys["radius.window"].To.(float64) {
		t.Errorf("radius.window %v, want %v", after.Num("radius.window"), keys["radius.window"].To)
	}
	if fk.Style.CornerRadius != int(after.Num("radius.window")) {
		t.Errorf("compositor style not applied: %d", fk.Style.CornerRadius)
	}
	if theme.Luminance(after.Str("color.bg")) >= theme.Luminance(before.Str("color.bg")) {
		t.Errorf("bg not darker: %s -> %s", before.Str("color.bg"), after.Str("color.bg"))
	}
	// Settings persisted and audit chain intact.
	if _, err := os.Stat(filepath.Join(dir, "cfg", "settings.json")); err != nil {
		t.Fatal(err)
	}
	if n, err := audit.Verify(filepath.Join(dir, "audit.jsonl")); err != nil || n < 3 {
		t.Fatalf("audit: %d %v", n, err)
	}
}

func TestStaleProposalRefused(t *testing.T) {
	c, _, _ := newCore(t)
	ctx := context.Background()
	p1, err := c.Propose(ctx, Meta{Origin: "mcp", Actor: "agent:test"}, []Call{{Action: "theme.set_tokens", Args: map[string]any{"tokens": map[string]any{"radius.md": 20.0}}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Execute(ctx, "ui", []Call{{Action: "theme.switch", Args: map[string]any{"mode": "light"}}}); err != nil {
		t.Fatal(err)
	}
	p, err := c.Decide(ctx, p1.ID, true, "ui")
	if err == nil || p.Status != StatusStale {
		t.Fatalf("want stale, got %s %v", p.Status, err)
	}
}

func TestDeclineAndValidation(t *testing.T) {
	c, fk, _ := newCore(t)
	ctx := context.Background()
	if _, err := c.Propose(ctx, Meta{Origin: "mcp"}, []Call{{Action: "shell.exec", Args: map[string]any{"cmd": "rm -rf /"}}}); err == nil {
		t.Fatal("unknown action accepted")
	}
	if _, err := c.Propose(ctx, Meta{Origin: "mcp"}, []Call{{Action: "theme.set_tokens", Args: map[string]any{"tokens": map[string]any{"radius.md": 999.0}}}}); err == nil {
		t.Fatal("out of range accepted")
	}
	if _, err := c.Propose(ctx, Meta{Origin: "mcp"}, []Call{{Action: "window.close", Args: map[string]any{"window": "1", "force": true}}}); err == nil {
		t.Fatal("unknown parameter accepted")
	}
	pr, err := c.Propose(ctx, Meta{Origin: "mcp"}, []Call{{Action: "window.close", Args: map[string]any{"window": "foot"}}})
	if err != nil {
		t.Fatal(err)
	}
	if _, err := c.Decide(ctx, pr.ID, false, "ui"); err != nil {
		t.Fatal(err)
	}
	for _, call := range fk.Calls {
		if strings.HasPrefix(call, "close") {
			t.Fatal("declined close ran")
		}
	}
}

func TestArrangeGrid(t *testing.T) {
	c, fk, _ := newCore(t)
	ctx := context.Background()
	if _, err := c.Execute(ctx, "ui", []Call{{Action: "windows.arrange", Args: map[string]any{"layout": "columns"}}}); err != nil {
		t.Fatal(err)
	}
	ws, _ := fk.Windows(ctx)
	if ws[0].Rect.X >= ws[1].Rect.X || ws[0].Rect.W != ws[1].Rect.W {
		t.Fatalf("not side by side: %+v", ws)
	}
}

func TestIPCRolesAndWait(t *testing.T) {
	c, _, dir := newCore(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	sock := filepath.Join(dir, "s.sock")
	srv := &Server{Core: c, Path: sock, UI: UICheck{Mode: UICheckExe, Executables: []string{"no-such-ui"}}}
	go srv.Listen(ctx)
	time.Sleep(100 * time.Millisecond)
	// An agent cannot decide or execute.
	agent := dialT(t, sock, "ui")
	if r := agent.call("hello", map[string]any{"role": "ui"}); r.OK {
		t.Fatal("non-UI peer got the ui role")
	}
	if r := agent.call("execute", map[string]any{"calls": []Call{{Action: "theme.switch", Args: map[string]any{"mode": "light"}}}}); r.OK {
		t.Fatal("agent executed directly")
	}
	r := agent.call("propose", map[string]any{"calls": []Call{{Action: "theme.switch", Args: map[string]any{"mode": "light"}}}})
	if !r.OK {
		t.Fatal(r.Error)
	}
	var p Proposal
	b, _ := json.Marshal(r.Result)
	_ = json.Unmarshal(b, &p)
	if r := agent.call("decide", map[string]any{"id": p.ID, "approve": true}); r.OK {
		t.Fatal("agent confirmed its own proposal")
	}
	if c.Themes.Settings().Mode != "dark" {
		t.Fatal("mode changed without confirmation")
	}
}
