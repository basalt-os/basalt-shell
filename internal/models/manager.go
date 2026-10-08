package models

import (
	"bufio"
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"
)

// Job states.
const (
	StateAuthorizing = "authorizing" // pkexec: polkit is deciding (or asking for a password)
	StateQueued      = "queued"      // the download service was started
	StateDownloading = "downloading"
	StateVerifying   = "verifying" // checking the SHA-256
	StateEnabling    = "enabling"  // the assistant's model: the model service is being turned on
	StateDone        = "done"
	StateWaiting     = "waiting" // the network is down: starts again by itself when it is back
	StateFailed      = "failed"
	StateCancelled   = "cancelled"
)

// Why a job failed (Job.Error).
const (
	ErrNetwork  = "network"  // the server could not be reached
	ErrChecksum = "checksum" // the file did not match its pinned checksum and was deleted
	ErrRefused  = "refused"  // polkit said no, or the password dialog was closed
	ErrPolicy   = "policy"   // the administrator turned downloads off
	ErrAdmin    = "admin"    // an administrator must approve
	ErrMissing  = "missing"  // the download tools are not installed
	ErrStorage  = "storage"  // the model directory is not writable
	ErrStopped  = "stopped"  // the download service stopped without saying why
	ErrFailed   = "failed"   // anything else (Message says what)
)

// Job is one download the person agreed to.
type Job struct {
	ID       string    `json:"id"` // kind-target, the service's instance
	Kind     string    `json:"kind"`
	Target   string    `json:"target"`
	Purpose  string    `json:"purpose,omitempty"` // push-to-talk, skill, settings
	Lang     string    `json:"lang,omitempty"`    // the speech language a voice model is for
	State    string    `json:"state"`
	Model    string    `json:"model,omitempty"` // the file being downloaded
	Index    int       `json:"index,omitempty"`
	Count    int       `json:"count,omitempty"`
	Bytes    int64     `json:"bytes"`
	Total    int64     `json:"total"`
	Error    string    `json:"error,omitempty"`
	Message  string    `json:"message,omitempty"`
	Attempts int       `json:"attempts"`
	Started  time.Time `json:"started"`
	Updated  time.Time `json:"updated"`
}

// Final reports whether the job ended (a waiting job has not: it starts
// again when the network is back).
func (j Job) Final() bool {
	return j.State == StateDone || j.State == StateFailed || j.State == StateCancelled
}

// Active reports whether the download service is (or should be) working.
func (j Job) Active() bool {
	switch j.State {
	case StateQueued, StateDownloading, StateVerifying, StateEnabling:
		return true
	}
	return false
}

// Percent of the bytes downloaded (0 to 100).
func (j Job) Percent() int {
	if j.Total <= 0 {
		return 0
	}
	p := int(j.Bytes * 100 / j.Total)
	if p > 100 {
		p = 100
	}
	return p
}

// Advance applies one reading of the service's progress file to a job.
// A failure of the network makes the job wait (it starts again when the
// computer is back online); any other failure ends it.
func Advance(j Job, p Progress, now time.Time) Job {
	if j.Final() || j.State == StateWaiting || j.State == StateAuthorizing {
		return j
	}
	switch p.State {
	case StateQueued, StateDownloading, StateVerifying, StateEnabling:
		j.State = p.State
	case StateDone:
		j.State = StateDone
		j.Error, j.Message = "", ""
	case StateFailed:
		if p.Error == ErrNetwork {
			j.State, j.Error = StateWaiting, ErrNetwork
		} else {
			j.State, j.Error = StateFailed, p.Error
			if j.Error == "" {
				j.Error = ErrFailed
			}
		}
		j.Message = p.Message
	default:
		return j
	}
	if p.Model != "" {
		j.Model = p.Model
	}
	j.Index, j.Count = p.Index, p.Count
	if p.Total > 0 {
		j.Total = p.Total
	}
	if p.Bytes > 0 || p.State == StateDownloading {
		j.Bytes = p.Bytes
	}
	if j.State == StateDone && j.Total > 0 {
		j.Bytes = j.Total
	}
	j.Updated = now
	return j
}

