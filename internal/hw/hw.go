// Package hw decides whether the machine can animate the shell smoothly.
// Motion "auto" turns animations off when it cannot: no GPU render node
// (software rendering, for example a virtual machine without 3D), a
// render node backed by a software rasterizer, very few CPUs or little
// memory. BASALT_SHELL_WEAK=1 or 0 overrides the probe.
package hw

import (
	"bufio"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

// Report explains the decision.
type Report struct {
	Weak    bool     `json:"weak"`
	Reasons []string `json:"reasons"`
	GPU     string   `json:"gpu"` // driver of the first render node, or "none"
	CPUs    int      `json:"cpus"`
	MemMiB  int      `json:"mem_mib"`

	software bool
}

// Probe inspects /sys and /proc.
func Probe() Report {
	r := Report{CPUs: runtime.NumCPU(), GPU: "none"}
	nodes, _ := filepath.Glob("/dev/dri/renderD*")
	if len(nodes) > 0 {
		base := filepath.Base(nodes[0])
		dev := filepath.Join("/sys/class/drm", base, "device")
		if link, err := os.Readlink(filepath.Join(dev, "driver")); err == nil {
			r.GPU = filepath.Base(link)
		} else {
			r.GPU = "unknown"
		}
		// A virtio GPU: 3D only with the VIRGL feature (bit 0 of the
		// virtio device's feature string), else the guest renders with
		// llvmpipe on a 2D framebuffer.
		if feats, _ := filepath.Glob(filepath.Join(dev, "virtio*", "features")); len(feats) > 0 {
			r.GPU = "virtio_gpu"
			if b, err := os.ReadFile(feats[0]); err == nil && len(b) > 0 && b[0] == '0' {
				r.GPU = "virtio_gpu (2D, no virgl)"
				r.software = true
			}
		}
	}
	r.MemMiB = memMiB()
	switch {
	case r.GPU == "none":
		r.Reasons = append(r.Reasons, "no GPU render node: software rendering")
	case r.software:
		r.Reasons = append(r.Reasons, "GPU without 3D acceleration ("+r.GPU+"): software rendering")
	case r.GPU == "vgem" || r.GPU == "vkms":
		r.Reasons = append(r.Reasons, "virtual display without acceleration ("+r.GPU+")")
	}
	if strings.Contains(strings.ToLower(os.Getenv("LIBGL_ALWAYS_SOFTWARE")), "1") {
		r.Reasons = append(r.Reasons, "LIBGL_ALWAYS_SOFTWARE is set")
	}
	if r.CPUs < 2 {
		r.Reasons = append(r.Reasons, "fewer than 2 CPUs")
	}
	if r.MemMiB > 0 && r.MemMiB < 3000 {
		r.Reasons = append(r.Reasons, "less than 3 GiB of memory")
	}
	r.Weak = len(r.Reasons) > 0
	switch os.Getenv("BASALT_SHELL_WEAK") {
	case "1":
		r.Weak, r.Reasons = true, append(r.Reasons, "BASALT_SHELL_WEAK=1")
	case "0":
		r.Weak, r.Reasons = false, []string{"BASALT_SHELL_WEAK=0"}
	}
	return r
}

func memMiB() int {
	f, err := os.Open("/proc/meminfo")
	if err != nil {
		return 0
	}
	defer f.Close()
	sc := bufio.NewScanner(f)
	for sc.Scan() {
		fs := strings.Fields(sc.Text())
		if len(fs) >= 2 && fs[0] == "MemTotal:" {
			kb, _ := strconv.Atoi(fs[1])
			return kb / 1024
		}
	}
	return 0
}
