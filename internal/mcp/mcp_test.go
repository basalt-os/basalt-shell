package mcp

import (
	"bufio"
	"context"
	"encoding/json"
	"io"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/openbasalt/basalt-shell/internal/audit"
	"github.com/openbasalt/basalt-shell/internal/compositor/fake"
	"github.com/openbasalt/basalt-shell/internal/hw"
	"github.com/openbasalt/basalt-shell/internal/shell"
	"github.com/openbasalt/basalt-shell/internal/theme"
)

func TestMCPFlow(t *testing.T) {
	dir := t.TempDir()
	themes, _ := filepath.Abs("../../themes")
	st := theme.NewStore([]string{themes}, filepath.Join(dir, "cfg"))
	if err := st.Load(); err != nil {
		t.Fatal(err)
	}
	lg, _ := audit.Open(filepath.Join(dir, "audit.jsonl"))
	core := shell.New(fake.New(), st, lg, hw.Report{}, filepath.Join(dir, "xdg"))
	core.ApplyApps = false
	sock := filepath.Join(dir, "s.sock")
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	go (&shell.Server{Core: core, Path: sock}).Listen(ctx)
	time.Sleep(100 * time.Millisecond)

	inR, inW := io.Pipe()
	outR, outW := io.Pipe()
	srv := &Server{Socket: sock, WaitSeconds: 5}
	go srv.Run(ctx, inR, outW)
	rd := bufio.NewReader(outR)
	send := func(id int, method string, params any) map[string]any {
		b, _ := json.Marshal(map[string]any{"jsonrpc": "2.0", "id": id, "method": method, "params": params})
		inW.Write(append(b, '\n'))
		line, err := rd.ReadBytes('\n')
		if err != nil {
			t.Fatal(err)
		}
		var m map[string]any
		json.Unmarshal(line, &m)
		return m
	}
	init := send(1, "initialize", map[string]any{"protocolVersion": "2025-06-18", "capabilities": map[string]any{}, "clientInfo": map[string]any{"name": "t", "version": "1"}})
	if init["result"] == nil {
		t.Fatalf("initialize: %v", init)
	}
	list := send(2, "tools/list", map[string]any{})
	tools := list["result"].(map[string]any)["tools"].([]any)
	names := map[string]bool{}
	for _, x := range tools {
		names[x.(map[string]any)["name"].(string)] = true
	}
	for _, n := range []string{"desktop_state", "theme_get", "window_move", "theme_set_tokens", "app_launch", "workspace_switch", "settings_open"} {
		if !names[n] {
			t.Errorf("missing tool %s", n)
		}
	}
	// A write: the UI confirms it while the tool waits.
	go func() {
		for i := 0; i < 50; i++ {
			time.Sleep(50 * time.Millisecond)
			if p := core.Pending(); len(p) > 0 {
				core.Decide(ctx, p[0].ID, true, "ui")
				return
			}
		}
	}()
	res := send(3, "tools/call", map[string]any{"name": "theme_set_tokens", "arguments": map[string]any{"tokens": map[string]any{"radius.md": 18}}})
	text := res["result"].(map[string]any)["content"].([]any)[0].(map[string]any)["text"].(string)
	if !strings.Contains(text, `"status": "applied"`) {
		t.Fatalf("not applied: %s", text)
	}
	if st.Settings().Overrides["radius.md"] != 18.0 {
		t.Fatalf("override not stored: %v", st.Settings().Overrides)
	}
	// Declined: nothing changes.
	go func() {
		for i := 0; i < 50; i++ {
			time.Sleep(50 * time.Millisecond)
			if p := core.Pending(); len(p) > 0 {
				core.Decide(ctx, p[0].ID, false, "ui")
				return
			}
		}
	}()
	res = send(4, "tools/call", map[string]any{"name": "theme_switch", "arguments": map[string]any{"mode": "light"}})
	r := res["result"].(map[string]any)
	if r["isError"] != true || st.Settings().Mode != "dark" {
		t.Fatalf("decline: %v mode %s", r, st.Settings().Mode)
	}
}