// AfterRequest applies the exit status of the request program (through
// pkexec) to a job: 0 started; 126 the person closed the password dialog
// and 127 polkit said no; 10, 11 and 12 the administrator's policy and a
// missing tool (see basalt-models' request).
func AfterRequest(j Job, code int, out string) Job {
	j.Error, j.Message = "", ""
	switch code {
	case 0:
		// The progress may already be further along (read while the
		// request was still finishing).
		if !j.Active() {
			j.State = StateQueued
		}
		return j
	case 126, 127:
		j.Error = ErrRefused
	case 10:
		j.Error = ErrPolicy
	case 11:
		j.Error = ErrAdmin
	case 12:
		j.Error = ErrMissing
	default:
		j.Error = ErrFailed
	}
	j.State = StateFailed
	j.Message = strings.TrimSpace(lastLine(out))
	return j
}

func lastLine(s string) string {
	s = strings.TrimSpace(s)
	if i := strings.LastIndex(s, "\n"); i >= 0 {
		return s[i+1:]
	}
	return s
}

// Offer is a download the desktop asks the person about (Download or
// Not now). Nothing is downloaded before they choose Download.
type Offer struct {
	ID      string    `json:"id"`
	Kind    string    `json:"kind"`
	Target  string    `json:"target"`
	Purpose string    `json:"purpose"`
	Lang    string    `json:"lang,omitempty"`
	Files   []File    `json:"files"`
	Bytes   int64     `json:"bytes"` // what is missing
	MB      int64     `json:"mb"`    // the same, in megabytes (rounded up)
	Host    string    `json:"host"`
	Ask     string    `json:"ask"` // person, admin, or "" (downloads are off)
	Created time.Time `json:"created"`
}

// State is what the UI shows: open offers and the jobs.
type State struct {
	Offers []Offer `json:"offers"`
	Jobs   []Job   `json:"jobs"`
	Policy Policy  `json:"policy"`
	Ask    string  `json:"ask"`
}

// Manager keeps the offers and the jobs of this session.
type Manager struct {
	// Exec runs a program and returns its standard output (and error
	// output) and exit status; replaced in tests.
	Exec func(ctx context.Context, argv []string) ([]byte, int, error)
	// Online reports whether the computer has a network route.
	Online func() bool
	// LoadPolicy reads the administrator's policy.
	LoadPolicy func() Policy
	// IsAdmin reports whether the person is in the administrators' group.
	IsAdmin func(group string) bool
	// ServiceRunning reports whether a job's download service runs.
	ServiceRunning func(id string) bool
	// ReadProgress reads a job's progress file.
	ReadProgress func(id string) (Progress, bool)
	Pkexec       string
	Poll         time.Duration // progress reading interval
	RetryEvery   time.Duration // how often a waiting job checks the network
	Stale        time.Duration // no progress for this long: ask whether the service still runs
	MaxAttempts  int
	// OnChange is called after every change (the daemon broadcasts the
	// state); OnDone when a job ends (done, failed or cancelled).
	OnChange func()
	OnDone   func(Job)
	Now      func() time.Time

	mu     sync.Mutex
	jobs   map[string]*Job
	order  []string
	offers map[string]*Offer
	wake   map[string]chan struct{}
}

// New returns a manager for Basalt OS (pkexec, systemd, the progress files).
func New() *Manager {
	m := &Manager{Exec: run, Online: HasRoute, LoadPolicy: func() Policy { return LoadPolicy(PolicyPath) }, IsAdmin: InGroup,
		ServiceRunning: serviceRunning, ReadProgress: readProgress, Poll: 500 * time.Millisecond, RetryEvery: 15 * time.Second,
		Stale: 2 * time.Minute, MaxAttempts: 40, Now: time.Now}
	if p, err := exec.LookPath("pkexec"); err == nil {
		m.Pkexec = p
	}
	return m
}

func (m *Manager) init() {
	if m.jobs == nil {
		m.jobs, m.offers, m.wake = map[string]*Job{}, map[string]*Offer{}, map[string]chan struct{}{}
	}
	if m.Now == nil {
		m.Now = time.Now
	}
}

func (m *Manager) changed() {
	if m.OnChange != nil {
		m.OnChange()
	}
}

// Available reports whether downloads of kind can be offered here: its
// download tool and the request program are installed.
func (m *Manager) Available(kind string) bool {
	tool := VoiceFetch
	if kind == LLM {
		tool = LLMFetch
	}
	for _, p := range []string{tool, Request} {
		if st, err := os.Stat(p); err != nil || st.IsDir() {
			return false
		}
	}
	return m.Pkexec != ""
}

