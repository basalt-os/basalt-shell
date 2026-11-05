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

func TestWifiArgs(t *testing.T) {
	if got := strings.Join(wifiAddArgs("Casa", "wlan0", "wpa-psk"), " "); got != "connection add type wifi ifname wlan0 con-name Casa ssid Casa wifi-sec.key-mgmt wpa-psk" {
		t.Errorf("add %q", got)
	}
	if got := strings.Join(wifiUpArgs("id", "Casa", true), " "); got != "--wait 30 connection up id Casa passwd-file /dev/stdin" {
		t.Errorf("up %q", got)
	}
	for sec, want := range map[string]string{"WPA2": "wpa-psk", "WPA1 WPA2": "wpa-psk", "WPA2 WPA3": "wpa-psk", "WPA3": "sae", "WEP": "", "WPA2 802.1X": ""} {
		if got := keyMgmt(sec); got != want {
			t.Errorf("%s: %q", sec, got)
		}
	}
	for out, want := range map[string]bool{"GENERAL.STATE:70 (connecting (getting IP configuration))\n": true,
		"GENERAL.STATE:100 (connected)": true, "GENERAL.STATE:60 (connecting (need authentication))": false, "GENERAL.STATE:30 (disconnected)": false} {
		if parseAuthenticated(out) != want {
			t.Errorf("%q", out)
		}
	}
	if u := parseAddedUUID("Connection 'Casa' (46d64f85-3bd5-44d3-bd87-200d02399a9a) successfully added.\n"); u != "46d64f85-3bd5-44d3-bd87-200d02399a9a" {
		t.Errorf("uuid %q", u)
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
	case strings.Contains(a, "GENERAL.STATE"):
		return "GENERAL.STATE:30 (disconnected)\n", nil
	case strings.Contains(a, "connection add"):
		return "Connection 'Casa' (0000-1111) successfully added.\n", nil
	case strings.Contains(a, "connection up"):
		if f.fail != "" {
			return f.fail, &stringErr{f.fail}
		}
		return "Connection successfully activated.\n", nil
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
	if last != "--wait 30 connection up uuid 0000-1111 passwd-file /dev/stdin" {
		t.Errorf("argv %q", last)
	}
	for _, a := range f.calls {
		if strings.Contains(a, "s3cret") {
			t.Errorf("the password is on a command line: %q", a)
		}
	}
	if f.stdins[len(f.stdins)-1] != "802-11-wireless-security.psk:s3cret-pass\n" {
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
	// A wrong password: NetworkManager asks again until the wait is
	// over; the new connection is deleted.
	f.fail = "Error: Timeout expired (30 seconds)"
	f.calls = nil
	err := c.WifiConnect(context.Background(), "Casa", "wrong-password")
	if err == nil || !strings.Contains(err.Error(), "password was not accepted") {
		t.Errorf("wrong password: %v", err)
	}
	if last := f.calls[len(f.calls)-1]; last != "connection delete uuid 0000-1111" {
		t.Errorf("not cleaned up: %q", last)
	}
	if err := c.WifiConnect(context.Background(), "Casa", "a\nb"); err == nil {
		t.Error("a password with a newline must be refused")
	}
}

// Passwords nmcli's passwd-file reader would change if written as they
// are: backslashes (escapes), spaces at either end (stripped), a literal
// "\n" or octal-looking text, tabs. Each one reaches NetworkManager
// unchanged after nmcli's unescaping (unescapePasswd follows
// nm_utils_buf_utf8safe_unescape with STRIP_SPACES).
func TestWifiPasswdFileEscaping(t *testing.T) {
	for _, pw := range []string{
		`plain-password`, `back\slash`, `ends\`, `\\double`, ` leading space`, `trailing space `,
		"  both  ", "tab\there", `literal\n and \040`, `octal\101`, "çãé senha", `a:b=c#d`,
	} {
		line := wifiPasswdFile(pw)
		if !strings.HasPrefix(line, "802-11-wireless-security.psk:") || !strings.HasSuffix(line, "\n") || strings.Count(line, "\n") != 1 {
			t.Errorf("%q: line %q", pw, line)
			continue
		}
		v := strings.TrimSuffix(strings.TrimPrefix(line, "802-11-wireless-security.psk:"), "\n")
		if got := unescapePasswd(v); got != pw {
			t.Errorf("%q: nmcli would read %q (line %q)", pw, got, line)
		}
	}
	// The model itself: what nmcli does to an unescaped value.
	if unescapePasswd(` a\qb `) != "aqb" || unescapePasswd(`x\\y`) != `x\y` || unescapePasswd(`\040x\040`) != " x " {
		t.Error("unescape model")
	}
}

func TestValidWifiPassword(t *testing.T) {
	hex := strings.Repeat("0a", 32)
	for _, c := range []struct {
		mgmt, pw string
		ok       bool
	}{
		{"wpa-psk", "1234567", false}, {"wpa-psk", "12345678", true}, {"wpa-psk", strings.Repeat("x", 63), true},
		{"wpa-psk", strings.Repeat("x", 64), false}, {"wpa-psk", hex, true}, {"wpa-psk", strings.Repeat("x", 65), false},
		{"wpa-psk", " spaced \\ 1 ", true}, {"wpa-psk", "line\nbreak1", false},
		{"sae", "short", true}, {"sae", strings.Repeat("x", 128), true}, {"sae", strings.Repeat("x", 129), false},
	} {
		if validWifiPassword(c.mgmt, c.pw) != c.ok {
			t.Errorf("%s %q: want %v", c.mgmt, c.pw, c.ok)
		}
	}
}
