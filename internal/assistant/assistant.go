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
	// Admin: the person is an administrator (group wheel), whom polkit
	// lets run the read helper without a password. For anyone else the
	// background refresh of the proposals (Pending) never goes through
	// pkexec, so a person who is not an administrator is not asked for a
	// password at login and every minute after.
	Admin bool
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
	if args[0] == "drivers" {
		return driversArgs(args[1:])
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

// driversArgs validates `basalt drivers` requests (Additional drivers):
// the report, the license, and storing the install or rollback proposal.
// Storing a proposal changes nothing: applying it still goes through
// Apply (pkexec, the person authenticates, the confirmation code).
func driversArgs(rest []string) error {
	bad := fmt.Errorf("not an Additional drivers request: drivers %s", strings.Join(rest, " "))
	switch {
	case len(rest) == 0, len(rest) == 1 && rest[0] == "--json":
		return nil
	case len(rest) == 2 && rest[0] == "license" && rest[1] == "nvidia":
		return nil
	case len(rest) == 2 && rest[0] == "rollback" && rest[1] == "--json":
		return nil
	case len(rest) >= 3 && rest[0] == "install" && rest[1] == "nvidia" && rest[len(rest)-1] == "--json":
		switch len(rest) {
		case 3:
			return nil
		case 4:
			if rest[2] == "display" || rest[2] == "compute" {
				return nil
			}
		}
	}
	return bad
}

// ErrDriversUnsupported: the installed assistant is older than `basalt
// drivers` (before 0.10.0). The Additional drivers page then says that
// driver installation is coming soon instead of showing an error.
var ErrDriversUnsupported = errors.New("the system assistant does not support basalt drivers yet")

// Drivers is `basalt drivers --json`: the graphics hardware, the driver
// that fits, what installing it changes, the NVIDIA license text, the
// Secure Boot state and the driver's state (trial, in use, fallback).
func (b *Bridge) Drivers(ctx context.Context) (json.RawMessage, error) {
	out, err := b.Read(ctx, []string{"drivers", "--json"})
	if err != nil {
		if strings.Contains(out, `unknown command "drivers"`) {
			return nil, ErrDriversUnsupported
		}
		return nil, fmt.Errorf("%v: %s", err, strings.TrimSpace(out))
	}
	var v json.RawMessage
	if err := json.Unmarshal([]byte(out), &v); err != nil {
		return nil, fmt.Errorf("drivers: %v", err)
	}
	return v, nil
}

// DriversPropose stores the driver.install proposal (as root, through the
// read helper) and returns it with its report and confirmation code, for
// the confirmation sheet. variant: "" (the recommendation), display or
// compute.
func (b *Bridge) DriversPropose(ctx context.Context, variant string) (Proposal, error) {
	args := []string{"drivers", "install", "nvidia"}
	if variant != "" {
		args = append(args, variant)
	}
	return b.stored(ctx, append(args, "--json"))
}

// DriversRollback stores the proposal that returns the system to the
// snapshot taken before the NVIDIA driver install.
func (b *Bridge) DriversRollback(ctx context.Context) (Proposal, error) {
	return b.stored(ctx, []string{"drivers", "rollback", "--json"})
}

func (b *Bridge) stored(ctx context.Context, args []string) (Proposal, error) {
	out, err := b.Read(ctx, args)
	if err != nil {
		return Proposal{}, fmt.Errorf("%s", strings.TrimSpace(strings.TrimPrefix(out, "basalt: ")))
	}
	var ref struct {
		ID     string `json:"id"`
		Stored bool   `json:"stored"`
	}
	if err := json.Unmarshal([]byte(out), &ref); err != nil || !reID.MatchString(ref.ID) {
		return Proposal{}, fmt.Errorf("unexpected answer from basalt: %s", strings.TrimSpace(out))
	}
	if !ref.Stored {
		return Proposal{}, errors.New("the proposal could not be stored (the assistant needs root: is the read helper installed?)")
	}
	return b.Show(ctx, ref.ID)
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
	if !b.Admin {
		// The person's own view (no proposals are stored for them); a
		// failure means there is nothing to show, not an error to report.
		out, err := b.run(ctx, []string{b.Basalt, "pending", "--json"})
		var ps []Proposal
		if err != nil || json.Unmarshal([]byte(out), &ps) != nil {
			return []Proposal{}, nil
		}
		return ps, nil
	}
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

// Submitted is a proposal queued in the approval gate.
type Submitted struct {
	Proposal string `json:"proposal"`
	ID       string `json:"id"` // the gate's request id
	Decision string `json:"decision"`
	Class    string `json:"class"`
	Reason   string `json:"reason"`
	By       string `json:"by"`
	Enforced bool   `json:"enforced"`
}

var reGateID = regexp.MustCompile(`^g-[0-9a-f]{12}$`)

// Submit queues a proposal in the approval gate through the read helper
// (`basalt submit ID --json`, as root): queueing changes nothing; the
// person then decides on the shell's sheet, which answers the gate, and
// the gate's executor applies it.
func (b *Bridge) Submit(ctx context.Context, id string) (Submitted, error) {
	var s Submitted
	if !reID.MatchString(id) {
		return s, errors.New("invalid proposal id")
	}
	if b.Pkexec == "" {
		return s, errors.New("pkexec is not installed")
	}
	out, err := b.run(ctx, []string{b.Pkexec, b.Helper, "submit", id})
	if err != nil {
		return s, fmt.Errorf("%v: %s", err, strings.TrimSpace(out))
	}
	if err := json.Unmarshal([]byte(out), &s); err != nil || !reGateID.MatchString(s.ID) {
		return s, fmt.Errorf("unexpected answer from basalt submit: %s", strings.TrimSpace(out))
	}
	return s, nil
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
