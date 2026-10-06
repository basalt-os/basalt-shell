package models

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"
)

func TestParsePlan(t *testing.T) {
	out := []byte("ggml-base.en 147964211 MIT huggingface.co absent\nggml-silero-v5.1.2 885098 MIT huggingface.co present\n\nnoise\n")
	p, err := ParsePlan(Voice, "english", out)
	if err != nil {
		t.Fatal(err)
	}
	if len(p.Files) != 2 || p.Missing() != 147964211 || p.Ready() || p.Host() != "huggingface.co" {
		t.Errorf("plan %+v missing %d", p, p.Missing())
	}
	if n := p.Names(); len(n) != 1 || n[0] != "ggml-base.en" {
		t.Errorf("names %v", n)
	}
	p.Files[0].Present = true
	if !p.Ready() || p.Missing() != 0 {
		t.Error("all present is ready")
	}
	u, err := ParsePlan(LLM, "x", []byte("basalt-translator-0.6b-q8_0 0 Apache-2.0,unpublished - absent\n"))
	if err != nil || !u.Files[0].Unpublished || u.Files[0].License != "Apache-2.0" || u.Host() != "" {
		t.Errorf("unpublished %+v %v", u, err)
	}
	if _, err := ParsePlan(Voice, "x", []byte("")); err == nil {
		t.Error("empty plan accepted")
	}
	if _, err := ParsePlan(Voice, "x", []byte("a -5 MIT h absent\n")); err == nil {
		t.Error("negative size accepted")
	}
	for _, bad := range []string{"", "../x", "a b", "-x", "A", strings.Repeat("a", 65)} {
		if ValidTarget(bad) {
			t.Errorf("target %q accepted", bad)
		}
	}
	for _, good := range []string{"english", "ggml-base.en", "qwen3-1.7b-q8_0", "recommended"} {
		if !ValidTarget(good) {
			t.Errorf("target %q refused", good)
		}
	}
}

// The polkit decision as the desktop makes it: the same table as the
// request program's (basalt-models tests/models-test.sh).
func TestPolicyDecision(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "models.conf")
	if p := LoadPolicy(path); p.Downloads != "everyone" || p.AdminGroup != "wheel" {
		t.Errorf("missing file: %+v", p)
	}
	cases := []struct {
		conf  string
		admin bool
		want  string
	}{
		{"downloads = everyone\n", false, AskPerson},
		{"downloads = everyone\n", true, AskPerson},
		{"downloads = administrators # admins only\n", false, AskAdmin},
		{"downloads = administrators\n", true, AskPerson},
		{"downloads = nobody\n", true, AskNone},
		{"downloads = nobody\n", false, AskNone},
		{"downloads = sometimes\n", true, AskNone}, // unknown values fail closed
		{"# downloads = nobody\n", false, AskPerson},
	}
	for _, c := range cases {
		_ = os.WriteFile(path, []byte(c.conf), 0o644)
		if got := LoadPolicy(path).Action(c.admin); got != c.want {
			t.Errorf("%q admin=%v: %q, want %q", c.conf, c.admin, got, c.want)
		}
	}
	_ = os.WriteFile(path, []byte("admin_group = basalt-admins\n"), 0o644)
	if p := LoadPolicy(path); p.AdminGroup != "basalt-admins" || p.Downloads != "everyone" {
		t.Errorf("group: %+v", p)
	}
}

func TestParseProgress(t *testing.T) {
	p := ParseProgress([]byte("time=1700000000\nstate=downloading\nmodel=ggml-base.en\nindex=1\ncount=2\nbytes=1000\ntotal=4000\n"))
	if p.State != StateDownloading || p.Model != "ggml-base.en" || p.Index != 1 || p.Count != 2 || p.Bytes != 1000 || p.Total != 4000 || p.Time.Unix() != 1700000000 {
		t.Errorf("%+v", p)
	}
	f := ParseProgress([]byte("state=failed\nerror=checksum\nmessage=checksum mismatch, file removed\n"))
	if f.State != StateFailed || f.Error != ErrChecksum || f.Message != "checksum mismatch, file removed" {
		t.Errorf("%+v", f)
	}
}

