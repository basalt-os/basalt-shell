package assistant

import (
	"context"
	"os"
	"path/filepath"
	"testing"
)

// The checks of an applied proposal show the assistant's localized texts
// (checks_local, checks_output_local) and fall back to the English ones.
func TestCheckLines(t *testing.T) {
	dir := t.TempDir()
	basalt := filepath.Join(dir, "basalt")
	script := `#!/bin/sh
case "$*" in
  "show p-0c0c0c --json") echo '{"id":"p-0c0c0c","status":"applied","result":{"ok":true,"checks":[{"what":"the channel basalt-tools is off","ok":true,"output":"disabled"},{"what":"dnf check","ok":true,"output":"/etc/yum.repos.d"}]},"checks_local":["o canal basalt-tools está desligado"],"checks_output_local":["desativado","/etc/yum.repos.d"]}' ;;
  "show p-0d0d0d --json") echo '{"id":"p-0d0d0d","status":"applied","result":{"ok":false,"checks":[{"what":"the service runs","ok":false,"output":"inactive"}]}}' ;;
  *) echo "" ;;
esac
`
	if err := os.WriteFile(basalt, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	b := &Bridge{Basalt: basalt, Helper: filepath.Join(dir, "missing")}
	p, err := b.Show(context.Background(), "p-0c0c0c")
	if err != nil {
		t.Fatal(err)
	}
	want := []CheckLine{{What: "o canal basalt-tools está desligado", Output: "desativado", OK: true}, {What: "dnf check", Output: "/etc/yum.repos.d", OK: true}}
	if len(p.CheckLines) != 2 || p.CheckLines[0] != want[0] || p.CheckLines[1] != want[1] {
		t.Errorf("localized: %+v", p.CheckLines)
	}
	// An older assistant: no local fields, the English texts.
	p, err = b.Show(context.Background(), "p-0d0d0d")
	if err != nil {
		t.Fatal(err)
	}
	if len(p.CheckLines) != 1 || p.CheckLines[0] != (CheckLine{What: "the service runs", Output: "inactive", OK: false}) {
		t.Errorf("fallback: %+v", p.CheckLines)
	}
}
