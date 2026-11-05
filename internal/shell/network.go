package shell

import (
	"context"
	"errors"
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

// wifiConnectArgs is the nmcli request that joins a network: a saved
// connection is brought up as it is; a new one is created by nmcli, which
// reads the password from its standard input (--ask) when the network
// needs one.
func wifiConnectArgs(ssid string, known, withPassword bool, device string) []string {
	if known && !withPassword {
		return []string{"--wait", "45", "connection", "up", "id", ssid}
	}
	args := []string{"--wait", "45"}
	if withPassword {
		args = append(args, "--ask")
	}
	args = append(args, "device", "wifi", "connect", ssid)
	if device != "" {
		args = append(args, "ifname", device)
	}
	return args
}

// WifiConnect joins a network. password is "" for open or saved networks.
// A saved network whose password changed is joined again with the new
// password (its old connection is replaced by nmcli).
func (c *Core) WifiConnect(ctx context.Context, ssid, password string) error {
	if !validSSID(ssid) {
		return errors.New(i18n.G("That is not a network name."))
	}
	if strings.ContainsAny(password, "\n\r") || len(password) > 128 {
		return errors.New(i18n.G("That password cannot be right: a Wi-Fi password has at most 63 characters on one line."))
	}
	st := c.Wifi(ctx, false)
	if st.Device == "" {
		return errors.New(i18n.G("This computer has no Wi-Fi adapter that works."))
	}
	known := false
	for _, n := range st.Networks {
		if n.SSID == ssid {
			if n.Enterprise {
				return errors.New(i18n.G("%s uses a company login (802.1X): join it in the network settings.", ssid))
			}
			known = n.Known
		}
	}
	cctx, cancel := context.WithTimeout(ctx, 60*time.Second)
	defer cancel()
	stdin := ""
	if password != "" {
		stdin = password + "\n"
	}
	_, err := nmcliRun(cctx, stdin, wifiConnectArgs(ssid, known, password != "", st.Device)...)
	_, _ = c.Audit.Append("ui", "ui", "wifi.connect", map[string]any{"ssid": ssid, "ok": err == nil})
	if err != nil {
		return wifiError(err)
	}
	return nil
}

// wifiError turns nmcli's message into one for the person.
func wifiError(err error) error {
	m := err.Error()
	switch {
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
