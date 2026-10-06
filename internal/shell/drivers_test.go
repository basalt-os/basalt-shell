package shell

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestDriverNotice(t *testing.T) {
	dir := t.TempDir()
	nvidiaStateFile, nvidiaBootFile = filepath.Join(dir, "state"), filepath.Join(dir, "boot")
	write := func(p, s string) {
		if err := os.WriteFile(p, []byte(s), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	if driverNotice() != nil {
		t.Error("no state, no notice")
	}
	write(nvidiaStateFile, "mode=active\n")
	write(nvidiaBootFile, "driver=nvidia\nreason=\n")
	if driverNotice() != nil {
		t.Error("a working driver needs no notice")
	}
	write(nvidiaStateFile, "mode=fallback\nreason=nvidia-smi does not answer\nsnapshot=42\n")
	n := driverNotice()
	if n == nil || n.Urgency != "critical" || !strings.Contains(n.Body, "nvidia-smi does not answer") || !strings.Contains(n.Body, "Additional drivers") {
		t.Errorf("fallback: %+v", n)
	}
	write(nvidiaStateFile, "mode=active\n")
	write(nvidiaBootFile, "driver=nouveau\nreason=no NVIDIA module for the running kernel 6.19.10-300.fc44.x86_64\n")
	if n := driverNotice(); n == nil || !strings.Contains(n.Body, "6.19.10") {
		t.Errorf("nouveau this boot: %+v", n)
	}
	write(nvidiaBootFile, "driver=nvidia\n")
	write(nvidiaStateFile, "mode=active\nheld_kernel=7.2.9-200.fc44.x86_64\n")
	if n := driverNotice(); n == nil || n.Urgency != "low" || !strings.Contains(n.Body, "7.2.9") {
		t.Errorf("held kernel: %+v", n)
	}
}
