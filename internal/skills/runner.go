package skills

import (
	"bufio"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"os"
	"os/exec"
	"path"
	"path/filepath"
	"strings"
	"time"
)

// Runner starts one worker per job, each in a network session of its
// own: a systemd scope in a slice (basaltskill.slice/basaltskill-ID.slice)
// that is registered with basalt-resolver before the worker gets its job,
// so every connection it makes is default-deny in the kernel except to
// the names of the session's allowlist (the person's grants).
type Runner struct {
	// Worker, Indexer, Sender and Mover are the installed programs (one
	// binary, four SELinux types: read content, index documents, send one
	// confirmed e-mail, rename files inside a grant).
	Worker, Indexer, Sender, Mover string
	// ResolverSocket is basalt-resolver's control socket.
	ResolverSocket string
	// RequireResolver refuses network jobs (mail, web) when the resolver
	// is not running. Off only in development.
	RequireResolver bool
	// NoScope runs the worker directly (unit tests).
	NoScope bool
	Timeout time.Duration
	// Env is added to the worker's environment.
	Env []string
}

// DefaultRunner uses the installed programs.
func DefaultRunner() *Runner {
	r := &Runner{Worker: "/usr/libexec/basalt-shell/basalt-skill", Indexer: "/usr/libexec/basalt-shell/basalt-skill-index",
		Sender: "/usr/libexec/basalt-shell/basalt-skill-send", Mover: "/usr/libexec/basalt-shell/basalt-skill-files",
		ResolverSocket: "/run/basalt-resolver/control.sock", RequireResolver: true, Timeout: 120 * time.Second}
	if p := os.Getenv("BASALT_SKILL_BIN"); p != "" {
		d := filepath.Dir(p)
		r.Worker, r.Indexer, r.Sender, r.Mover = p, filepath.Join(d, "basalt-skill-index"), filepath.Join(d, "basalt-skill-send"), filepath.Join(d, "basalt-skill-files")
	}
	if os.Getenv("BASALT_SKILL_NO_RESOLVER") == "1" {
		r.RequireResolver = false
	}
	return r
}

// Session is what a job ran in (for the audit log).
type Session struct {
	ID       string   `json:"id"`
	Slice    string   `json:"slice,omitempty"`
	Allow    []string `json:"allow"`
	Resolver bool     `json:"resolver"`
	DNS      string   `json:"dns,omitempty"`
	Domain   string   `json:"domain,omitempty"`
	MS       int64    `json:"ms"`
}

func (r *Runner) resolverUp() bool {
	st, err := os.Stat(r.ResolverSocket)
	return err == nil && st.Mode()&os.ModeSocket != 0
}

type resolverReq struct {
	Op       string   `json:"op"`
	Session  string   `json:"session,omitempty"`
	Profile  string   `json:"profile,omitempty"`
	Mode     string   `json:"mode,omitempty"`
	App      string   `json:"app,omitempty"`
	Cgroup   string   `json:"cgroup,omitempty"`
	Allow    []string `json:"allow,omitempty"`
	Loopback *bool    `json:"loopback,omitempty"`
}

func (r *Runner) resolver(req resolverReq) (string, error) {
	c, err := net.DialTimeout("unix", r.ResolverSocket, 3*time.Second)
	if err != nil {
		return "", fmt.Errorf("basalt-resolver: %w", err)
	}
	defer c.Close()
	_ = c.SetDeadline(time.Now().Add(15 * time.Second))
	b, _ := json.Marshal(req)
	if _, err := c.Write(append(b, '\n')); err != nil {
		return "", err
	}
	var rep struct {
		OK    bool   `json:"ok"`
		Error string `json:"error"`
		DNS   string `json:"dns"`
	}
	if err := json.NewDecoder(c).Decode(&rep); err != nil {
		return "", fmt.Errorf("basalt-resolver: no reply: %w", err)
	}
	if !rep.OK {
		return "", fmt.Errorf("basalt-resolver: %s", rep.Error)
	}
	return rep.DNS, nil
}

func cgroupOf(pid int) (string, error) {
	b, err := os.ReadFile(fmt.Sprintf("/proc/%d/cgroup", pid))
	if err != nil {
		return "", err
	}
	for _, l := range strings.Split(string(b), "\n") {
		if p, ok := strings.CutPrefix(l, "0::"); ok {
			return p, nil
		}
	}
	return "", errors.New("no cgroup v2 entry")
}