// Plan asks the download tool what a download of target would fetch
// (sizes, host, what is already there; no root, no network).
func (m *Manager) Plan(ctx context.Context, kind, target string) (Plan, error) {
	if !ValidTarget(target) {
		return Plan{}, fmt.Errorf("bad model name %q", target)
	}
	tool := VoiceFetch
	if kind == LLM {
		tool = LLMFetch
	} else if kind != Voice {
		return Plan{}, fmt.Errorf("unknown kind %q", kind)
	}
	out, code, err := m.Exec(ctx, []string{tool, "--plan", target})
	if err != nil || code != 0 {
		return Plan{}, fmt.Errorf("no %s model %s", kind, target)
	}
	return ParsePlan(kind, target, out)
}

// Ask is how this person's downloads are approved (Policy.Action).
func (m *Manager) Ask() (Policy, string) {
	pol := m.LoadPolicy()
	return pol, pol.Action(m.IsAdmin(pol.AdminGroup))
}

func newID() string {
	b := make([]byte, 6)
	_, _ = rand.Read(b)
	return "o-" + hex.EncodeToString(b)
}

// NewOffer makes an offer for what target still misses; nil when nothing
// is missing. An offer of the same model replaces an older one.
func (m *Manager) NewOffer(ctx context.Context, kind, target, purpose, lang string) (*Offer, error) {
	p, err := m.Plan(ctx, kind, target)
	if err != nil {
		return nil, err
	}
	if p.Ready() {
		return nil, nil
	}
	for _, f := range p.Files {
		if f.Unpublished {
			return nil, fmt.Errorf("%s is not published yet", f.Name)
		}
	}
	_, ask := m.Ask()
	o := &Offer{ID: newID(), Kind: kind, Target: target, Purpose: purpose, Lang: lang, Files: p.Files, Bytes: p.Missing(),
		Host: p.Host(), Ask: ask, Created: m.now()}
	o.MB = (o.Bytes + 999_999) / 1_000_000
	m.mu.Lock()
	m.init()
	for id, old := range m.offers {
		if old.Kind == kind {
			delete(m.offers, id)
		}
	}
	m.offers[o.ID] = o
	m.mu.Unlock()
	m.changed()
	return o, nil
}

func (m *Manager) now() time.Time {
	if m.Now == nil {
		return time.Now()
	}
	return m.Now()
}

// Offer returns an open offer.
func (m *Manager) Offer(id string) (Offer, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	o, ok := m.offers[id]
	if !ok {
		return Offer{}, false
	}
	return *o, true
}

// Dismiss closes an offer (Not now).
func (m *Manager) Dismiss(id string) bool {
	m.mu.Lock()
	m.init()
	_, ok := m.offers[id]
	delete(m.offers, id)
	m.mu.Unlock()
	if ok {
		m.changed()
	}
	return ok
}

// Accept starts the download of an offer (the person chose Download).
func (m *Manager) Accept(id string) (Job, error) {
	m.mu.Lock()
	m.init()
	o, ok := m.offers[id]
	if ok {
		delete(m.offers, id)
	}
	m.mu.Unlock()
	if !ok {
		return Job{}, errors.New("this offer is no longer open")
	}
	m.changed()
	return m.Start(o.Kind, o.Target, o.Purpose, o.Lang, o.Bytes)
}

// Start starts a download the person agreed to (from an offer or from
// Settings). A running job of the same model is returned as it is.
func (m *Manager) Start(kind, target, purpose, lang string, total int64) (Job, error) {
	if kind != Voice && kind != LLM {
		return Job{}, fmt.Errorf("unknown kind %q", kind)
	}
	if !ValidTarget(target) {
		return Job{}, fmt.Errorf("bad model name %q", target)
	}
	if !m.Available(kind) {
		return Job{}, errors.New("model downloads are not installed")
	}
	_, ask := m.Ask()
	if ask == AskNone {
		return Job{}, errors.New("the administrator turned model downloads off on this computer")
	}
	id := kind + "-" + target
	m.mu.Lock()
	m.init()
	if j, ok := m.jobs[id]; ok && !j.Final() {
		cp := *j
		m.mu.Unlock()
		if cp.State == StateWaiting {
			m.wakeUp(id)
		}
		return cp, nil
	}
	j := &Job{ID: id, Kind: kind, Target: target, Purpose: purpose, Lang: lang, State: StateAuthorizing, Total: total,
		Started: m.now(), Updated: m.now()}
	if _, ok := m.jobs[id]; !ok {
		m.order = append(m.order, id)
	}
	m.jobs[id] = j
	wake := make(chan struct{}, 1)
	m.wake[id] = wake
	cp := *j
	m.mu.Unlock()
	m.changed()
	go m.run(id, ask, wake)
	return cp, nil
}