func TestAdvance(t *testing.T) {
	now := time.Unix(100, 0)
	j := Job{ID: "voice-english", State: StateQueued, Total: 4000}
	steps := []struct {
		p                Progress
		state, err       string
		bytes, total     int64
		percent          int
		final, isWaiting bool
	}{
		{Progress{State: StateDownloading, Bytes: 1000, Total: 4000, Model: "ggml-base.en"}, StateDownloading, "", 1000, 4000, 25, false, false},
		{Progress{State: StateDownloading, Bytes: 3000, Total: 4000}, StateDownloading, "", 3000, 4000, 75, false, false},
		{Progress{State: StateVerifying, Bytes: 4000, Total: 4000}, StateVerifying, "", 4000, 4000, 100, false, false},
		{Progress{State: StateDone, Total: 4000}, StateDone, "", 4000, 4000, 100, true, false},
		// A final job does not move again.
		{Progress{State: StateDownloading, Bytes: 5}, StateDone, "", 4000, 4000, 100, true, false},
	}
	for i, s := range steps {
		j = Advance(j, s.p, now)
		if j.State != s.state || j.Error != s.err || j.Bytes != s.bytes || j.Total != s.total || j.Percent() != s.percent || j.Final() != s.final {
			t.Errorf("step %d: %+v (percent %d)", i, j, j.Percent())
		}
	}
	// The network fails: the job waits (not final), and a later progress
	// reading does not move it until it is started again.
	w := Advance(Job{State: StateDownloading}, Progress{State: StateFailed, Error: ErrNetwork, Message: "curl exit 6"}, now)
	if w.State != StateWaiting || w.Error != ErrNetwork || w.Final() {
		t.Errorf("network: %+v", w)
	}
	if Advance(w, Progress{State: StateDownloading}, now).State != StateWaiting {
		t.Error("a waiting job moved without a new request")
	}
	c := Advance(Job{State: StateVerifying}, Progress{State: StateFailed, Error: ErrChecksum}, now)
	if c.State != StateFailed || c.Error != ErrChecksum || !c.Final() {
		t.Errorf("checksum: %+v", c)
	}
	u := Advance(Job{State: StateDownloading}, Progress{State: StateFailed}, now)
	if u.Error != ErrFailed {
		t.Errorf("failure without a reason: %+v", u)
	}
	// The assistant's model: enabling, then done.
	e := Advance(Job{State: StateVerifying}, Progress{State: StateEnabling}, now)
	if e.State != StateEnabling || e.Final() || !e.Active() {
		t.Errorf("enabling: %+v", e)
	}
	if Advance(Job{State: StateQueued}, Progress{State: "nonsense"}, now).State != StateQueued {
		t.Error("an unknown state moved the job")
	}
}

func TestAfterRequest(t *testing.T) {
	for code, want := range map[int]string{126: ErrRefused, 127: ErrRefused, 10: ErrPolicy, 11: ErrAdmin, 12: ErrMissing, 2: ErrFailed, -1: ErrFailed} {
		j := AfterRequest(Job{State: StateAuthorizing}, code, "basalt-models: something\nbasalt-models: the last line")
		if j.State != StateFailed || j.Error != want || j.Message != "basalt-models: the last line" {
			t.Errorf("code %d: %+v", code, j)
		}
	}
	if j := AfterRequest(Job{State: StateAuthorizing}, 0, "started voice-english"); j.State != StateQueued || j.Error != "" {
		t.Errorf("started: %+v", j)
	}
}

// fakeService plays polkit, the request program and the confined
// download service: each scenario says what the next request answers and
// which progress the service writes.
type fakeService struct {
	mu       sync.Mutex
	t        *testing.T
	progress map[string]Progress
	calls    [][]string
	// next scenario per request: "ok", "network", "checksum", "refused", "stall"
	script  []string
	online  bool
	running map[string]bool
}

