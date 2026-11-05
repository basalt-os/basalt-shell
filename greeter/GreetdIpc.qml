pragma Singleton

import QtQuick
import Quickshell
import Quickshell.Io
import "greetd.js" as G

// The login conversation's transport: greetd's requests and answers go
// through basalt-greetd-ipc, a small program that holds greetd's socket
// and frames its messages (one JSON line per request on its stdin, one
// per answer on its stdout). Quickshell's own greetd client, in the
// version Fedora ships, breaks when greetd's length and body arrive in two
// reads, and the login then hung with no message (release audit 2026-11,
// B6). The state machine (greetd.js, tested with Node) is the one that
// client had: same states, same signals, so Login.qml reads the same.
Singleton {
    id: ipc

    readonly property string program: Quickshell.env("BASALT_GREETD_IPC") || "/usr/libexec/basalt-shell/basalt-greetd-ipc"
    readonly property bool available: Quickshell.env("GREETD_SOCK") !== ""
    // The machine's state (greetd.js newState); `state` mirrors its
    // state for bindings: inactive, authenticating, ready, launching,
    // launched.
    property var m: G.newState()
    property string state: "inactive"
    readonly property bool inactive: state === "inactive"

    signal authMessage(string message, bool isError, bool responseRequired, bool echoResponse)
    signal authFailure(string message)
    signal readyToLaunch()
    signal launched()
    signal error(string message)

    // run carries out the machine's effects, in order.
    function run(fx) {
        ipc.state = ipc.m.state;
        for (const e of fx) {
            if (e.send) proc.write(JSON.stringify(e.send) + "\n");
            else if (e.quit) Qt.quit();
            else if (e.emit === "authMessage") ipc.authMessage(e.args[0], e.args[1], e.args[2], e.args[3]);
            else if (e.emit === "authFailure") ipc.authFailure(e.args[0]);
            else if (e.emit === "readyToLaunch") ipc.readyToLaunch();
            else if (e.emit === "launched") ipc.launched();
            else if (e.emit === "error") ipc.error(e.args[0]);
        }
        ipc.state = ipc.m.state;
    }

    function createSession(name) { if (available) run(G.createSession(m, name)); }
    function cancelSession() { run(G.cancelSession(m)); }
    function respond(answer) { run(G.respond(m, answer)); }
    function launch(command, env, exit) { run(G.launch(m, command, env, exit)); }
    // reset drops the connection (greetd ends a conversation whose client
    // goes away) and opens a new one: the way out of a conversation that
    // stopped answering.
    property bool restartAsked: false
    function reset() {
        run(G.reset(m));
        restartAsked = true;
        if (proc.running) proc.running = false; else proc.running = available;
    }

    function onLine(line) {
        let msg;
        try { msg = JSON.parse(line); } catch (e) { console.warn("greetd: unreadable answer"); return; }
        if (msg.type === "ipc_error") console.warn("greetd:", msg.description);
        run(G.answer(m, msg));
    }

    Process {
        id: proc
        command: [ipc.program]
        running: ipc.available
        stdinEnabled: true
        stdout: SplitParser { onRead: line => ipc.onLine(line) }
        onExited: (code, status) => {
            const asked = ipc.restartAsked;
            ipc.restartAsked = false;
            const launchedBefore = ipc.m.state === "launched";
            ipc.run(G.disconnected(ipc.m, asked));
            if (!launchedBefore) restartTimer.restart();
        }
    }
    // A new connection after the old one ended (greetd restarted, the
    // program failed): a moment later, so a broken socket does not spin.
    Timer {
        id: restartTimer
        interval: 500
        onTriggered: if (ipc.available && !proc.running) proc.running = true
    }
}
