package shell

import (
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/basalt-os/basalt-shell/internal/apps"
	"github.com/basalt-os/basalt-shell/internal/i18n"
)

// Quick settings, network: the Wi-Fi networks around and joining one, from
// the shell UI only (the person's own choice; agents cannot join networks).
// Everything goes through NetworkManager's nmcli, which asks
// NetworkManager over the system bus; polkit lets the person of the active
// local session change their networks. A network's password goes from
// the UI to nmcli's standard input and nowhere else: never on a command
// line (other processes can read those), never in the activity log.

// WifiNetwork is one network in range (the strongest access point of each
// name).
type WifiNetwork struct {
	SSID     string `json:"ssid"`
	Signal   int    `json:"signal"`
	Security string `json:"security"`
	// Secure: a password is needed to join (WPA, WEP). Enterprise:
	// 802.1X, joined with the network settings application only.
	Secure     bool `json:"secure"`
	Enterprise bool `json:"enterprise,omitempty"`
	Active     bool `json:"active"`
	// Known: a saved connection of that name exists (joined before).
	Known bool `json:"known"`
}

// WifiState is what the quick settings' Wi-Fi list shows.
type WifiState struct {
	// Device: the first Wi-Fi interface ("" when there is none).
	Device   string        `json:"device"`
	Radio    bool          `json:"radio"`
	Networks []WifiNetwork `json:"networks"`
	// Editor: a network settings application can be opened
	// (nm-connection-editor, or nmtui in a terminal).
	Editor string `json:"editor"`
	Error  string `json:"error,omitempty"`
}

// nmcliRun runs nmcli with the given standard input (tests replace it).
var nmcliRun = func(ctx context.Context, stdin string, args ...string) (string, error) {
	cmd := exec.CommandContext(ctx, "nmcli", args...)
	// Messages in English, so they can be read here (the person sees the
	// shell's own translated text).
	cmd.Env = append(os.Environ(), "LC_ALL=C")
	if stdin != "" {
		cmd.Stdin = strings.NewReader(stdin)
	}
	out, err := cmd.CombinedOutput()
	if err != nil {
		msg := strings.TrimSpace(string(out))
		if msg == "" {
			msg = err.Error()
		}
		return string(out), errors.New(msg)
	}
	return string(out), nil
}

// splitTerse splits one line of nmcli's terse output (-t): fields are
// separated by ':', and ':' and '\' inside values are escaped with '\'.
func splitTerse(line string) []string {
	var out []string
	var b strings.Builder
	esc := false
	for _, r := range line {
		switch {
		case esc:
			b.WriteRune(r)
			esc = false
		case r == '\\':
			esc = true
		case r == ':':
			out = append(out, b.String())
			b.Reset()
		default:
			b.WriteRune(r)
		}
	}
	return append(out, b.String())
}

// parseWifiDevice returns the first Wi-Fi device of
// `nmcli -t -f DEVICE,TYPE,STATE device`.
func parseWifiDevice(out string) string {
	for _, l := range strings.Split(out, "\n") {
		f := splitTerse(l)
		if len(f) >= 2 && f[1] == "wifi" {
			return f[0]
		}
	}
	return ""
}

// parseWifiList reads `nmcli -t -f IN-USE,SSID,SECURITY,SIGNAL device wifi
// list`: one entry per name (the strongest, or the one in use), hidden
// networks (no name) left out, the network in use first, then by signal.
func parseWifiList(out string, known map[string]bool) []WifiNetwork {
	by := map[string]WifiNetwork{}
	for _, l := range strings.Split(out, "\n") {
		f := splitTerse(l)
		if len(f) < 4 || f[1] == "" {
			continue
		}
		sig, _ := strconv.Atoi(strings.TrimSpace(f[3]))
		sec := strings.TrimSpace(f[2])
		if sec == "--" {
			sec = ""
		}
		n := WifiNetwork{SSID: f[1], Signal: sig, Security: sec, Active: strings.TrimSpace(f[0]) == "*", Known: known[f[1]]}
		n.Secure = sec != ""
		n.Enterprise = strings.Contains(sec, "802.1X")
		old, seen := by[n.SSID]
		if seen {
			n.Active = n.Active || old.Active
			if old.Signal > n.Signal {
				n.Signal, n.Security, n.Secure, n.Enterprise = old.Signal, old.Security, old.Secure, old.Enterprise
			}
		}
		by[n.SSID] = n
	}
	list := make([]WifiNetwork, 0, len(by))
	for _, n := range by {
		list = append(list, n)
	}
	sort.Slice(list, func(i, j int) bool {
		if list[i].Active != list[j].Active {
			return list[i].Active
		}
		if list[i].Signal != list[j].Signal {
			return list[i].Signal > list[j].Signal
		}
		return strings.ToLower(list[i].SSID) < strings.ToLower(list[j].SSID)
	})
	return list
}