func (f *fakeService) exec(ctx context.Context, argv []string) ([]byte, int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls = append(f.calls, argv)
	switch {
	case len(argv) == 3 && argv[0] == VoiceFetch && argv[1] == "--plan":
		if argv[2] == "english" {
			return []byte("ggml-base.en 4000 MIT huggingface.co absent\nggml-silero-v5.1.2 1000 MIT huggingface.co present\n"), 0, nil
		}
		if argv[2] == "ggml-base-q5_1" {
			return []byte("ggml-base-q5_1 9 MIT huggingface.co present\nggml-silero-v5.1.2 1000 MIT huggingface.co present\n"), 0, nil
		}
		return nil, 2, nil
	case len(argv) >= 5 && argv[0] == "pkexec" && argv[2] == "download":
		id := argv[3] + "-" + argv[4]
		sc := "ok"
		if len(f.script) > 0 {
			sc, f.script = f.script[0], f.script[1:]
		}
		switch sc {
		case "refused":
			return []byte("Error executing command as another user: Not authorized"), 127, nil
		case "policy":
			return []byte("basalt-models: the administrator turned model downloads off on this computer"), 10, nil
		}
		f.progress[id] = Progress{State: StateQueued, Total: 4000, Time: time.Now()}
		f.running[id] = true
		go f.service(id, sc)
		return []byte("started " + id), 0, nil
	case len(argv) >= 5 && argv[0] == "pkexec" && argv[2] == "remove":
		return []byte("removed"), 0, nil
	}
	return nil, 1, fmt.Errorf("unexpected %v", argv)
}

// service writes the progress of one download, like basalt-voice-fetch.
func (f *fakeService) service(id, sc string) {
	set := func(p Progress) {
		f.mu.Lock()
		p.Time = time.Now()
		f.progress[id] = p
		f.mu.Unlock()
		time.Sleep(15 * time.Millisecond)
	}
	if sc == "stall" {
		f.mu.Lock()
		f.running[id] = false // died without a word
		f.mu.Unlock()
		return
	}
	for b := int64(0); b <= 4000; b += 1000 {
		set(Progress{State: StateDownloading, Model: "ggml-base.en", Index: 1, Count: 1, Bytes: b, Total: 4000})
		if sc == "network" && b == 2000 {
			set(Progress{State: StateFailed, Error: ErrNetwork, Bytes: b, Total: 4000, Message: "curl exit 6"})
			f.mu.Lock()
			f.running[id] = false
			f.mu.Unlock()
			return
		}
	}
	set(Progress{State: StateVerifying, Bytes: 4000, Total: 4000})
	if sc == "checksum" {
		set(Progress{State: StateFailed, Error: ErrChecksum, Message: "checksum mismatch, file removed"})
	} else {
		set(Progress{State: StateDone, Bytes: 4000, Total: 4000})
	}
	f.mu.Lock()
	f.running[id] = false
	f.mu.Unlock()
}

func newTestManager(t *testing.T, policy string, script ...string) (*Manager, *fakeService, chan Job, *[]Job) {
	dir := t.TempDir()
	for _, p := range []*string{&VoiceFetch, &LLMFetch, &Request, &RequestAdmin, &PolicyPath} {
		*p = filepath.Join(dir, filepath.Base(*p))
		_ = os.WriteFile(*p, []byte("#!/bin/sh\n"), 0o755)
	}
	_ = os.WriteFile(PolicyPath, []byte("downloads = "+policy+"\n"), 0o644)
	f := &fakeService{t: t, progress: map[string]Progress{}, script: script, running: map[string]bool{}}
	done := make(chan Job, 4)
	var seenMu sync.Mutex
	seen := &[]Job{}
	m := &Manager{Exec: f.exec, Pkexec: "pkexec", Poll: 5 * time.Millisecond, RetryEvery: 10 * time.Millisecond, Stale: 80 * time.Millisecond,
		MaxAttempts: 5, LoadPolicy: func() Policy { return LoadPolicy(PolicyPath) }, IsAdmin: func(string) bool { return false },
		Online:         func() bool { f.mu.Lock(); defer f.mu.Unlock(); return f.online },
		ServiceRunning: func(id string) bool { f.mu.Lock(); defer f.mu.Unlock(); return f.running[id] },
		ReadProgress: func(id string) (Progress, bool) {
			f.mu.Lock()
			defer f.mu.Unlock()
			p, ok := f.progress[id]
			return p, ok
		},
		OnDone: func(j Job) { done <- j }}
	m.OnChange = func() {
		for _, j := range m.Snapshot().Jobs {
			seenMu.Lock()
			*seen = append(*seen, j)
			seenMu.Unlock()
		}
	}
	return m, f, done, seen
}

