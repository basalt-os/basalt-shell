package shell

import (
	"context"
	"encoding/json"
	"net/http"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"sync"
	"time"
)

// The driver channels (basalt-nonfree, basalt-nonfree-testing) are defined
// by basalt-nonfree-release, which can be installed long before anything
// is published in them: turning one on then leaves dnf failing on a 404
// for its repomd.xml. Whether a channel can be turned on therefore also
// depends on its repository being published, which only the repository
// itself can say: the daemon asks for its repodata/repomd.xml and adds
// "published" to the reports the Settings pages read (channels.state,
// drivers.state). A request that fails for any reason counts as not
// published; a channel that is on can always be turned off.

// Variables for tests.
var (
	dnfVarsDir    = "/etc/dnf/vars"
	osReleaseFile = "/etc/os-release"
	probeClient   = &http.Client{Timeout: 6 * time.Second}
)

// Default base URLs of the driver channels, as basalt-nonfree-release
// ships them (its /etc/dnf/vars files win when present).
var nonfreeChannelVars = map[string]struct{ Var, Default string }{
	"basalt-nonfree":         {"basalt_nonfree_url", "https://obpkg.org/basalt-nonfree"},
	"basalt-nonfree-testing": {"basalt_nonfree_testing_url", "https://obpkg.org/basalt-nonfree-testing"},
}

// Results are kept for a while: the page loads the reports each time it
// opens, and the answer changes only when a release is published.
const (
	probeTTLPublished   = 30 * time.Minute
	probeTTLUnpublished = 2 * time.Minute
)

type probeResult struct {
	published bool
	at        time.Time
}

var (
	probeMu    sync.Mutex
	probeCache = map[string]probeResult{}
)

var dnfVarRe = regexp.MustCompile(`\$\{?([A-Za-z_][A-Za-z0-9_]*)\}?`)

// dnfVar reads one dnf variable file ("" when missing).
func dnfVar(name string) string {
	if strings.ContainsAny(name, "/.") {
		return ""
	}
	b, err := os.ReadFile(filepath.Join(dnfVarsDir, name))
	if err != nil {
		return ""
	}
	return strings.TrimSpace(string(b))
}

// releaseVer is $releasever as dnf expands it: /etc/dnf/vars/releasever
// when set, else the major version of os-release's VERSION_ID. Basalt OS
// writes VERSION_ID=44.0 while dnf's $releasever is 44 (from
// system-release(releasever)); the full "44.0" made every probe ask for
// .../44.0/x86_64/ and find nothing (round 11 lab).
func releaseVer() string {
	if v := dnfVar("releasever"); v != "" {
		return v
	}
	v := strings.Trim(strings.TrimSpace(readKV(osReleaseFile)["VERSION_ID"]), `"'`)
	if i := strings.IndexByte(v, '.'); i > 0 {
		v = v[:i]
	}
	return v
}

// baseArch is $basearch for this machine.
func baseArch() string {
	switch runtime.GOARCH {
	case "amd64":
		return "x86_64"
	case "arm64":
		return "aarch64"
	case "ppc64le":
		return "ppc64le"
	case "s390x":
		return "s390x"
	}
	return runtime.GOARCH
}

// expandDnfVars replaces $releasever, $basearch and the variables of
// /etc/dnf/vars in a baseurl; ok is false when one stays unknown.
func expandDnfVars(s string) (string, bool) {
	ok := true
	out := dnfVarRe.ReplaceAllStringFunc(s, func(m string) string {
		name := dnfVarRe.FindStringSubmatch(m)[1]
		var v string
		switch name {
		case "releasever":
			v = releaseVer()
		case "basearch", "arch":
			v = baseArch()
		default:
			v = dnfVar(name)
		}
		if v == "" {
			ok = false
		}
		return v
	})
	return out, ok
}

