package shell

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"syscall"
)

var reUnitChar = regexp.MustCompile(`[^A-Za-z0-9_.-]`)

// Launch starts an application in its own transient systemd scope
// (app-basalt-<id>-<random>.scope), the way desktop environments do, so
// it is tracked, can be inspected and outlives the shell; without
// systemd it falls back to the compositor's spawn.
func (c *Core) Launch(ctx context.Context, id string, argv []string) error {
	if sr, err := exec.LookPath("systemd-run"); err == nil && os.Getenv("BASALT_SHELL_NO_SCOPE") != "1" {
		b := make([]byte, 4)
		_, _ = rand.Read(b)
		unit := "app-basalt-" + strings.Trim(reUnitChar.ReplaceAllString(id, "_"), "_") + "-" + hex.EncodeToString(b)
		args := append([]string{"--user", "--scope", "--collect", "--quiet", "--unit", unit, "--"}, argv...)
		cmd := exec.Command(sr, args...)
		cmd.Stdin, cmd.Stdout, cmd.Stderr = nil, nil, nil
		cmd.SysProcAttr = &syscall.SysProcAttr{Setsid: true}
		if err := cmd.Start(); err == nil {
			go func() { _ = cmd.Wait() }()
			return nil
		}
	}
	return c.Comp.Spawn(ctx, argv)
}
