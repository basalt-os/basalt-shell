pragma Singleton

import QtQuick
import Quickshell
import Quickshell.Io
import Quickshell.Services.UPower
import "logic.js" as L

// What the login screen reads from the system: the people who can log in,
// the installed sessions, the greeter's configuration and memory, the
// network, the battery, the keyboard layout and Caps Lock. And the few
// things it does to the system: power actions and switching the layout.
// Paths can be moved for the tests (BASALT_GREETER_ROOT and friends); the
// greeter's launcher never sets them.
Singleton {
    id: sys

    readonly property string root: Quickshell.env("BASALT_GREETER_ROOT") || ""
    readonly property string stateDir: Quickshell.env("BASALT_GREETER_STATE") || "/var/cache/basalt-greeter"
    readonly property string runDir: Quickshell.env("BASALT_GREETER_RUN") || "/run/basalt-greeter"
    readonly property string sessionsDir: root + "/usr/share/wayland-sessions"
    readonly property string avatarDir: root + "/var/lib/AccountsService/icons"

    // readFile: the whole file, or "" when it cannot be read.
    function readFile(path) {
        const fv = Qt.createQmlObject('import Quickshell.Io; FileView { blockLoading: true; printErrors: false }', sys, "readFile");
        fv.path = path;
        const t = fv.text() || "";
        fv.destroy();
        return t;
    }

    // ------------------------------------------------------------ configuration
    // /etc/basalt/greeter.conf: THEME, MODE, HIDE_USERS (comma separated),
    // DEFAULT_SESSION (a file name in wayland-sessions).
    readonly property var conf: L.parseKeyValue(readFile(root + "/etc/basalt/greeter.conf"))
    readonly property string osName: L.parseKeyValue(readFile(root + "/etc/os-release")).NAME || "Basalt OS"

    // ------------------------------------------------------------ memory
    property var state: L.parseState(readFile(stateDir + "/state.json"))
    FileView {
        id: stateFile
        path: sys.stateDir + "/state.json"
        printErrors: false
    }
    function saveState(s) {
        sys.state = L.parseState(JSON.stringify(s));
        stateFile.setText(L.serializeState(sys.state));
    }

    // ------------------------------------------------------------ people
    readonly property var users: L.parsePasswd(readFile(root + "/etc/passwd"),
                                               L.parseLoginDefs(readFile(root + "/etc/login.defs")),
                                               (conf.HIDE_USERS || "").split(",").map(s => s.trim()).filter(s => s !== ""))
    function avatar(name) { return "file://" + avatarDir + "/" + name; }

    // ------------------------------------------------------------ sessions
    property var sessions: []
    Process {
        id: sessionsProc
        // One read of every sessions file: a NUL and the file name before
        // each file's text.
        command: ["/bin/sh", "-c", 'for f in "$1"/*.desktop; do [ -f "$f" ] || continue; printf "\\001%s\\n" "${f##*/}"; cat "$f"; done', "sh", sys.sessionsDir]
        running: true
        stdout: StdioCollector {
            onStreamFinished: {
                const list = [];
                for (const chunk of text.split("\u0001")) {
                    const nl = chunk.indexOf("\n");
                    if (nl <= 0) continue;
                    const file = chunk.slice(0, nl);
                    const s = L.makeSession(file, L.parseDesktopEntry(chunk.slice(nl + 1), I18n.lang));
                    if (s) list.push(s);
                }
                sys.sessions = L.sortSessions(list);
            }
        }
    }

    // ------------------------------------------------------------ network
    property var net: ({ kind: "none", name: "" })
    Process {
        id: netProc
        command: ["nmcli", "-t", "-f", "TYPE,STATE,CONNECTION", "device"]
        stdout: StdioCollector { onStreamFinished: sys.net = L.parseNmcli(text) }
    }
    Timer { interval: 10000; running: true; repeat: true; triggeredOnStart: true; onTriggered: netProc.running = true }

    // ------------------------------------------------------------ battery
    readonly property var battery: UPower.displayDevice
    readonly property bool hasBattery: battery !== null && battery.isLaptopBattery
    readonly property int batteryPercent: hasBattery ? Math.round(battery.percentage * (battery.percentage <= 1 ? 100 : 1)) : 0
    readonly property bool charging: hasBattery && (battery.state === UPowerDeviceState.Charging || battery.state === UPowerDeviceState.FullyCharged)

    // ------------------------------------------------------------ keyboard
    readonly property string layoutCodes: Quickshell.env("XKB_DEFAULT_LAYOUT") || ""
    property var inputs: ({ names: [], codes: [], active: 0 })
    readonly property string layout: L.layoutCode(inputs)
    readonly property string layoutName: inputs.names[inputs.active] || layout
    readonly property bool canSwitchLayout: inputs.names.length > 1
    Process {
        id: inputsProc
        command: ["swaymsg", "-t", "get_inputs", "-r"]
        running: true
        stdout: StdioCollector { onStreamFinished: sys.inputs = L.parseInputs(text, sys.layoutCodes) }
    }
    Process {
        id: switchProc
        command: ["swaymsg", "input", "type:keyboard", "xkb_switch_layout", "next"]
        onExited: inputsProc.running = true
    }
    function nextLayout() { if (canSwitchLayout) switchProc.running = true; }

    // Caps Lock: the keyboards' LEDs when the kernel has them, else a
    // guess from the typed letters (Login.capsGuess).
    property var capsLed: null
    Process {
        id: capsProc
        command: ["/bin/sh", "-c", "cat /sys/class/leds/*::capslock/brightness 2>/dev/null; true"]
        stdout: StdioCollector { onStreamFinished: sys.capsLed = L.capsFromLeds(text) }
    }
    function checkCaps() { if (!capsProc.running) capsProc.running = true; }

    // ------------------------------------------------------------ power
    Process { id: powerProc }
    function power(action) {
        const verb = { suspend: "suspend", restart: "reboot", poweroff: "poweroff" }[action];
        if (!verb) return;
        powerProc.command = ["systemctl", verb];
        powerProc.running = true;
    }

    // ------------------------------------------------------------ ready
    // The launcher waits for this file: a greeter that never gets here
    // (broken QML, no GPU at all) is replaced by the text login.
    FileView { id: readyFile; path: sys.runDir + "/ready"; printErrors: false }
    function markReady() { readyFile.setText("ready\n"); }
}
