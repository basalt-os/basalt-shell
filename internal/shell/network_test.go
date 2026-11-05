package shell

import (
	"context"
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
)

func TestSplitTerse(t *testing.T) {
	got := splitTerse(`*:Café\: 2.4\\5G:WPA2:72`)
	want := []string{"*", `Café: 2.4\5G`, "WPA2", "72"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("got %q", got)
	}
}

func TestParseWifi(t *testing.T) {
	if d := parseWifiDevice("lo:loopback:unmanaged\nenp1s0:ethernet:connected\nwlp0s20f3:wifi:disconnected\n"); d != "wlp0s20f3" {
		t.Errorf("device %q", d)
	}
	if d := parseWifiDevice("enp1s0:ethernet:connected\n"); d != "" {
		t.Errorf("no wifi: %q", d)
	}
	known := parseKnownWifi("Casa:802-11-wireless\nWired connection 1:802-3-ethernet\n")
	out := strings.Join([]string{
		" :Vizinho:WPA2:40",
		"*:Casa:WPA2 WPA3:58",
		" :Casa:WPA2 WPA3:80",
		" ::WPA2:90",
		" :Cafe:--:65",
		" :Empresa:WPA2 802.1X:70",
	}, "\n")
	l := parseWifiList(out, known)
	names := []string{}
	for _, n := range l {
		names = append(names, n.SSID)
	}
	if !reflect.DeepEqual(names, []string{"Casa", "Empresa", "Cafe", "Vizinho"}) {
		t.Fatalf("order %v", names)
	}
	if !l[0].Active || !l[0].Known || l[0].Signal != 80 || !l[0].Secure {
		t.Errorf("Casa: %+v", l[0])
	}
	if !l[1].Enterprise || l[2].Secure || l[2].Known {
		t.Errorf("Empresa/Cafe: %+v %+v", l[1], l[2])
	}
}

func TestWifiConnectArgs(t *testing.T) {
	cases := []struct {
		known, pw bool
		want      string
	}{
		{true, false, "--wait 45 connection up id Casa"},
		{false, false, "--wait 45 device wifi connect Casa ifname wlan0"},
		{false, true, "--wait 45 --ask device wifi connect Casa ifname wlan0"},
		{true, true, "--wait 45 --ask device wifi connect Casa ifname wlan0"},
	}
	for _, c := range cases {
		if got := strings.Join(wifiConnectArgs("Casa", c.known, c.pw, "wlan0"), " "); got != c.want {
			t.Errorf("%v %v: %q", c.known, c.pw, got)
		}
	}
	for _, s := range []string{"", strings.Repeat("a", 33), "a\nb"} {
		if validSSID(s) {
			t.Errorf("valid %q", s)
		}
	}
}

// fakeNmcli records the calls and answers like a laptop with one network.
type fakeNmcli struct {
	calls  []string
	stdins []string
	fail   string
}

func (f *fakeNmcli) run(ctx context.Context, stdin string, args ...string) (string, error) {
	a := strings.Join(args, " ")
	f.calls = append(f.calls, a)
	f.stdins = append(f.stdins, stdin)
	switch {
	case strings.HasSuffix(a, " device"):
		return "wlan0:wifi:disconnected\n", nil
	case strings.HasSuffix(a, "radio"):
		return "enabled\n", nil
	case strings.Contains(a, "connection show"):
		return "", nil
	case strings.Contains(a, "wifi list"):
		return " :Casa:WPA2:70\n", nil
	case strings.Contains(a, "wifi connect"):
		if f.fail != "" {
			return f.fail, &stringErr{f.fail}
		}
		return "Device 'wlan0' successfully activated.\n", nil
	}
	return "", nil
}

type stringErr struct{ s string }

func (e *stringErr) Error() string { return e.s }

// Joining a network: the password reaches nmcli's standard input only, and
// the activity log has the network's name, never the password.
func TestWifiConnectPassword(t *testing.T) {
	c, _, dir := newCore(t)
	f := &fakeNmcli{}
	old := nmcliRun
	nmcliRun = f.run
	t.Cleanup(func() { nmcliRun = old })
	if err := c.WifiConnect(context.Background(), "Casa", "s3cret-pass"); err != nil {
		t.Fatal(err)
	}
	last := f.calls[len(f.calls)-1]
	if !strings.Contains(last, "--ask device wifi connect Casa") || strings.Contains(last, "s3cret") {
		t.Errorf("argv %q", last)
	}
	if f.stdins[len(f.stdins)-1] != "s3cret-pass\n" {
		t.Errorf("stdin %q", f.stdins[len(f.stdins)-1])
	}
	_ = filepath.Walk(dir, func(p string, info os.FileInfo, err error) error {
		if err == nil && !info.IsDir() {
			if b, _ := os.ReadFile(p); strings.Contains(string(b), "s3cret") {
				t.Errorf("the password is in %s", p)
			}
		}
		return nil
	})
	f.fail = "Error: Connection activation failed: Secrets were required, but not provided."
	err := c.WifiConnect(context.Background(), "Casa", "wrong")
	if err == nil || !strings.Contains(err.Error(), "password was not accepted") {
		t.Errorf("wrong password: %v", err)
	}
	if err := c.WifiConnect(context.Background(), "Casa", "a\nb"); err == nil {
		t.Error("a password with a newline must be refused")
	}
}
