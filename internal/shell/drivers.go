package shell

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/basalt-os/basalt-shell/internal/i18n"
	"github.com/basalt-os/basalt-shell/internal/paths"
)

// Files basalt-nvidia keeps (Basalt OS, the NVIDIA driver of the
// basalt-nonfree repository); both are readable by everyone. Variables
// for tests.
var (
	nvidiaStateFile = "/var/lib/basalt-nvidia/state"
	nvidiaBootFile  = "/run/basalt-nvidia/boot"
	bootIDFile      = "/proc/sys/kernel/random/boot_id"
)

// DriverNotice is what the person should hear about the NVIDIA driver at
// the start of a session: it fell back to nouveau (why, and that a
// rollback is one click away on the Additional drivers page), this boot
// used nouveau for another reason, or a kernel waits for its module.
type DriverNotice struct {
	Summary, Body, Urgency string
}

func readKV(path string) map[string]string {
	m := map[string]string{}
	b, err := os.ReadFile(path)
	if err != nil {
		return m
	}
	for _, l := range strings.Split(string(b), "\n") {
		if k, v, ok := strings.Cut(l, "="); ok {
			m[k] = v
		}
	}
	return m
}

// driverNotice reads basalt-nvidia's state; nil when there is nothing to say.
func driverNotice() *DriverNotice {
	st := readKV(nvidiaStateFile)
	boot := readKV(nvidiaBootFile)
	switch {
	case st["mode"] == "fallback":
		return &DriverNotice{Urgency: "critical", Summary: i18n.G("The NVIDIA driver did not start"),
			Body: i18n.G("Reason: %s. The computer uses nouveau now. Open Settings, Additional drivers, to return to the snapshot taken before the install or to try again.", st["reason"])}
	case boot["driver"] == "nouveau" && st["mode"] != "":
		return &DriverNotice{Urgency: "normal", Summary: i18n.G("This start uses nouveau instead of the NVIDIA driver"),
			Body: i18n.G("Reason: %s. More in Settings, Additional drivers.", boot["reason"])}
	case st["held_kernel"] != "":
		return &DriverNotice{Urgency: "low", Summary: i18n.G("A kernel update waits for its NVIDIA module"),
			Body: i18n.G("Kernel %s is installed, but its signed NVIDIA module is not published yet: the computer keeps starting the previous kernel.", st["held_kernel"])}
	}
	return nil
}

// WatchDrivers tells the person once per boot and situation, a little
// after the session starts (the driver check runs after the login screen
// and may finish later), and once more a few minutes later.
func (c *Core) WatchDrivers(ctx context.Context) {
	if _, err := os.Stat(nvidiaStateFile); err != nil {
		return
	}
	for _, wait := range []time.Duration{20 * time.Second, 4 * time.Minute} {
		select {
		case <-ctx.Done():
			return
		case <-time.After(wait):
		}
		n := driverNotice()
		if n == nil {
			continue
		}
		b, _ := os.ReadFile(bootIDFile)
		sum := sha256.Sum256([]byte(strings.TrimSpace(string(b)) + "\n" + n.Summary + "\n" + n.Body))
		key := hex.EncodeToString(sum[:8])
		mark := filepath.Join(paths.StateDir(), "driver-notice")
		if old, _ := os.ReadFile(mark); strings.TrimSpace(string(old)) == key {
			continue
		}
		_ = os.MkdirAll(filepath.Dir(mark), 0o700)
		_ = os.WriteFile(mark, []byte(key+"\n"), 0o600)
		c.Broadcast("notify", map[string]any{"summary": n.Summary, "body": n.Body, "urgency": n.Urgency,
			"app": i18n.G("Additional drivers"), "page": "drivers"})
	}
}