// Job returns a job.
func (m *Manager) Job(id string) (Job, bool) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	j, ok := m.jobs[id]
	if !ok {
		return Job{}, false
	}
	return *j, true
}

func (m *Manager) update(id string, f func(*Job)) Job {
	m.mu.Lock()
	j := m.jobs[id]
	before := *j
	f(j)
	after := *j
	m.mu.Unlock()
	if after != before {
		m.changed()
	}
	return after
}

func (m *Manager) wakeUp(id string) {
	m.mu.Lock()
	ch := m.wake[id]
	m.mu.Unlock()
	if ch != nil {
		select {
		case ch <- struct{}{}:
		default:
		}
	}
}

// Retry starts a waiting job again now, or a failed one again.
func (m *Manager) Retry(id string) (Job, error) {
	j, ok := m.Job(id)
	if !ok {
		return Job{}, errors.New("no such download")
	}
	switch {
	case j.State == StateWaiting:
		m.wakeUp(id)
		return j, nil
	case j.Final():
		return m.Start(j.Kind, j.Target, j.Purpose, j.Lang, j.Total)
	}
	return j, nil
}

// Cancel stops waiting for the network (a download that runs finishes:
// the service is the system's). Final jobs are cleared from the list.
func (m *Manager) Cancel(id string) bool {
	m.mu.Lock()
	m.init()
	j, ok := m.jobs[id]
	if !ok {
		m.mu.Unlock()
		return false
	}
	done := j.Final()
	if done {
		delete(m.jobs, id)
		for i, x := range m.order {
			if x == id {
				m.order = append(m.order[:i], m.order[i+1:]...)
				break
			}
		}
	} else if j.State == StateWaiting || j.State == StateAuthorizing {
		j.State, j.Updated = StateCancelled, m.now()
	}
	m.mu.Unlock()
	if !done {
		m.wakeUp(id)
	}
	m.changed()
	return true
}

func (m *Manager) helper(ask string) string {
	if ask == AskAdmin {
		return RequestAdmin
	}
	return Request
}

// run carries one job: the request through pkexec, then the progress of
// the service, waiting for the network and starting again when needed.
func (m *Manager) run(id, ask string, wake chan struct{}) {
	finish := func() {
		j, _ := m.Job(id)
		if m.OnDone != nil {
			m.OnDone(j)
		}
	}
	for {
		j, _ := m.Job(id)
		if j.State == StateCancelled {
			finish()
			return
		}
		attempt := m.now()
		m.update(id, func(j *Job) { j.State, j.Attempts, j.Updated = StateAuthorizing, j.Attempts+1, attempt })
		// The password dialog (an administrator's approval) may take a
		// while. Once the request is approved it writes a fresh progress
		// file before it records the consent and starts the service, so
		// the card follows that file meanwhile instead of waiting for the
		// request to return (the lab saw about 12 s there).
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
		stop := make(chan struct{})
		watched := make(chan struct{})
		go func() { defer close(watched); m.watchRequest(id, attempt, stop) }()
		out, code, err := m.Exec(ctx, []string{m.Pkexec, m.helper(ask), "download", j.Kind, j.Target})
		close(stop)
		<-watched
		cancel()
		if err != nil && code == 0 {
			code = -1
		}
		j = m.update(id, func(j *Job) { *j = AfterRequest(*j, code, string(out)); j.Updated = m.now() })
		if j.State == StateFailed {
			finish()
			return
		}
		j = m.follow(id)
		switch j.State {
		case StateWaiting:
			if !m.waitOnline(id, wake) {
				finish()
				return
			}
			continue
		default:
			finish()
			return
		}
	}
}

