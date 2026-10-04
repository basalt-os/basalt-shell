package main

import (
	"bytes"
	"errors"
	"fmt"
	"net"
	netmail "net/mail"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"time"
	"unsafe"

	"github.com/basalt-os/basalt-shell/internal/smtp"
)

// SendJob is one confirmed e-mail (basalt-skill-send).
type SendJob struct {
	Host string `json:"host"`
	Port int    `json:"port"`
	TLS  string `json:"tls"`
	User string `json:"user"`
	Pass string `json:"pass"`
	From string `json:"from"`
	To   string `json:"to"`
	Raw  string `json:"raw"`
}

// MoveJob is a confirmed list of renames inside one granted folder
// (basalt-skill-files).
type MoveJob struct {
	Root   string     `json:"root"`
	Moves  []moveItem `json:"moves"`
	Create []string   `json:"create"`
	Remove []string   `json:"remove"` // empty folders an undone batch created
}

type moveItem struct {
	From string `json:"from"`
	To   string `json:"to"`
}

type moveResult struct {
	From  string `json:"from"`
	To    string `json:"to"`
	OK    bool   `json:"ok"`
	Error string `json:"error,omitempty"`
	Dev   uint64 `json:"dev,omitempty"`
	Ino   uint64 `json:"ino,omitempty"`
}

// send checks the message once more (one plain-text message, its own
// envelope addresses in its headers, no attachment) and sends it.
func send(j SendJob) Result {
	m, err := netmail.ReadMessage(bytes.NewReader([]byte(j.Raw)))
	if err != nil {
		return Result{Error: "the message is not valid: " + err.Error()}
	}
	to, err1 := netmail.ParseAddress(m.Header.Get("To"))
	from, err2 := netmail.ParseAddress(m.Header.Get("From"))
	if err1 != nil || err2 != nil || !strings.EqualFold(to.Address, j.To) || !strings.EqualFold(from.Address, j.From) {
		return Result{Error: "the message's addresses differ from the confirmed ones"}
	}
	for _, h := range []string{"Cc", "Bcc", "Resent-To", "Resent-Cc"} {
		if m.Header.Get(h) != "" {
			return Result{Error: "the message has other recipients"}
		}
	}
	if !strings.HasPrefix(m.Header.Get("Content-Type"), "text/plain") {
		return Result{Error: "only plain-text messages are sent"}
	}
	if j.Port == 0 {
		j.Port = 587
	}
	res, err := smtp.Send(smtp.Options{Addr: net.JoinHostPort(j.Host, strconv.Itoa(j.Port)), TLS: j.TLS, User: j.User, Pass: j.Pass,
		Helo: "basalt", Timeout: 30 * time.Second}, j.From, j.To, []byte(j.Raw))
	if err != nil {
		return Result{Error: err.Error(), SMTP: res.Sent}
	}
	return Result{OK: true, SMTPReply: res.Reply, SMTP: res.Sent}
}

// Linux openat2 and renameat2.
const (
	sysOpenat2      = 437 // the same number on every architecture
	oPath           = 0x200000
	resolveNoXdev   = 0x01
	resolveNoMagic  = 0x02
	resolveNoSymlnk = 0x04
	resolveBeneath  = 0x08
	renameNoReplace = 1
)

type openHow struct {
	Flags   uint64
	Mode    uint64
	Resolve uint64
}

// openBeneath opens a directory below the root's fd, refusing symbolic
// links, magic links and other mounts on the way (the kernel checks every
// component, so a link swapped in after the check cannot redirect it).
func openBeneath(rootFD int, rel string) (int, error) {
	if rel == "" || rel == "." {
		return syscall.Dup(rootFD)
	}
	p, err := syscall.BytePtrFromString(rel)
	if err != nil {
		return -1, err
	}
	how := openHow{Flags: uint64(oPath | syscall.O_DIRECTORY | syscall.O_CLOEXEC), Resolve: resolveBeneath | resolveNoSymlnk | resolveNoMagic | resolveNoXdev}
	fd, _, e := syscall.Syscall6(sysOpenat2, uintptr(rootFD), uintptr(unsafe.Pointer(p)), uintptr(unsafe.Pointer(&how)), unsafe.Sizeof(how), 0, 0)
	if e != 0 {
		return -1, e
	}
	return int(fd), nil
}

func renameNoReplaceAt(oldDir int, oldName string, newDir int, newName string) error {
	o, err := syscall.BytePtrFromString(oldName)
	if err != nil {
		return err
	}
	n, err := syscall.BytePtrFromString(newName)
	if err != nil {
		return err
	}
	_, _, e := syscall.Syscall6(sysRenameat2, uintptr(oldDir), uintptr(unsafe.Pointer(o)), uintptr(newDir), uintptr(unsafe.Pointer(n)), renameNoReplace, 0)
	if e != 0 {
		return e
	}
	return nil
}