// channelRepomdURL is the repomd.xml of a driver channel on this machine:
// from its baseurl when the channel is defined (url in the report),
// otherwise from the defaults basalt-nonfree-release would install.
func channelRepomdURL(id, baseurl string) string {
	cv, known := nonfreeChannelVars[id]
	if !known {
		return ""
	}
	// A baseurl may list several URLs; the first one is enough here.
	base := ""
	if f := strings.Fields(baseurl); len(f) > 0 {
		base = f[0]
	}
	if base == "" {
		root := dnfVar(cv.Var)
		if root == "" {
			root = cv.Default
		}
		base = strings.TrimRight(root, "/") + "/$releasever/$basearch/"
	}
	exp, ok := expandDnfVars(base)
	if !ok {
		return ""
	}
	u, err := url.Parse(exp)
	if err != nil || (u.Scheme != "https" && u.Scheme != "http" && u.Scheme != "file") {
		return ""
	}
	return strings.TrimRight(exp, "/") + "/repodata/repomd.xml"
}

// repoPublished reports whether the repomd.xml at u can be fetched now
// (cached; any failure counts as not published).
func repoPublished(ctx context.Context, u string) bool {
	if u == "" {
		return false
	}
	probeMu.Lock()
	r, hit := probeCache[u]
	probeMu.Unlock()
	if hit {
		ttl := probeTTLUnpublished
		if r.published {
			ttl = probeTTLPublished
		}
		if time.Since(r.at) < ttl {
			return r.published
		}
	}
	ok := fetchRepomd(ctx, u)
	probeMu.Lock()
	probeCache[u] = probeResult{published: ok, at: time.Now()}
	probeMu.Unlock()
	return ok
}

func fetchRepomd(ctx context.Context, u string) bool {
	if strings.HasPrefix(u, "file://") {
		p, err := url.Parse(u)
		if err != nil {
			return false
		}
		st, err := os.Stat(p.Path)
		return err == nil && st.Mode().IsRegular() && st.Size() > 0
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false
	}
	req.Header.Set("Cache-Control", "no-cache")
	resp, err := probeClient.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	return resp.StatusCode == http.StatusOK
}

// annotateChannels adds "published" to each driver channel of a
// `basalt channels --json` report. Other channels and an unreadable
// report pass through unchanged.
func annotateChannels(ctx context.Context, raw json.RawMessage) json.RawMessage {
	var rep map[string]any
	if json.Unmarshal(raw, &rep) != nil {
		return raw
	}
	list, ok := rep["channels"].([]any)
	if !ok {
		return raw
	}
	type job struct {
		ch  map[string]any
		url string
	}
	var jobs []job
	for _, it := range list {
		ch, ok := it.(map[string]any)
		if !ok {
			continue
		}
		id, _ := ch["id"].(string)
		if _, nonfree := nonfreeChannelVars[id]; !nonfree {
			continue
		}
		base, _ := ch["url"].(string)
		jobs = append(jobs, job{ch, channelRepomdURL(id, base)})
	}
	if len(jobs) == 0 {
		return raw
	}
	res := make([]bool, len(jobs))
	var wg sync.WaitGroup
	for i, j := range jobs {
		wg.Add(1)
		go func(i int, u string) {
			defer wg.Done()
			res[i] = repoPublished(ctx, u)
		}(i, j.url)
	}
	wg.Wait()
	for i, j := range jobs {
		j.ch["published"] = res[i]
	}
	out, err := json.Marshal(rep)
	if err != nil {
		return raw
	}
	return out
}

// annotateDrivers adds state.nonfree_published (the basalt-nonfree
// repository answers) to a `basalt drivers --json` report: installing
// the driver turns that repository on, so it must be published too.
func annotateDrivers(ctx context.Context, raw json.RawMessage) json.RawMessage {
	var rep map[string]any
	if json.Unmarshal(raw, &rep) != nil {
		return raw
	}
	st, ok := rep["state"].(map[string]any)
	if !ok {
		return raw
	}
	st["nonfree_published"] = repoPublished(ctx, channelRepomdURL("basalt-nonfree", ""))
	out, err := json.Marshal(rep)
	if err != nil {
		return raw
	}
	return out
}
