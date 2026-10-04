// Package assistant connects the shell to the Basalt OS system assistant
// (the `basalt` command line): system questions typed in the command bar
// run `basalt` read commands, and the assistant's proposals are shown
// with Apply / Ignore. Apply always goes through the assistant's own
// confirmation flow: `basalt apply ID --yes --confirm CODE`, where CODE
// binds the confirmation to the exact commands the person was shown, run
// through pkexec so the person also authenticates. The shell never
// applies anything by itself.
//
// The assistant's state is readable only by root. Read commands go
// through a small helper (assistant-read) that accepts only read-only
// subcommands; a polkit rule lets local administrators run it without a
// password. When the helper is missing, the shell runs `basalt` as the
// user, which shows previews but cannot store proposals.
package assistant

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"regexp"
	"strings"
	"time"
)

// Bridge runs the assistant's command line.
type Bridge struct {
	Basalt string // path of `basalt`
	Helper string // path of the read helper (run through pkexec)
	Pkexec string
}

// Default finds the tools; ok is false when the assistant is not installed.
func Default() (*Bridge, bool) {
	b := &Bridge{Helper: "/usr/libexec/basalt-shell/assistant-read"}
	if p, err := exec.LookPath("basalt"); err == nil {
		b.Basalt = p
	}
	if p, err := exec.LookPath("pkexec"); err == nil {
		b.Pkexec = p
	}
	if v := os.Getenv("BASALT_SHELL_ASSISTANT_HELPER"); v != "" {
		b.Helper = v
	}
	return b, b.Basalt != ""
}

// Available reports whether the assistant can be used.
func (b *Bridge) Available() bool { return b != nil && b.Basalt != "" }

var reWord = regexp.MustCompile(`^[A-Za-z0-9@._:-]{1,100}$`)

// readArgs validates a read-only request (the helper checks again).
func readArgs(args []string) error {
	if len(args) == 0 {
		return errors.New("empty request")
	}
	for _, a := range args {
		if !reWord.MatchString(a) && !strings.HasPrefix(a, "--") {
			return fmt.Errorf("invalid argument %q", a)
		}
	}
	switch args[0] {
	case "status", "disk", "pending", "snapshots", "audit":
	case "why", "show":
		if len(args) < 2 {
			return fmt.Errorf("%s needs an argument", args[0])
		}
	case "fix":
		if len(args) < 2 || args[1] != "selinux" {
			return errors.New("only `fix selinux` is a read request")
		}
	default:
		return fmt.Errorf("%q is not a read-only assistant command", args[0])
	}
	for _, a := range args {
		if a == "--apply" || a == "--yes" || a == "rollback" {
			return errors.New("changes are not run from the command bar")
		}
	}
	return nil
}

func (b *Bridge) run(ctx context.Context, argv []string) (string, error) {
	ctx, cancel := context.WithTimeout(ctx, 90*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var out bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &out
	cmd.Stdin = nil
	err := cmd.Run()
	return out.String(), err
}

// Read runs a read-only command and returns its text output.
func (b *Bridge) Read(ctx context.Context, args []string) (string, error) {
	if !b.Available() {
		return "", errors.New("the system assistant (basalt) is not installed")
	}
	if err := readArgs(args); err != nil {
		return "", err
	}
	if b.Pkexec != "" {
		if st, err := os.Stat(b.Helper); err == nil && !st.IsDir() {
			out, err := b.run(ctx, append([]string{b.Pkexec, b.Helper}, args...))
			if err == nil || !strings.Contains(out, "Not authorized") {
				return out, err
			}
		}
	}
	return b.run(ctx, append([]string{b.Basalt}, args...))
}

// Ask runs `basalt ask TEXT` through the helper (the assistant's own
// translator). It fails when the translator is off.
func (b *Bridge) Ask(ctx context.Context, text string) (string, error) {
	if !b.Available() {
		return "", errors.New("the system assistant (basalt) is not installed")
	}
	if len(text) > 500 || strings.ContainsAny(text, "\x00\n") {
		return "", errors.New("request too long")
	}
	if b.Pkexec != "" {
		if _, err := os.Stat(b.Helper); err == nil {
			return b.run(ctx, []string{b.Pkexec, b.Helper, "ask", text})
		}
	}
	return b.run(ctx, []string{b.Basalt, "ask", text})
}

// Proposal is an assistant proposal as the shell shows it.
type Proposal struct {
	ID       string  `json:"id"`
	Title    string  `json:"title"`
	Kind     string  `json:"kind"`
	Subject  string  `json:"subject"`
	Status   string  `json:"status"`
	Severity float64 `json:"severity"`
	Report   string  `json:"report"` // the assistant's full text, with the exact commands
	Code     string  `json:"code"`   // confirmation code for exactly these commands
	Review   bool    `json:"needs_review"`
}

var (
	reApply = regexp.MustCompile(`basalt apply (p-[0-9a-f]{6})\s.*--confirm ([0-9a-f]{8})`)
)

// FindProposal extracts a proposal id and its confirmation code from a
// rendered report.
func FindProposal(text string) (id, code string) {
	if m := reApply.FindStringSubmatch(text); m != nil {
		return m[1], m[2]
	}
	return "", ""
}

// Pending lists pending proposals with their reports.
func (b *Bridge) Pending(ctx context.Context) ([]Proposal, error) {
	out, err := b.Read(ctx, []string{"pending", "--json"})
	if err != nil {
		return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(out))
	}
	var ps []Proposal
	if err := json.Unmarshal([]byte(out), &ps); err != nil {
		return nil, fmt.Errorf("pending: %v", err)
	}
	return ps, nil
}

