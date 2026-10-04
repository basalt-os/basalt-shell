package shell

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"unsafe"
)

// Peer is what the kernel says about the other end of a connection.
type Peer struct {
	UID     int    `json:"uid"`
	PID     int    `json:"pid"`
	Exe     string `json:"exe,omitempty"`
	Context string `json:"context,omitempty"` // SELinux context (SO_PEERSEC), "" without SELinux
	Type    string `json:"type,omitempty"`    // the context's type (domain)
}

// soPeerSec is SO_PEERSEC on Linux.
const soPeerSec = 31

// peerInfo reads SO_PEERCRED and SO_PEERSEC of a connection. Both are
// taken by the kernel when the client connected, so they cannot be
// changed by the client afterwards.
func peerInfo(c *net.UnixConn) (Peer, error) {
	raw, err := c.SyscallConn()
	if err != nil {
		return Peer{}, err
	}
	var p Peer
	var cerr error
	if err := raw.Control(func(fd uintptr) {
		cred, err := syscall.GetsockoptUcred(int(fd), syscall.SOL_SOCKET, syscall.SO_PEERCRED)
		if err != nil {
			cerr = err
			return
		}
		p.UID, p.PID = int(cred.Uid), int(cred.Pid)
		buf := make([]byte, 256)
		n := uint32(len(buf))
		_, _, e := syscall.Syscall6(syscall.SYS_GETSOCKOPT, fd, syscall.SOL_SOCKET, soPeerSec,
			uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&n)), 0)
		if e == 0 && n > 0 && int(n) <= len(buf) {
			p.Context = string(bytes.TrimRight(buf[:n], "\x00"))
		}
	}); err != nil {
		return Peer{}, err
	}
	if cerr != nil {
		return Peer{}, cerr
	}
	p.Type = contextType(p.Context)
	p.Exe, _ = os.Readlink(fmt.Sprintf("/proc/%d/exe", p.PID))
	p.Exe = exeName(p.Exe)
	return p, nil
}

// exeName normalizes the /proc/PID/exe link of a peer. A program that was
// replaced on disk while it runs (a package update) reads as
// "/usr/bin/quickshell (deleted)": it is still the same program, and its
// SELinux domain (checked first) has not changed.
func exeName(link string) string {
	return strings.TrimSuffix(link, " (deleted)")
}

// contextType returns the type field of user:role:type:level.
func contextType(ctx string) string {
	parts := strings.SplitN(ctx, ":", 4)
	if len(parts) < 3 {
		return ""
	}
	return parts[2]
}

// UI check modes.
const (
	// UICheckSELinux: the ui role needs the peer's SELinux domain to be
	// one of the shell UI domains (basalt_shell_ui_t), entered only by
	// executing the UI launcher. Agents confined in basalt_agent_* domains
	// and any other process of the user cannot take it. This is the
	// product mode.
	UICheckSELinux = "selinux"
	// UICheckExe: the Basalt policy is not loaded (a container, a system
	// without SELinux): the peer's executable name is checked. Weak: any
	// process of the same user could run a program with that name.
	UICheckExe = "exe"
	// UICheckInsecure: any same-user peer may take the ui role
	// (development and unit tests only).
	UICheckInsecure = "insecure"
	// UICheckNone: nobody may take the ui role (SELinux was required but
	// the policy is missing).
	UICheckNone = "none"
)

// UICheck decides which peers may take the ui role.
type UICheck struct {
	Mode        string   `json:"mode"`
	Types       []string `json:"types,omitempty"`       // SELinux domains of the shell UI
	Executables []string `json:"executables,omitempty"` // program names of the shell UI
	Reason      string   `json:"reason,omitempty"`
}

// DefaultUITypes are the SELinux domains allowed to confirm.
var DefaultUITypes = []string{"basalt_shell_ui_t"}

// DefaultUIExecutables are the program names of the shell UI.
var DefaultUIExecutables = []string{"quickshell", "qs"}

// selinuxFS is where selinuxfs is mounted (a variable for tests).
var selinuxFS = "/sys/fs/selinux"

// policyHasType reports whether the loaded SELinux policy knows a file
// type, by asking the kernel to validate a context with it.
func policyHasType(typ string) bool {
	f, err := os.OpenFile(filepath.Join(selinuxFS, "context"), os.O_RDWR, 0)
	if err != nil {
		return false
	}
	defer f.Close()
	_, err = f.Write([]byte("system_u:object_r:" + typ + ":s0\x00"))
	return err == nil
}

// DetectUICheck picks the mode. want is BASALT_SHELL_UI_CHECK: "" or
// "auto" (SELinux when the Basalt policy is loaded, else the executable
// name), "selinux" (required: no ui role without the policy), "exe" or
// "insecure".
func DetectUICheck(want string) UICheck {
	u := UICheck{Types: DefaultUITypes, Executables: DefaultUIExecutables}
	loaded := false
	if _, err := os.Stat(filepath.Join(selinuxFS, "enforce")); err == nil {
		loaded = policyHasType("basalt_shell_ui_exec_t")
	}
	switch want {
	case UICheckInsecure:
		u.Mode, u.Reason = UICheckInsecure, "BASALT_SHELL_UI_CHECK=insecure (development only)"
	case UICheckExe:
		u.Mode, u.Reason = UICheckExe, "BASALT_SHELL_UI_CHECK=exe"
	case UICheckSELinux:
		if loaded {
			u.Mode = UICheckSELinux
		} else {
			u.Mode, u.Reason = UICheckNone, "SELinux check required but the basalt_shell policy is not loaded"
		}
	default:
		if loaded {
			u.Mode = UICheckSELinux
		} else {
			u.Mode, u.Reason = UICheckExe, "the basalt_shell SELinux policy is not loaded: falling back to the program name (weak)"
		}
	}
	return u
}

// Allows reports whether a peer may take the ui role, and why not.
func (u UICheck) Allows(p Peer) error {
	exeOK := false
	base := filepath.Base(p.Exe)
	for _, n := range u.Executables {
		if base == n {
			exeOK = true
		}
	}
	switch u.Mode {
	case UICheckInsecure:
		return nil
	case UICheckExe:
		if exeOK {
			return nil
		}
		return fmt.Errorf("program %q is not the shell UI", base)
	case UICheckSELinux:
		for _, t := range u.Types {
			if p.Type == t {
				if !exeOK {
					return fmt.Errorf("domain %s but program %q", p.Type, base)
				}
				return nil
			}
		}
		return fmt.Errorf("SELinux domain %q is not the shell UI's", p.Type)
	}
	return errors.New("no process may take the ui role: " + u.Reason)
}