// parseKnownWifi returns the names of saved Wi-Fi connections from
// `nmcli -t -f NAME,TYPE connection show`.
func parseKnownWifi(out string) map[string]bool {
	known := map[string]bool{}
	for _, l := range strings.Split(out, "\n") {
		f := splitTerse(l)
		if len(f) >= 2 && (f[1] == "802-11-wireless" || f[1] == "wifi") {
			known[f[0]] = true
		}
	}
	return known
}

// networkEditor is how the network settings open: nm-connection-editor
// when installed, else nmtui in the terminal, else "" (nothing to open).
func (c *Core) networkEditor() (kind string, argv []string) {
	if a, ok := apps.Find(c.Apps(), "nm-connection-editor"); ok {
		if v, err := a.Argv(); err == nil {
			return "nm-connection-editor", v
		}
	}
	// Its desktop entry can be hidden from menus (NoDisplay): the
	// program itself is enough.
	if p, err := exec.LookPath("nm-connection-editor"); err == nil {
		return "nm-connection-editor", []string{p}
	}
	if _, err := exec.LookPath("nmtui"); err == nil {
		if t, err := exec.LookPath("foot"); err == nil {
			return "nmtui", []string{t, "--title", "Network", "nmtui"}
		}
	}
	return "", nil
}

// Wifi reports the Wi-Fi device, the radio and the networks in range.
// rescan asks the device to scan again first (a few seconds).
func (c *Core) Wifi(ctx context.Context, rescan bool) WifiState {
	st := WifiState{Networks: []WifiNetwork{}}
	st.Editor, _ = c.networkEditor()
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	out, err := nmcliRun(ctx, "", "-t", "-f", "DEVICE,TYPE,STATE", "device")
	if err != nil {
		st.Error = i18n.G("NetworkManager does not answer: %s", err.Error())
		return st
	}
	st.Device = parseWifiDevice(out)
	if st.Device == "" {
		return st
	}
	if r, err := nmcliRun(ctx, "", "-t", "-f", "WIFI", "radio"); err == nil {
		st.Radio = strings.TrimSpace(r) == "enabled"
	}
	if !st.Radio {
		return st
	}
	known := map[string]bool{}
	if k, err := nmcliRun(ctx, "", "-t", "-f", "NAME,TYPE", "connection", "show"); err == nil {
		known = parseKnownWifi(k)
	}
	re := "auto"
	if rescan {
		re = "yes"
	}
	list, err := nmcliRun(ctx, "", "-t", "-f", "IN-USE,SSID,SECURITY,SIGNAL", "device", "wifi", "list", "ifname", st.Device, "--rescan", re)
	if err != nil {
		st.Error = err.Error()
		return st
	}
	st.Networks = parseWifiList(list, known)
	return st
}

