// Package models is the shell's side of the one-click model downloads
// (Basalt OS package basalt-models): what a speech or assistant model
// would cost to download, the consent offer the voice card or the model
// card shows, and the download job after the person chose Download.
//
// The shell never downloads anything itself and never runs as root. After
// the person's consent it runs the privileged request program through
// pkexec (polkit action org.basalt-os.models.download: a person in an
// active local session, no password; org.basalt-os.models.download-admin,
// an administrator's password, when /etc/basalt/models.conf says
// downloads = administrators and the person is not one). That program
// checks the request against the manifests, records who consented in the
// audit ledger and starts the confined system service that downloads and
// verifies the files (pinned URLs, SHA-256). The service writes its
// progress to /run/basalt-models/<job>.state, which this package reads.
//
// A download that fails because the network is down waits and starts
// again by itself when the computer is back online (the person already
// agreed); a checksum mismatch or a refusal stops and says why.
package models

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"os"
	"os/user"
	"path/filepath"
	"regexp"
	"strconv"
	"strings"
	"time"
)

// Kinds of model.
const (
	Voice = "voice" // speech to text (whisper.cpp) and its voice activity detector
	LLM   = "llm"   // the assistant's local language model (basalt-llm)
)

// Default paths of Basalt OS (variables for tests).
var (
	PolicyPath   = "/etc/basalt/models.conf"
	RunDir       = "/run/basalt-models"
	VoiceFetch   = "/usr/bin/basalt-voice-fetch"
	LLMFetch     = "/usr/bin/basalt-llm-fetch"
	Request      = "/usr/libexec/basalt-models/request"
	RequestAdmin = "/usr/libexec/basalt-models/request-admin"
	LLMModelDir  = "/var/lib/basalt-llm/models"
)

// File is one model file of a plan.
type File struct {
	Name    string `json:"name"`
	Size    int64  `json:"size"`
	License string `json:"license"`
	Host    string `json:"host"`
	Present bool   `json:"present"`
	// Unpublished: the manifest knows it but its weights have no release.
	Unpublished bool `json:"unpublished,omitempty"`
}

// Plan is what a download of Target would fetch.
type Plan struct {
	Kind   string `json:"kind"`
	Target string `json:"target"`
	Files  []File `json:"files"`
}

// Missing is the bytes still to download.
func (p Plan) Missing() int64 {
	var n int64
	for _, f := range p.Files {
		if !f.Present {
			n += f.Size
		}
	}
	return n
}

// Ready reports whether every file is there.
func (p Plan) Ready() bool { return len(p.Files) > 0 && p.allPresent() }

func (p Plan) allPresent() bool {
	for _, f := range p.Files {
		if !f.Present {
			return false
		}
	}
	return true
}

// Host is where the files come from (one host, or a list).
func (p Plan) Host() string {
	var hs []string
	for _, f := range p.Files {
		if f.Host != "" && f.Host != "-" && !contains(hs, f.Host) {
			hs = append(hs, f.Host)
		}
	}
	return strings.Join(hs, ", ")
}

// Names of the files that are still missing.
func (p Plan) Names() []string {
	var out []string
	for _, f := range p.Files {
		if !f.Present {
			out = append(out, f.Name)
		}
	}
	return out
}

func contains(s []string, v string) bool {
	for _, x := range s {
		if x == v {
			return true
		}
	}
	return false
}

var reTarget = regexp.MustCompile(`^[a-z0-9][a-z0-9._-]{0,63}$`)

// ValidTarget reports whether a set or model name may be requested (the
// request program checks it again against the manifest).
func ValidTarget(t string) bool { return reTarget.MatchString(t) }