// Run runs one job. allow is the session's egress allowlist (empty:
// no network at all); network says whether the job needs one (the
// resolver is then required).
func (r *Runner) Run(ctx context.Context, job any, kind string, allow []string, network bool) (json.RawMessage, Session, error) {
	start := time.Now()
	b := make([]byte, 5)
	_, _ = rand.Read(b)
	hexid := hex.EncodeToString(b)
	sess := Session{ID: "sk-" + hexid, Allow: allow}
	prog := r.Worker
	switch kind {
	case "index":
		prog = r.Indexer
	case "send":
		prog = r.Sender
	case "move":
		prog = r.Mover
	}
	useResolver := r.resolverUp() && !r.NoScope
	if network && !useResolver && r.RequireResolver {
		return nil, sess, errors.New("network isolation (basalt-resolver) is not running: the skill will not go online without it")
	}
	argv := []string{prog}
	if !r.NoScope {
		slice := "basaltskill-" + hexid + ".slice"
		argv = append([]string{"systemd-run", "--user", "--scope", "--quiet", "--collect", "--slice=" + slice,
			"--unit=basaltskill-" + hexid + "-" + kind, "--"}, argv...)
	}
	timeout := r.Timeout
	if timeout == 0 {
		timeout = 120 * time.Second
	}
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	// The pure Go resolver sends DNS to the stub address, which the
	// session's rules redirect to its own resolver port.
	cmd.Env = append(os.Environ(), "GODEBUG=netdns=go")
	cmd.Env = append(cmd.Env, r.Env...)
	in, err := cmd.StdinPipe()
	if err != nil {
		return nil, sess, err
	}
	out, err := cmd.StdoutPipe()
	if err != nil {
		return nil, sess, err
	}
	var stderr strings.Builder
	cmd.Stderr = &capWriter{b: &stderr, n: 4096}
	if err := cmd.Start(); err != nil {
		return nil, sess, err
	}
	defer func() { _ = cmd.Wait() }()
	br := bufio.NewReaderSize(out, 1<<20)
	line, err := br.ReadBytes('\n')
	if err != nil {
		return nil, sess, fmt.Errorf("worker did not start: %v %s", err, strings.TrimSpace(stderr.String()))
	}
	var ready struct {
		Ready bool `json:"ready"`
		PID   int  `json:"pid"`
	}
	if json.Unmarshal(line, &ready) != nil || !ready.Ready {
		return nil, sess, fmt.Errorf("worker: unexpected %q", strings.TrimSpace(string(line)))
	}
	if useResolver {
		cg, err := cgroupOf(ready.PID)
		if err != nil {
			_ = cmd.Process.Kill()
			return nil, sess, err
		}
		slice := path.Dir(cg)
		if !strings.HasSuffix(path.Base(slice), ".slice") || !strings.Contains(slice, "basaltskill-"+hexid) {
			_ = cmd.Process.Kill()
			return nil, sess, fmt.Errorf("worker is in %s, not in its session slice", cg)
		}
		sess.Slice = slice
		lb := false
		dns, err := r.resolver(resolverReq{Op: "register", Session: sess.ID, Profile: "basalt-skill-" + kind, Mode: "skill",
			App: "basalt-shell skill " + kind, Cgroup: slice, Allow: allow, Loopback: &lb})
		if err != nil {
			_ = cmd.Process.Kill()
			return nil, sess, err
		}
		sess.Resolver, sess.DNS = true, dns
		defer func() { _, _ = r.resolver(resolverReq{Op: "end", Session: sess.ID}) }()
	}
	jb, _ := json.Marshal(job)
	if _, err := in.Write(append(jb, '\n')); err != nil {
		return nil, sess, err
	}
	in.Close()
	res, err := readLimited(br, 8<<20)
	sess.MS = time.Since(start).Milliseconds()
	if err != nil {
		return nil, sess, fmt.Errorf("worker: %v %s", err, strings.TrimSpace(stderr.String()))
	}
	var probe struct {
		Domain string `json:"domain"`
	}
	_ = json.Unmarshal(res, &probe)
	sess.Domain = probe.Domain
	return res, sess, nil
}

func readLimited(br *bufio.Reader, n int) ([]byte, error) {
	var buf []byte
	for {
		chunk, err := br.ReadSlice('\n')
		buf = append(buf, chunk...)
		if len(buf) > n {
			return nil, errors.New("result too large")
		}
		if err == nil {
			return buf, nil
		}
		if err != bufio.ErrBufferFull {
			if len(buf) > 0 {
				return buf, nil
			}
			return nil, err
		}
	}
}

type capWriter struct {
	b *strings.Builder
	n int
}

func (c *capWriter) Write(p []byte) (int, error) {
	if c.b.Len() < c.n {
		k := c.n - c.b.Len()
		if k > len(p) {
			k = len(p)
		}
		c.b.Write(p[:k])
	}
	return len(p), nil
}