func wait(t *testing.T, done chan Job) Job {
	t.Helper()
	select {
	case j := <-done:
		return j
	case <-time.After(10 * time.Second):
		t.Fatal("the job did not end")
	}
	return Job{}
}

func TestManagerDownload(t *testing.T) {
	m, f, done, seen := newTestManager(t, "everyone", "ok")
	o, err := m.NewOffer(context.Background(), Voice, "english", "push-to-talk", "en")
	if err != nil || o == nil {
		t.Fatalf("offer %+v %v", o, err)
	}
	if o.Bytes != 4000 || o.MB != 1 || o.Host != "huggingface.co" || o.Ask != AskPerson || len(m.Snapshot().Offers) != 1 {
		t.Errorf("offer %+v", o)
	}
	// Nothing is downloaded before the person accepts.
	for _, c := range f.calls {
		if c[0] == "pkexec" {
			t.Fatal("download before consent")
		}
	}
	if _, err := m.Accept(o.ID); err != nil {
		t.Fatal(err)
	}
	if len(m.Snapshot().Offers) != 0 {
		t.Error("offer stays open after Download")
	}
	j := wait(t, done)
	if j.State != StateDone || j.Bytes != 4000 || j.Attempts != 1 || j.Purpose != "push-to-talk" {
		t.Errorf("job %+v", j)
	}
	var sawProgress bool
	for _, s := range *seen {
		if s.State == StateDownloading && s.Bytes > 0 && s.Bytes < 4000 {
			sawProgress = true
		}
	}
	if !sawProgress {
		t.Error("no progress seen while downloading")
	}
	req := f.calls[len(f.calls)-1]
	if strings.Join(req, " ") != "pkexec "+Request+" download voice english" {
		t.Errorf("request %v", req)
	}
	// Nothing missing: no offer.
	if o, err := m.NewOffer(context.Background(), Voice, "ggml-base-q5_1", "push-to-talk", "pt-BR"); o != nil || err != nil {
		t.Errorf("offer for a present model: %+v %v", o, err)
	}
	if _, err := m.NewOffer(context.Background(), Voice, "nosuch", "settings", ""); err == nil {
		t.Error("offer for an unknown model")
	}
}

func TestManagerChecksum(t *testing.T) {
	m, _, done, _ := newTestManager(t, "everyone", "checksum")
	if _, err := m.Start(Voice, "english", "settings", "", 4000); err != nil {
		t.Fatal(err)
	}
	j := wait(t, done)
	if j.State != StateFailed || j.Error != ErrChecksum {
		t.Errorf("job %+v", j)
	}
	// Retry starts it again (the person chose Retry).
	if _, err := m.Retry(j.ID); err != nil {
		t.Fatal(err)
	}
	if j := wait(t, done); j.State != StateDone || j.Attempts != 1 {
		t.Errorf("retry after a checksum failure: %+v", j)
	}
}

func TestManagerOfflineThenOnline(t *testing.T) {
	m, f, done, seen := newTestManager(t, "everyone", "network", "ok")
	if _, err := m.Start(Voice, "english", "push-to-talk", "en", 4000); err != nil {
		t.Fatal(err)
	}
	// It waits while there is no network,
	deadline := time.Now().Add(5 * time.Second)
	for {
		j, _ := m.Job("voice-english")
		if j.State == StateWaiting {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("not waiting: %+v", j)
		}
		time.Sleep(5 * time.Millisecond)
	}
	time.Sleep(100 * time.Millisecond)
	if j, _ := m.Job("voice-english"); j.State != StateWaiting || j.Attempts != 1 || j.Error != ErrNetwork {
		t.Errorf("offline: %+v", j)
	}
	// and starts again by itself when the network is back.
	f.mu.Lock()
	f.online = true
	f.mu.Unlock()
	j := wait(t, done)
	if j.State != StateDone || j.Attempts != 2 {
		t.Errorf("after the network came back: %+v", j)
	}
	var waited bool
	for _, s := range *seen {
		waited = waited || s.State == StateWaiting
	}
	if !waited {
		t.Error("the waiting state was not shown")
	}
}

