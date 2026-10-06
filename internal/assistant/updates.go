package assistant

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"
)

// Settings, Updates and channels: the assistant's `basalt updates` and
// `basalt channels`. Reading, checking (update.check refreshes the
// package lists, nothing is installed) and storing a proposal go through
// the read helper; applying a proposal is Apply (or the approval gate),
// exactly as for any other proposal. The shell never runs dnf.

var (
	reRepo    = regexp.MustCompile(`^[a-z][a-z0-9._-]{1,62}$`)
	reEntry   = regexp.MustCompile(`^[a-z][a-z0-9-]{1,40}$`)
	reCopr    = regexp.MustCompile(`^@?[A-Za-z0-9][A-Za-z0-9_.-]{0,63}/[A-Za-z0-9][A-Za-z0-9_.+-]{0,99}$`)
	reHTTPS   = regexp.MustCompile(`^https://[A-Za-z0-9.-]{1,253}(:[0-9]{1,5})?(/[A-Za-z0-9._~%/:?&=+,@$-]*)?$`)
	reSrcName = regexp.MustCompile(`^[A-Za-z0-9 ._()+-]{1,80}$`)
)

// TestingConsent is the token a testing channel's repo.enable carries:
// the person read and accepted the preview builds notice.
const TestingConsent = "preview-builds-1"

// updatesArgs validates `basalt updates` requests.
func updatesArgs(rest []string) error {
	switch strings.Join(rest, " ") {
	case "", "--json", "status --json", "check --json", "install --json", "install --security --json", "rollback --json":
		return nil
	}
	return fmt.Errorf("not an Updates request: updates %s", strings.Join(rest, " "))
}

// channelsArgs validates `basalt channels` requests.
func channelsArgs(rest []string) error {
	bad := fmt.Errorf("not a channels request: channels %s", strings.Join(rest, " "))
	if len(rest) == 0 || rest[len(rest)-1] != "--json" {
		if len(rest) == 0 {
			return nil
		}
		return bad
	}
	r := rest[:len(rest)-1]
	switch {
	case len(r) == 0:
		return nil
	case len(r) == 2 && (r[0] == "enable" || r[0] == "disable" || r[0] == "remove") && reRepo.MatchString(r[1]):
		return nil
	case len(r) == 4 && r[0] == "enable" && reRepo.MatchString(r[1]) && r[2] == "--consent" && r[3] == TestingConsent:
		return nil
	case len(r) == 2 && r[0] == "add" && reEntry.MatchString(r[1]) && r[1] != "copr" && r[1] != "custom":
		return nil
	case len(r) == 4 && r[0] == "add" && r[1] == "copr" && r[2] == "--id" && reCopr.MatchString(r[3]):
		return nil
	case len(r) == 4 && r[0] == "add" && r[1] == "custom" && r[2] == "--repo-url" && reHTTPS.MatchString(r[3]):
		return nil
	case len(r) == 8 && r[0] == "add" && r[1] == "custom" && r[2] == "--baseurl" && reHTTPS.MatchString(r[3]) &&
		r[4] == "--key-url" && reHTTPS.MatchString(r[5]) && r[6] == "--name" && reSrcName.MatchString(r[7]):
		return nil
	}
	return bad
}

// ErrUpdatesUnsupported: an assistant older than `basalt updates`.
var ErrUpdatesUnsupported = errors.New("the system assistant does not support basalt updates yet")

func (b *Bridge) readJSON(ctx context.Context, limit time.Duration, args []string) (json.RawMessage, error) {
	out, err := b.readFor(ctx, limit, args)
	if err != nil && !strings.HasPrefix(strings.TrimSpace(out), "{") {
		if strings.Contains(out, `unknown command "`+args[0]+`"`) {
			return nil, ErrUpdatesUnsupported
		}
		return nil, fmt.Errorf("%s", strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "basalt: ")))
	}
	i := strings.Index(out, "{")
	if i < 0 {
		return nil, fmt.Errorf("unexpected answer from basalt: %s", strings.TrimSpace(out))
	}
	var v json.RawMessage
	if jerr := json.Unmarshal([]byte(out[i:]), &v); jerr != nil {
		return nil, fmt.Errorf("%s: %v", args[0], jerr)
	}
	return v, nil
}