// watchRequest moves a job out of "authorizing" as soon as the approved
// request wrote this attempt's progress file (queued, then the service's
// own states), until stop. Older files (a previous attempt's, a failed
// one) are ignored; failures are read by follow, after the request.
func (m *Manager) watchRequest(id string, attempt time.Time, stop chan struct{}) {
	since := attempt.Truncate(time.Second)
	for {
		select {
		case <-stop:
			return
		case <-time.After(m.Poll):
		}
		p, ok := m.ReadProgress(id)
		if !ok || p.Time.Before(since) {
			continue
		}
		switch p.State {
		case StateQueued, StateDownloading, StateVerifying:
		default:
			continue
		}
		now := m.now()
		m.update(id, func(j *Job) {
			if j.State != StateAuthorizing && !j.Active() {
				return
			}
			if j.State == StateAuthorizing {
				j.State = StateQueued
			}
			*j = Advance(*j, p, now)
		})
	}
}

// follow reads the progress until the job ends or waits for the network.
func (m *Manager) follow(id string) Job {
	last := m.now()
	var lastSeen time.Time
	for {
		time.Sleep(m.Poll)
		j, _ := m.Job(id)
		if j.Final() || j.State == StateWaiting {
			return j
		}
		p, ok := m.ReadProgress(id)
		now := m.now()
		if ok && !p.Time.Equal(lastSeen) {
			lastSeen, last = p.Time, now
		}
		if ok {
			j = m.update(id, func(j *Job) { *j = Advance(*j, p, now) })
			if j.Final() || j.State == StateWaiting {
				return j
			}
		}
		// No progress for a while: is the service still there?
		if now.Sub(last) > m.Stale {
			if m.ServiceRunning(id) {
				last = now
				continue
			}
			// One more reading: it may have ended just now.
			if p, ok := m.ReadProgress(id); ok {
				j = m.update(id, func(j *Job) { *j = Advance(*j, p, now) })
				if j.Final() || j.State == StateWaiting {
					return j
				}
			}
			return m.update(id, func(j *Job) { j.State, j.Error, j.Updated = StateFailed, ErrStopped, now })
		}
	}
}

// waitOnline waits until the computer has a network route again (or the
// person asks to retry now); false when the job was cancelled or tried
// too often.
func (m *Manager) waitOnline(id string, wake chan struct{}) bool {
	j, _ := m.Job(id)
	if m.MaxAttempts > 0 && j.Attempts >= m.MaxAttempts {
		m.update(id, func(j *Job) { j.State, j.Updated = StateFailed, m.now() })
		return false
	}
	// Without a route nothing is tried (attempts are not spent); with one
	// (the server may be what is unreachable) the pause grows with each
	// attempt, up to five minutes. Retry now (the card's button) wakes it.
	pause := m.RetryEvery * time.Duration(j.Attempts)
	if pause > 5*time.Minute {
		pause = 5 * time.Minute
	}
	waited := time.Duration(0)
	for {
		select {
		case <-wake:
			j, _ := m.Job(id)
			return j.State != StateCancelled
		case <-time.After(m.RetryEvery):
			waited += m.RetryEvery
		}
		j, _ := m.Job(id)
		if j.State == StateCancelled {
			return false
		}
		if j.State != StateWaiting {
			return true
		}
		if !m.Online() {
			waited = 0
			continue
		}
		if waited >= pause {
			return true
		}
	}
}

// Remove deletes a downloaded model (through the same polkit action).
func (m *Manager) Remove(ctx context.Context, kind, name string) error {
	if kind != Voice && kind != LLM {
		return fmt.Errorf("unknown kind %q", kind)
	}
	if !ValidTarget(name) {
		return fmt.Errorf("bad model name %q", name)
	}
	if !m.Available(kind) {
		return errors.New("model downloads are not installed")
	}
	_, ask := m.Ask()
	if ask == AskNone {
		return errors.New("the administrator turned model downloads off on this computer")
	}
	ctx, cancel := context.WithTimeout(ctx, 10*time.Minute)
	defer cancel()
	out, code, err := m.Exec(ctx, []string{m.Pkexec, m.helper(ask), "remove", kind, name})
	if code != 0 || err != nil {
		j := AfterRequest(Job{}, code, string(out))
		return &JobError{Code: j.Error, Msg: j.Message}
	}
	m.changed()
	return nil
}

// JobError is a refused request with its code (Job.Error values).
type JobError struct{ Code, Msg string }