func TestManagerRetryNowAndCancel(t *testing.T) {
	m, _, done, _ := newTestManager(t, "everyone", "network", "ok")
	if _, err := m.Start(Voice, "english", "settings", "", 4000); err != nil {
		t.Fatal(err)
	}
	for {
		if j, _ := m.Job("voice-english"); j.State == StateWaiting {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	// Retry now, offline: the person asked, so it is tried.
	if _, err := m.Retry("voice-english"); err != nil {
		t.Fatal(err)
	}
	if j := wait(t, done); j.State != StateDone || j.Attempts != 2 {
		t.Errorf("retry now: %+v", j)
	}
	// A waiting job can be cancelled.
	m2, _, done2, _ := newTestManager(t, "everyone", "network")
	_, _ = m2.Start(Voice, "english", "settings", "", 4000)
	for {
		if j, _ := m2.Job("voice-english"); j.State == StateWaiting {
			break
		}
		time.Sleep(5 * time.Millisecond)
	}
	m2.Cancel("voice-english")
	if j := wait(t, done2); j.State != StateCancelled {
		t.Errorf("cancel: %+v", j)
	}
	m2.Cancel("voice-english")
	if len(m2.Snapshot().Jobs) != 0 {
		t.Error("a final job is cleared by a second cancel")
	}
}

func TestManagerRefusals(t *testing.T) {
	m, _, done, _ := newTestManager(t, "everyone", "refused")
	if _, err := m.Start(Voice, "english", "settings", "", 4000); err != nil {
		t.Fatal(err)
	}
	if j := wait(t, done); j.State != StateFailed || j.Error != ErrRefused {
		t.Errorf("polkit said no: %+v", j)
	}
	// Downloads off: no offer can be accepted and no job starts.
	m2, f2, _, _ := newTestManager(t, "nobody")
	o, err := m2.NewOffer(context.Background(), Voice, "english", "push-to-talk", "en")
	if err != nil || o == nil || o.Ask != AskNone {
		t.Fatalf("offer when off: %+v %v", o, err)
	}
	if _, err := m2.Accept(o.ID); err == nil {
		t.Error("download started although downloads are off")
	}
	for _, c := range f2.calls {
		if c[0] == "pkexec" {
			t.Error("pkexec ran although downloads are off")
		}
	}
	// Administrators only, the person is not one: the administrator's action.
	m3, f3, done3, _ := newTestManager(t, "administrators", "ok")
	if _, err := m3.Start(Voice, "english", "settings", "", 4000); err != nil {
		t.Fatal(err)
	}
	wait(t, done3)
	if c := f3.calls[0]; c[1] != RequestAdmin {
		t.Errorf("administrators policy used %v", c)
	}
	if _, err := m3.Start("kernel", "x", "", "", 0); err == nil {
		t.Error("unknown kind started")
	}
	if _, err := m3.Start(Voice, "../x", "", "", 0); err == nil {
		t.Error("bad target started")
	}
}

func TestManagerStalledService(t *testing.T) {
	m, _, done, _ := newTestManager(t, "everyone", "stall")
	if _, err := m.Start(LLM, "recommended", "skill", "", 4000); err != nil {
		t.Fatal(err)
	}
	if j := wait(t, done); j.State != StateFailed || j.Error != ErrStopped {
		t.Errorf("stalled: %+v", j)
	}
}

func TestHasRouteRuns(t *testing.T) {
	_ = HasRoute() // reads /proc; the value depends on the machine
}