// validSSID: a network name nmcli can be given as one argument (1 to 32
// bytes, no control characters).
func validSSID(s string) bool {
	if s == "" || len(s) > 32 {
		return false
	}
	for _, r := range s {
		if r < 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

// keyMgmt is the key management a new connection uses for a network's
// security as nmcli lists it ("WPA2", "WPA1 WPA2", "WPA3", "WPA2 WPA3"):
// sae for WPA3 alone, wpa-psk otherwise; "" when this list cannot join it
// (WEP, 802.1X: the network settings can).
func keyMgmt(security string) string {
	switch {
	case strings.Contains(security, "802.1X") || strings.Contains(security, "WEP"):
		return ""
	case strings.Contains(security, "WPA3") && !strings.Contains(security, "WPA2") && !strings.Contains(security, "WPA1"):
		return "sae"
	}
	return "wpa-psk"
}

// The nmcli requests that join a network. A network's password never goes
// on a command line (other processes can read those): a new connection is
// added without it, then brought up with the password in a passwd-file
// read from standard input, and NetworkManager keeps it with the
// connection (the system's secret store, as for nm-connection-editor).

func wifiAddArgs(ssid, device, mgmt string) []string {
	args := []string{"connection", "add", "type", "wifi", "ifname", device, "con-name", ssid, "ssid", ssid}
	if mgmt != "" {
		args = append(args, "wifi-sec.key-mgmt", mgmt)
	}
	return args
}

func wifiUpArgs(kind, ref string, withPassword bool) []string {
	args := []string{"--wait", "30", "connection", "up", kind, ref}
	if withPassword {
		args = append(args, "passwd-file", "/dev/stdin")
	}
	return args
}

// wifiPasswdFile is the passwd-file line that gives the password.
// nmcli reads the value through nm_utils_buf_utf8safe_unescape with
// STRIP_SPACES (libnmc-base nmc_utils_parse_passwd_file): a backslash
// starts an escape (\\, \n, \ooo octal) and unescaped spaces at either end
// are dropped. So every backslash, and every space or control character,
// is written as an escape; anything else (UTF-8 included) goes as it is.
func wifiPasswdFile(password string) string {
	var b strings.Builder
	b.WriteString("802-11-wireless-security.psk:")
	for i := 0; i < len(password); i++ {
		ch := password[i]
		switch {
		case ch == '\\':
			b.WriteString(`\\`)
		case ch <= ' ' || ch == 0x7f:
			fmt.Fprintf(&b, "\\%03o", ch)
		default:
			b.WriteByte(ch)
		}
	}
	b.WriteString("\n")
	return b.String()
}

// unescapePasswd is nmcli's reading of a passwd-file value, for the tests
// (the same rules as nm_utils_buf_utf8safe_unescape with STRIP_SPACES).
func unescapePasswd(v string) string {
	isSpace := func(c byte) bool { return c == ' ' || (c >= '\t' && c <= '\r') }
	for len(v) > 0 && isSpace(v[0]) {
		v = v[1:]
	}
	var out []byte
	keep := 0 // bytes of out that came from escapes or before them (not stripped)
	for i := 0; i < len(v); i++ {
		if v[i] != '\\' {
			out = append(out, v[i])
			continue
		}
		i++
		if i >= len(v) {
			break
		}
		ch := v[i]
		if ch >= '0' && ch <= '9' {
			n := uint(ch - '0')
			for k := 0; k < 2 && i+1 < len(v) && v[i+1] >= '0' && v[i+1] <= '7'; k++ {
				i++
				n = n*8 + uint(v[i]-'0')
			}
			ch = byte(n & 0xff)
		} else {
			switch ch {
			case 'b':
				ch = '\b'
			case 'f':
				ch = '\f'
			case 'n':
				ch = '\n'
			case 'r':
				ch = '\r'
			case 't':
				ch = '\t'
			case 'v':
				ch = '\v'
			}
		}
		out = append(out, ch)
		keep = len(out)
	}
	for len(out) > keep && isSpace(out[len(out)-1]) {
		out = out[:len(out)-1]
	}
	return string(out)
}

// validWifiPassword checks a password for the network's key management the
// way NetworkManager does (nm_utils_wpa_psk_valid): WPA-PSK takes 8 to 63
// bytes, or 64 hexadecimal digits (the key itself); WPA3-SAE takes 1 to
// 128 bytes. One line only.
func validWifiPassword(mgmt, pw string) bool {
	if strings.ContainsAny(pw, "\n\r") {
		return false
	}
	if mgmt == "sae" {
		return len(pw) >= 1 && len(pw) <= 128
	}
	if len(pw) == 64 {
		for _, c := range pw {
			if !strings.ContainsRune("0123456789abcdefABCDEF", c) {
				return false
			}
		}
		return true
	}
	return len(pw) >= 8 && len(pw) <= 63
}

// parseAddedUUID reads the UUID from nmcli's "Connection 'X' (UUID)
// successfully added."
func parseAddedUUID(out string) string {
	i, j := strings.LastIndex(out, "("), strings.LastIndex(out, ")")
	if i < 0 || j <= i {
		return ""
	}
	return out[i+1 : j]
}

// WifiConnect joins a network. password is "" for open or saved networks;
// a saved network can be given a new password.
func (c *Core) WifiConnect(ctx context.Context, ssid, password string) error {
	if !validSSID(ssid) {
		return errors.New(i18n.G("That is not a network name."))
	}
	if strings.ContainsAny(password, "\n\r") || len(password) > 128 {
		return errors.New(i18n.G("A Wi-Fi password has 8 to 63 characters (or 64 hexadecimal digits), on one line."))
	}
	st := c.Wifi(ctx, false)
	if st.Device == "" {
		return errors.New(i18n.G("This computer has no Wi-Fi adapter that works."))
	}
	var net *WifiNetwork
	for i := range st.Networks {
		if st.Networks[i].SSID == ssid {
			net = &st.Networks[i]
		}
	}
	if net == nil {
		return errors.New(i18n.G("That network is not in range any more."))
	}
	mgmt := keyMgmt(net.Security)
	if net.Secure && mgmt == "" {
		return errors.New(i18n.G("%s uses a company login (802.1X) or WEP: join it in the network settings.", ssid))
	}
	if net.Secure && !net.Known && password == "" {
		return errors.New(i18n.G("%s needs a password.", ssid))
	}
	if password != "" && !validWifiPassword(mgmt, password) {
		return errors.New(i18n.G("A Wi-Fi password has 8 to 63 characters (or 64 hexadecimal digits), on one line."))
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	var err error
	switch {
	case !net.Secure && !net.Known:
		_, err = nmcliRun(cctx, "", "--wait", "30", "device", "wifi", "connect", ssid, "ifname", st.Device)
	case net.Known:
		stdin := ""
		if password != "" {
			stdin = wifiPasswdFile(password)
		}
		_, err = nmcliRun(cctx, stdin, wifiUpArgs("id", ssid, password != "")...)
	default:
		var out, uuid string
		out, err = nmcliRun(cctx, "", wifiAddArgs(ssid, st.Device, mgmt)...)
		if err == nil {
			uuid = parseAddedUUID(out)
			if uuid == "" {
				err = errors.New("nmcli did not say which connection it added")
			}
		}
		if err == nil {
			_, err = nmcliRun(cctx, wifiPasswdFile(password), wifiUpArgs("uuid", uuid, true)...)
			if err != nil && authenticated(st.Device) {
				// The password was right; the network has not given an
				// address yet (NetworkManager keeps trying).
				_, _ = c.Audit.Append("ui", "ui", "wifi.connect", map[string]any{"ssid": ssid, "ok": true, "address": false})
				return errors.New(i18n.G("Joined %s, but the network has not given this computer an address yet. It keeps trying.", ssid))
			}
			if err != nil {
				// A refused password must not leave a connection behind
				// that tries again on its own.
				dctx, dcancel := context.WithTimeout(context.Background(), 10*time.Second)
				_, _ = nmcliRun(dctx, "", "connection", "delete", "uuid", uuid)
				dcancel()
			}
		}
	}
	_, _ = c.Audit.Append("ui", "ui", "wifi.connect", map[string]any{"ssid": ssid, "ok": err == nil})
	if err != nil {
		return wifiError(err, net.Secure)
	}
	return nil
}

// authenticated reports whether the Wi-Fi device got past the password
// (NetworkManager is getting or checking an address, or is connected).
func authenticated(device string) bool {
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	out, err := nmcliRun(ctx, "", "-t", "-f", "GENERAL.STATE", "device", "show", device)
	if err != nil {
		return false
	}
	return parseAuthenticated(out)
}

// parseAuthenticated reads `nmcli -t -f GENERAL.STATE device show`:
// "GENERAL.STATE:70 (connecting (getting IP configuration))". States 70
// (ip-config) to 100 (connected) come after the authentication.
func parseAuthenticated(out string) bool {
	v := strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(out), "GENERAL.STATE:"))
	n, err := strconv.Atoi(strings.SplitN(v, " ", 2)[0])
	return err == nil && n >= 70 && n <= 100
}

// wifiError turns nmcli's message (in the C locale) into one for the
// person. A wrong WPA password shows as a timeout: NetworkManager asks for
// the secrets again until the wait is over.
func wifiError(err error, secure bool) error {
	m := err.Error()
	switch {
	case secure && (strings.Contains(m, "Timeout") || strings.Contains(m, "timed out")):
		return errors.New(i18n.G("The password was not accepted, or the network did not answer. Check the password and try again."))
	case strings.Contains(m, "Secrets were required") || strings.Contains(m, "802-11-wireless-security.psk: property is invalid") ||
		(strings.Contains(m, "password") && strings.Contains(m, "invalid")):
		return errors.New(i18n.G("The password was not accepted. Check it and try again."))
	case strings.Contains(m, "No network with SSID"):
		return errors.New(i18n.G("That network is not in range any more."))
	case strings.Contains(m, "Timeout") || strings.Contains(m, "timed out"):
		return errors.New(i18n.G("The network did not answer in time. Try again closer to it."))
	}
	return errors.New(i18n.G("Could not join the network: %s", strings.TrimPrefix(m, "Error: ")))
}

// WifiRadio turns Wi-Fi on or off.
func (c *Core) WifiRadio(ctx context.Context, on bool) error {
	v := "off"
	if on {
		v = "on"
	}
	cctx, cancel := context.WithTimeout(ctx, 15*time.Second)
	defer cancel()
	_, err := nmcliRun(cctx, "", "radio", "wifi", v)
	_, _ = c.Audit.Append("ui", "ui", "wifi.radio", map[string]any{"on": on, "ok": err == nil})
	if err != nil && on {
		return errors.New(i18n.G("Could not turn Wi-Fi on: %s", err.Error()))
	}
	if err != nil {
		return errors.New(i18n.G("Could not turn Wi-Fi off: %s", err.Error()))
	}
	return nil
}

// OpenNetworkSettings opens the network settings application, or nmtui
// in a terminal, and says which; an error when neither can start.
func (c *Core) OpenNetworkSettings(ctx context.Context) (string, error) {
	kind, argv := c.networkEditor()
	if kind == "" {
		return "", errors.New(i18n.G("No network settings application is installed (nm-connection-editor, or NetworkManager-tui with foot)."))
	}
	if err := c.Launch(ctx, kind, argv); err != nil {
		return kind, errors.New(i18n.G("Could not open the network settings: %s", err.Error()))
	}
	return kind, nil
}