// Show returns one proposal with its rendered report and code.
func (b *Bridge) Show(ctx context.Context, id string) (Proposal, error) {
	var p Proposal
	out, err := b.Read(ctx, []string{"show", id, "--json"})
	if err != nil {
		return p, fmt.Errorf("%v: %s", err, strings.TrimSpace(out))
	}
	if err := json.Unmarshal([]byte(out), &p); err != nil {
		return p, err
	}
	text, err := b.Read(ctx, []string{"show", id})
	if err != nil {
		return p, err
	}
	p.Report = text
	_, p.Code = FindProposal(text)
	return p, nil
}

var reID = regexp.MustCompile(`^p-[0-9a-f]{6}$`)
var reCode = regexp.MustCompile(`^[0-9a-f]{8}$`)

// Apply runs the assistant's confirmation flow for a proposal the person
// accepted in the shell: pkexec (the person authenticates), then
// `basalt apply ID --yes --confirm CODE`, which re-checks that the code
// matches the exact commands, takes snapshots, runs, verifies and audits.
func (b *Bridge) Apply(ctx context.Context, id, code string) (string, error) {
	if !reID.MatchString(id) || !reCode.MatchString(code) {
		return "", errors.New("invalid proposal id or confirmation code")
	}
	if b.Pkexec == "" {
		return "", errors.New("pkexec is not installed")
	}
	return b.run(ctx, []string{b.Pkexec, b.Basalt, "apply", id, "--yes", "--confirm", code})
}

// Ignore closes a proposal without running it (recorded by the assistant).
func (b *Bridge) Ignore(ctx context.Context, id string) (string, error) {
	if !reID.MatchString(id) {
		return "", errors.New("invalid proposal id")
	}
	if b.Pkexec == "" {
		return "", errors.New("pkexec is not installed")
	}
	return b.run(ctx, []string{b.Pkexec, b.Basalt, "ignore", id, "--reason", "ignored from the desktop shell"})
}

var reUnderstood = regexp.MustCompile(`(?m)^Understood as: basalt ([a-z0-9@._: -]+)$`)

// Understood extracts the command from `basalt ask --dry-run` output
// ("Understood as: basalt why nginx"). Only read-only commands are
// returned; a change (apply, rollback) is never run from a request in
// natural language.
func Understood(out string) []string {
	m := reUnderstood.FindStringSubmatch(out)
	if m == nil {
		return nil
	}
	args := strings.Fields(m[1])
	if readArgs(args) != nil {
		return nil
	}
	return args
}

// TranslateHelper returns a function that posts a chat completions body
// to the system's local model through the read helper (pkexec), for users
// who may not open the model's socket. Nil when the helper is missing.
func (b *Bridge) TranslateHelper() func(ctx context.Context, body []byte) ([]byte, error) {
	if b == nil || b.Pkexec == "" {
		return nil
	}
	if _, err := os.Stat(b.Helper); err != nil {
		return nil
	}
	return func(ctx context.Context, body []byte) ([]byte, error) {
		if len(body) > 64<<10 {
			return nil, errors.New("translator request too large")
		}
		ctx, cancel := context.WithTimeout(ctx, 60*time.Second)
		defer cancel()
		cmd := exec.CommandContext(ctx, b.Pkexec, b.Helper, "translate")
		cmd.Stdin = bytes.NewReader(body)
		var out, errb bytes.Buffer
		cmd.Stdout, cmd.Stderr = &out, &errb
		if err := cmd.Run(); err != nil {
			return nil, fmt.Errorf("translator helper: %v: %s", err, strings.TrimSpace(errb.String()))
		}
		return out.Bytes(), nil
	}
}
