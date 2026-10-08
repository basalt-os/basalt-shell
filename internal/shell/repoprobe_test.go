package shell

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
)

// withProbeEnv points the probe at a temporary dnf vars directory and
// os-release, with an empty cache.
func withProbeEnv(t *testing.T, vars map[string]string) {
	t.Helper()
	dir := t.TempDir()
	vd := filepath.Join(dir, "vars")
	if err := os.MkdirAll(vd, 0o755); err != nil {
		t.Fatal(err)
	}
	for k, v := range vars {
		if err := os.WriteFile(filepath.Join(vd, k), []byte(v+"\n"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	osr := filepath.Join(dir, "os-release")
	if err := os.WriteFile(osr, []byte("NAME=\"Basalt OS\"\nVERSION_ID=44\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	oldVars, oldOSR := dnfVarsDir, osReleaseFile
	dnfVarsDir, osReleaseFile = vd, osr
	probeMu.Lock()
	probeCache = map[string]probeResult{}
	probeMu.Unlock()
	t.Cleanup(func() { dnfVarsDir, osReleaseFile = oldVars, oldOSR })
}

// repoServer answers repomd.xml only under the given repository paths.
func repoServer(t *testing.T, published ...string) *httptest.Server {
	t.Helper()
	ok := map[string]bool{}
	for _, p := range published {
		ok[p] = true
	}
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if ok[r.URL.Path] {
			_, _ = w.Write([]byte("<repomd/>"))
			return
		}
		http.NotFound(w, r)
	}))
	t.Cleanup(srv.Close)
	return srv
}

func TestChannelRepomdURL(t *testing.T) {
	withProbeEnv(t, map[string]string{"basalt_nonfree_testing_url": "https://example.test/nft"})
	arch := baseArch()
	cases := []struct{ id, baseurl, want string }{
		// Defined: the channel's baseurl, variables expanded.
		{"basalt-nonfree-testing", "$basalt_nonfree_testing_url/$releasever/$basearch/", "https://example.test/nft/44/" + arch + "/repodata/repomd.xml"},
		// Not defined: basalt-nonfree-release's defaults (or the vars file).
		{"basalt-nonfree", "", "https://obpkg.org/basalt-nonfree/44/" + arch + "/repodata/repomd.xml"},
		{"basalt-nonfree-testing", "", "https://example.test/nft/44/" + arch + "/repodata/repomd.xml"},
		// A variable nobody defines: unknown, so not published.
		{"basalt-nonfree", "$nowhere/$releasever/", ""},
		// Not a driver channel.
		{"basalt-testing", "", ""},
	}
	for _, c := range cases {
		if got := channelRepomdURL(c.id, c.baseurl); got != c.want {
			t.Errorf("channelRepomdURL(%q, %q) = %q, want %q", c.id, c.baseurl, got, c.want)
		}
	}
}

// The owner's VM: basalt-nonfree-release installed (both channels defined),
// nothing published yet. Turning basalt-nonfree-testing on must not be
// offered until its repomd.xml answers.
func TestAnnotateChannelsPublished(t *testing.T) {
	srv := repoServer(t, "/nft/44/"+baseArch()+"/repodata/repomd.xml")
	withProbeEnv(t, map[string]string{
		"basalt_nonfree_url":         srv.URL + "/nf",
		"basalt_nonfree_testing_url": srv.URL + "/nft",
	})
	in := `{"channels":[
		{"id":"basalt","defined":true,"enabled":true},
		{"id":"basalt-nonfree","defined":true,"enabled":false,"url":"$basalt_nonfree_url/$releasever/$basearch/"},
		{"id":"basalt-nonfree-testing","defined":true,"enabled":false,"url":"$basalt_nonfree_testing_url/$releasever/$basearch/"}],
		"release_key":"X"}`
	out := annotateChannels(context.Background(), json.RawMessage(in))
	var rep struct {
		Channels []map[string]any `json:"channels"`
		Key      string           `json:"release_key"`
	}
	if err := json.Unmarshal(out, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Key != "X" || len(rep.Channels) != 3 {
		t.Fatalf("report changed: %s", out)
	}
	if _, has := rep.Channels[0]["published"]; has {
		t.Error("basalt got a published field")
	}
	if rep.Channels[1]["published"] != false {
		t.Errorf("basalt-nonfree (404) published = %v, want false", rep.Channels[1]["published"])
	}
	if rep.Channels[2]["published"] != true {
		t.Errorf("basalt-nonfree-testing published = %v, want true", rep.Channels[2]["published"])
	}
}

func TestAnnotateDrivers(t *testing.T) {
	srv := repoServer(t)
	withProbeEnv(t, map[string]string{"basalt_nonfree_url": srv.URL + "/nf"})
	out := annotateDrivers(context.Background(), json.RawMessage(`{"state":{"nonfree_available":true},"gpus":[]}`))
	var rep struct {
		State map[string]any `json:"state"`
	}
	if err := json.Unmarshal(out, &rep); err != nil {
		t.Fatal(err)
	}
	if rep.State["nonfree_published"] != false || rep.State["nonfree_available"] != true {
		t.Errorf("state = %v", rep.State)
	}
	// Not a report: unchanged.
	if got := string(annotateDrivers(context.Background(), json.RawMessage(`{"coming_soon":true}`))); got != `{"coming_soon":true}` {
		t.Errorf("report without state changed: %s", got)
	}
}

func TestRepoPublishedUnreachable(t *testing.T) {
	withProbeEnv(t, nil)
	if repoPublished(context.Background(), "http://127.0.0.1:1/repodata/repomd.xml") {
		t.Error("an unreachable repository counts as published")
	}
	if repoPublished(context.Background(), "") {
		t.Error("an unknown URL counts as published")
	}
}