// rel returns p relative to root, refusing anything outside or hidden.
func rel(root, p string) (string, error) {
	r, err := filepath.Rel(root, p)
	if err != nil || r == "." || strings.HasPrefix(r, "..") || filepath.Clean(p) != p {
		return "", fmt.Errorf("%s is outside the granted folder", p)
	}
	for _, part := range strings.Split(r, "/") {
		if strings.HasPrefix(part, ".") {
			return "", fmt.Errorf("%s is hidden", p)
		}
	}
	return r, nil
}

// move renames inside the root only: no read, no delete, no replace.
func move(j MoveJob) Result {
	if !filepath.IsAbs(j.Root) || filepath.Clean(j.Root) != j.Root {
		return Result{Error: "bad root"}
	}
	rootFD, err := syscall.Open(j.Root, oPath|syscall.O_DIRECTORY|syscall.O_NOFOLLOW|syscall.O_CLOEXEC, 0)
	if err != nil {
		return Result{Error: "cannot open the granted folder: " + err.Error()}
	}
	defer syscall.Close(rootFD)
	var created []string
	for _, c := range j.Create {
		r, err := rel(j.Root, c)
		if err != nil {
			return Result{Error: err.Error()}
		}
		pfd, err := openBeneath(rootFD, filepath.Dir(r))
		if err != nil {
			return Result{Error: fmt.Sprintf("folder %s: %v", filepath.Dir(c), err)}
		}
		err = syscall.Mkdirat(pfd, filepath.Base(r), 0o755)
		syscall.Close(pfd)
		if err != nil && !errors.Is(err, syscall.EEXIST) {
			return Result{Error: fmt.Sprintf("cannot create %s: %v", c, err)}
		}
		if err == nil {
			created = append(created, c)
		}
	}
	var out []moveResult
	ok := true
	for _, m := range j.Moves {
		mr := moveResult{From: m.From, To: m.To}
		err := func() error {
			rf, err := rel(j.Root, m.From)
			if err != nil {
				return err
			}
			rt, err := rel(j.Root, m.To)
			if err != nil {
				return err
			}
			ofd, err := openBeneath(rootFD, filepath.Dir(rf))
			if err != nil {
				return err
			}
			defer syscall.Close(ofd)
			nfd, err := openBeneath(rootFD, filepath.Dir(rt))
			if err != nil {
				return err
			}
			defer syscall.Close(nfd)
			var st syscall.Stat_t
			if err := fstatat(ofd, filepath.Base(rf), &st); err != nil {
				return err
			}
			if st.Mode&syscall.S_IFMT == syscall.S_IFLNK {
				return errors.New("a link is never moved")
			}
			mr.Dev, mr.Ino = st.Dev, st.Ino
			return renameNoReplaceAt(ofd, filepath.Base(rf), nfd, filepath.Base(rt))
		}()
		if err != nil {
			mr.Error = err.Error()
			ok = false
		} else {
			mr.OK = true
		}
		out = append(out, mr)
		if !ok {
			break // stop at the first failure; what was done can be undone
		}
	}
	// Folders an undone batch had created, if they are empty now.
	if ok {
		for i := len(j.Remove) - 1; i >= 0; i-- {
			r, err := rel(j.Root, j.Remove[i])
			if err != nil {
				continue
			}
			pfd, err := openBeneath(rootFD, filepath.Dir(r))
			if err != nil {
				continue
			}
			_ = unlinkatDir(pfd, filepath.Base(r))
			syscall.Close(pfd)
		}
	}
	res := Result{OK: ok, Moves: out, Created: created}
	if !ok {
		res.Error = out[len(out)-1].Error
	}
	return res
}

// fstatat: lstat of name in the directory dirfd (through /proc/self/fd,
// which resolves the directory by its descriptor; the last component is
// not followed).
func fstatat(dirfd int, name string, st *syscall.Stat_t) error {
	return syscall.Lstat(fmt.Sprintf("/proc/self/fd/%d/%s", dirfd, name), st)
}

// unlinkatDir removes an empty folder (AT_REMOVEDIR fails on a non-empty one).
func unlinkatDir(dirfd int, name string) error {
	p, err := syscall.BytePtrFromString(name)
	if err != nil {
		return err
	}
	_, _, e := syscall.Syscall(syscall.SYS_UNLINKAT, uintptr(dirfd), uintptr(unsafe.Pointer(p)), 0x200)
	if e != 0 {
		return e
	}
	return nil
}