// Updates is `basalt updates --json`: the updates the last check found,
// grouped, the restart state, a running update, the history.
func (b *Bridge) Updates(ctx context.Context) (json.RawMessage, error) {
	return b.readJSON(ctx, 2*time.Minute, []string{"updates", "--json"})
}

// UpdatesProgress is `basalt updates status --json`: the running step,
// the restart state and the history, without a dnf query.
func (b *Bridge) UpdatesProgress(ctx context.Context) (json.RawMessage, error) {
	return b.readJSON(ctx, 30*time.Second, []string{"updates", "status", "--json"})
}

// UpdatesCheck is update.check: the package lists are refreshed (as root,
// through the read helper; nothing is installed), then the same report.
func (b *Bridge) UpdatesCheck(ctx context.Context) (json.RawMessage, error) {
	return b.readJSON(ctx, 10*time.Minute, []string{"updates", "check", "--json"})
}

// UpdatesPropose stores the update.install proposal (all updates, or the
// security ones only) for the confirmation step.
func (b *Bridge) UpdatesPropose(ctx context.Context, security bool) (Proposal, error) {
	args := []string{"updates", "install"}
	if security {
		args = append(args, "--security")
	}
	return b.stored(ctx, append(args, "--json"))
}

// UpdatesRollback stores the update.rollback proposal for the last update.
func (b *Bridge) UpdatesRollback(ctx context.Context) (Proposal, error) {
	return b.stored(ctx, []string{"updates", "rollback", "--json"})
}

// Channels is `basalt channels --json`.
func (b *Bridge) Channels(ctx context.Context) (json.RawMessage, error) {
	return b.readJSON(ctx, time.Minute, []string{"channels", "--json"})
}

// ChannelRequest is what the Channels section asks for.
type ChannelRequest struct {
	Op      string `json:"op"` // enable, disable, add, remove
	Repo    string `json:"repo"`
	Consent string `json:"consent"`
	Entry   string `json:"entry"` // a catalog id, "copr" or "custom"
	Project string `json:"project"`
	RepoURL string `json:"repo_url"`
	BaseURL string `json:"baseurl"`
	KeyURL  string `json:"key_url"`
	Name    string `json:"name"`
}

// Args is the request as `basalt channels` arguments (checked again by
// channelsArgs and by the read helper).
func (r ChannelRequest) Args() ([]string, error) {
	var a []string
	switch r.Op {
	case "enable":
		a = []string{"channels", "enable", r.Repo}
		if r.Consent != "" {
			a = append(a, "--consent", r.Consent)
		}
	case "disable", "remove":
		a = []string{"channels", r.Op, r.Repo}
	case "add":
		switch r.Entry {
		case "copr":
			a = []string{"channels", "add", "copr", "--id", r.Project}
		case "custom":
			if r.RepoURL != "" {
				a = []string{"channels", "add", "custom", "--repo-url", r.RepoURL}
			} else {
				a = []string{"channels", "add", "custom", "--baseurl", r.BaseURL, "--key-url", r.KeyURL, "--name", r.Name}
			}
		default:
			a = []string{"channels", "add", r.Entry}
		}
	default:
		return nil, fmt.Errorf("unknown channels operation %q", r.Op)
	}
	a = append(a, "--json")
	if strings.HasPrefix(strings.ToLower(r.RepoURL+r.BaseURL+r.KeyURL), "http://") {
		return nil, errors.New("only https sources can be added")
	}
	if err := channelsArgs(a[1:]); err != nil {
		return nil, err
	}
	return a, nil
}

// ChannelsPropose stores a repo.enable, repo.disable, source.add or
// source.remove proposal for the confirmation step.
func (b *Bridge) ChannelsPropose(ctx context.Context, r ChannelRequest) (Proposal, error) {
	args, err := r.Args()
	if err != nil {
		return Proposal{}, err
	}
	return b.stored(ctx, args)
}