func (e *JobError) Error() string {
	if e.Msg != "" {
		return e.Msg
	}
	return e.Code
}

// Snapshot is the state for the UI.
func (m *Manager) Snapshot() State {
	pol, ask := m.Ask()
	m.mu.Lock()
	defer m.mu.Unlock()
	m.init()
	st := State{Policy: pol, Ask: ask, Offers: []Offer{}, Jobs: []Job{}}
	for _, o := range m.offers {
		st.Offers = append(st.Offers, *o)
	}
	sort.Slice(st.Offers, func(i, j int) bool { return st.Offers[i].Created.Before(st.Offers[j].Created) })
	for _, id := range m.order {
		if j, ok := m.jobs[id]; ok {
			st.Jobs = append(st.Jobs, *j)
		}
	}
	return st
}

// Catalog is the Settings page's list: every model of the manifests with
// its size and whether it is downloaded.
type Catalog struct {
	Voice       []File `json:"voice"`
	LLM         []File `json:"llm"`
	Recommended *File  `json:"recommended,omitempty"` // the assistant model a download picks here
}

// List reads the manifests through the download tools.
func (m *Manager) List(ctx context.Context) Catalog {
	var c Catalog
	if st, err := os.Stat(VoiceFetch); err == nil && !st.IsDir() {
		if out, code, err := m.Exec(ctx, []string{VoiceFetch, "--list", "--porcelain"}); err == nil && code == 0 {
			if p, err := ParsePlan(Voice, "", out); err == nil {
				c.Voice = p.Files
			}
		}
	}
	if st, err := os.Stat(LLMFetch); err == nil && !st.IsDir() {
		if out, code, err := m.Exec(ctx, []string{LLMFetch, "--list", "--porcelain"}); err == nil && code == 0 {
			if p, err := ParsePlan(LLM, "", out); err == nil {
				for _, f := range p.Files {
					if !f.Unpublished {
						c.LLM = append(c.LLM, f)
					}
				}
			}
		}
		if p, err := m.Plan(ctx, LLM, "recommended"); err == nil && len(p.Files) == 1 {
			c.Recommended = &p.Files[0]
		}
	}
	return c
}

// ------------------------------------------------------------ Basalt OS

func run(ctx context.Context, argv []string) ([]byte, int, error) {
	cmd := exec.CommandContext(ctx, argv[0], argv[1:]...)
	var out, errb bytes.Buffer
	cmd.Stdout, cmd.Stderr = &out, &errb
	err := cmd.Run()
	code := 0
	if ee, ok := err.(*exec.ExitError); ok {
		code, err = ee.ExitCode(), nil
	} else if err != nil {
		code = -1
	}
	if code != 0 && errb.Len() > 0 {
		out.WriteString("\n")
		out.Write(errb.Bytes())
	}
	return out.Bytes(), code, err
}

func readProgress(id string) (Progress, bool) {
	b, err := os.ReadFile(ProgressPath(id))
	if err != nil {
		return Progress{}, false
	}
	return ParseProgress(b), true
}

func serviceRunning(id string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, _, err := run(ctx, []string{"systemctl", "show", "-P", "ActiveState", "basalt-models-fetch@" + id + ".service"})
	if err != nil {
		return false
	}
	switch strings.TrimSpace(string(out)) {
	case "active", "activating", "reloading":
		return true
	}
	return false
}

// HasRoute reports whether the computer has a default route (IPv4 or
// IPv6): enough to try the download again.
func HasRoute() bool {
	if f, err := os.Open("/proc/net/route"); err == nil {
		defer f.Close()
		sc := bufio.NewScanner(f)
		for sc.Scan() {
			fs := strings.Fields(sc.Text())
			// Columns Iface, Destination, Gateway, Flags: destination 0, route up.
			if len(fs) > 3 && fs[1] == "00000000" {
				if fl, err := strconv.ParseUint(fs[3], 16, 32); err == nil && fl&1 == 1 {
					return true
				}
			}
		}
	}
	if b, err := os.ReadFile("/proc/net/ipv6_route"); err == nil {
		for _, l := range strings.Split(string(b), "\n") {
			fs := strings.Fields(l)
			// A default route (::/0) that is not the loopback's.
			if len(fs) == 10 && fs[0] == strings.Repeat("0", 32) && fs[1] == "00" && fs[9] != "lo" {
				return true
			}
		}
	}
	return false
}