// ParsePlan reads the lines of `basalt-voice-fetch --plan` and
// `basalt-llm-fetch --plan` ("name size license host present|absent").
func ParsePlan(kind, target string, out []byte) (Plan, error) {
	p := Plan{Kind: kind, Target: target}
	sc := bufio.NewScanner(bytes.NewReader(out))
	for sc.Scan() {
		f := strings.Fields(sc.Text())
		if len(f) != 5 {
			continue
		}
		size, err := strconv.ParseInt(f[1], 10, 64)
		if err != nil || size < 0 {
			return Plan{}, fmt.Errorf("bad size in plan line %q", sc.Text())
		}
		lic, unpub := f[2], false
		if strings.HasSuffix(lic, ",unpublished") {
			lic, unpub = strings.TrimSuffix(lic, ",unpublished"), true
		}
		p.Files = append(p.Files, File{Name: f[0], Size: size, License: lic, Host: f[3], Present: f[4] == "present", Unpublished: unpub})
	}
	if len(p.Files) == 0 {
		return Plan{}, errors.New("empty plan")
	}
	return p, nil
}

// ------------------------------------------------------------ policy

// Policy is the administrator's /etc/basalt/models.conf.
type Policy struct {
	Downloads  string `json:"downloads"` // everyone, administrators, nobody
	AdminGroup string `json:"admin_group"`
}

// LoadPolicy reads the policy; a missing file is the default (everyone).
func LoadPolicy(path string) Policy {
	p := Policy{Downloads: "everyone", AdminGroup: "wheel"}
	b, err := os.ReadFile(path)
	if err != nil {
		return p
	}
	for _, l := range strings.Split(string(b), "\n") {
		if i := strings.Index(l, "#"); i >= 0 {
			l = l[:i]
		}
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		k, v = strings.TrimSpace(k), strings.TrimSpace(v)
		switch k {
		case "downloads":
			p.Downloads = v
		case "admin_group":
			if v != "" {
				p.AdminGroup = v
			}
		}
	}
	return p
}

// How the desktop asks for a download (Action).
const (
	AskPerson = "person" // the person's own polkit action, no password
	AskAdmin  = "admin"  // an administrator's password
	AskNone   = ""       // downloads are off: the offer says so
)

// Action decides which polkit action a download goes through, the same
// decision the request program makes (an unknown policy fails closed).
func (p Policy) Action(isAdmin bool) string {
	switch p.Downloads {
	case "everyone":
		return AskPerson
	case "administrators":
		if isAdmin {
			return AskPerson
		}
		return AskAdmin
	}
	return AskNone
}

// InGroup reports whether the current user is in group (by name).
func InGroup(group string) bool {
	u, err := user.Current()
	if err != nil {
		return false
	}
	if u.Uid == "0" {
		return true
	}
	ids, err := u.GroupIds()
	if err != nil {
		return false
	}
	for _, id := range ids {
		if g, err := user.LookupGroupId(id); err == nil && g.Name == group {
			return true
		}
	}
	return false
}

// ------------------------------------------------------------ progress

// Progress is one reading of the service's progress file.
type Progress struct {
	State   string // queued, downloading, verifying, enabling, done, failed
	Model   string
	Index   int
	Count   int
	Bytes   int64
	Total   int64
	Error   string // network, checksum, download, storage, unknown, unpublished
	Message string
	Time    time.Time
}

// ParseProgress reads a progress file (key=value lines).
func ParseProgress(b []byte) Progress {
	var p Progress
	for _, l := range strings.Split(string(b), "\n") {
		k, v, ok := strings.Cut(l, "=")
		if !ok {
			continue
		}
		switch k {
		case "state":
			p.State = v
		case "model":
			p.Model = v
		case "index":
			p.Index, _ = strconv.Atoi(v)
		case "count":
			p.Count, _ = strconv.Atoi(v)
		case "bytes":
			p.Bytes, _ = strconv.ParseInt(v, 10, 64)
		case "total":
			p.Total, _ = strconv.ParseInt(v, 10, 64)
		case "error":
			p.Error = v
		case "message":
			p.Message = v
		case "time":
			if s, err := strconv.ParseInt(v, 10, 64); err == nil {
				p.Time = time.Unix(s, 0)
			}
		}
	}
	return p
}

// ProgressPath is the progress file of a job.
func ProgressPath(id string) string { return filepath.Join(RunDir, id+".state") }

// HasLLMModel reports whether a language model file is downloaded.
func HasLLMModel() bool {
	m, _ := filepath.Glob(filepath.Join(LLMModelDir, "*.gguf"))
	return len(m) > 0
}
