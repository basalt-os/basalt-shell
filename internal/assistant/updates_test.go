package assistant

import (
	"strings"
	"testing"
)

func TestUpdatesAndChannelsArgs(t *testing.T) {
	for _, ok := range [][]string{
		{"updates", "--json"}, {"updates", "check", "--json"}, {"updates", "install", "--security", "--json"}, {"updates", "rollback", "--json"},
		{"channels", "--json"}, {"channels", "enable", "basalt-testing", "--consent", "preview-builds-1", "--json"},
		{"channels", "disable", "basalt-tools", "--json"}, {"channels", "add", "vscode", "--json"},
		{"channels", "add", "copr", "--id", "someone/tool", "--json"},
		{"channels", "add", "custom", "--repo-url", "https://repo.example.org/example.repo", "--json"},
		{"channels", "add", "custom", "--baseurl", "https://repo.example.org/f/$releasever/", "--key-url", "https://repo.example.org/key.asc", "--name", "Example tools", "--json"},
		{"channels", "remove", "rpmfusion-free", "--json"},
	} {
		if err := readArgs(ok); err != nil {
			t.Errorf("%v refused: %v", ok, err)
		}
	}
	for _, bad := range [][]string{
		{"updates", "install", "--apply"}, {"updates", "install", "--json", "--yes"}, {"updates", "upgrade", "--json"},
		{"channels", "enable", "basalt-testing", "--consent", "yes", "--json"},
		{"channels", "enable", "Basalt;rm", "--json"},
		{"channels", "add", "custom", "--repo-url", "http://repo.example.org/x.repo", "--json"},
		{"channels", "add", "custom", "--repo-url", "https://repo.example.org/$(id)", "--json"},
		{"channels", "add", "custom", "--baseurl", "https://a.example/", "--key-url", "https://a.example/k", "--name", "a\nb", "--json"},
		{"channels", "add", "copr", "--id", "../../etc", "--json"},
		{"channels", "add", "vscode"},
	} {
		if err := readArgs(bad); err == nil {
			t.Errorf("%v accepted", bad)
		}
	}
}

func TestChannelRequestArgs(t *testing.T) {
	a, err := ChannelRequest{Op: "enable", Repo: "basalt-nonfree-testing", Consent: TestingConsent}.Args()
	if err != nil || strings.Join(a, " ") != "channels enable basalt-nonfree-testing --consent preview-builds-1 --json" {
		t.Errorf("%v %v", a, err)
	}
	if _, err := (ChannelRequest{Op: "add", Entry: "custom", BaseURL: "http://a.example/", KeyURL: "https://a.example/k", Name: "A"}).Args(); err == nil {
		t.Error("plain http accepted")
	}
	if _, err := (ChannelRequest{Op: "apply"}).Args(); err == nil {
		t.Error("unknown op accepted")
	}
}

func TestUnderstoodKeepsUpdatesReadOnly(t *testing.T) {
	if Understood("Understood as: basalt updates") == nil {
		t.Error("the updates report was not understood")
	}
	for _, s := range []string{"Understood as: basalt updates install --json", "Understood as: basalt updates check --json",
		"Understood as: basalt channels add vscode --json"} {
		if Understood(s) != nil {
			t.Errorf("%q runs from natural language", s)
		}
	}
}
