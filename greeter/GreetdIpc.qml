pragma Singleton

import QtQuick
import Quickshell
import Quickshell.Io

// The login conversation's transport: greetd's requests and answers go
// through basalt-greetd-ipc, a small program that holds greetd's socket
// and frames its messages (one JSON line per request on its stdin, one
// per answer on its stdout). Quickshell's own greetd client, in the
// version Fedora ships, breaks when greetd's length and body arrive in two
// reads, and the login then hung with no message (release audit 2026-11,
// B6). The state machine below is the one that client had: same states,
// same signals, so Login.qml reads the same.
//
// Answers come back in the order of the requests. A cancel ends a
// conversation: answers to anything asked before it, and to the cancel
// itself, are dropped (greetd 0.10 answers a cancel after a refused
// password with an error, which is not news for the person).
Singleton {
    id: ipc

    readonly property string program: Quickshell.env("BASALT_GREETD_IPC") || "/usr/libexec/basalt-shell/basalt-greetd-ipc"
    readonly property bool available: Quickshell.env("GREETD_SOCK") !== ""
    // inactive, authenticating, ready, launching, launched
    property string state: "inactive"
    readonly property bool inactive: state === "inactive"
    property bool connected: false
    property bool responseRequired: false
    property string user: ""
    property bool exitAfterLaunch: false

    signal authMessage(string message, bool isError, bool responseRequired, bool echoResponse)
    signal authFailure(string message)
    signal readyToLaunch()
    signal launched()
    signal error(string message)

    // Requests waiting for an answer, oldest first: {type, gen}. gen is
    // the conversation they belong to; a cancel starts a new one.
    property var inflight: []
    property int gen: 0
    // Requests made before the program was up.
    property var queue: []

    function send(req) {
        if (!proc.running || !connected) { queue.push(req); return; }
        inflight.push({ type: req.type, gen: (req.type === "cancel_session" ? -1 : gen) });
        proc.write(JSON.stringify(req) + "\n");
    }
    function flushQueue() {
        const q = queue;
        queue = [];
        for (const r of q) send(r);
    }

    function createSession(name) {
        if (!available) return;
        user = name;
        if (state !== "inactive") cancelSession();
        state = "authenticating";
        send({ type: "create_session", username: name });
    }
    function cancelSession() {
        if (state === "inactive" && inflight.length === 0 && queue.length === 0) return;
        gen++;
        responseRequired = false;
        state = "inactive";
        send({ type: "cancel_session" });
    }
    function respond(answer) {
        if (!responseRequired) { console.warn("greetd: no question to answer"); return; }
        responseRequired = false;
        send({ type: "post_auth_message_response", response: answer });
    }
    function launch(command, env, exit) {
        if (state !== "ready") { console.warn("greetd: not ready to launch"); return; }
        state = "launching";
        exitAfterLaunch = exit;
        send({ type: "start_session", cmd: command, env: env });
    }
    // reset drops the connection (greetd ends a conversation whose client
    // goes away) and opens a new one: the way out of a conversation that
    // stopped answering.
    function reset() {
        gen++;
        inflight = [];
        queue = [];
        responseRequired = false;
        state = "inactive";
        connected = false;
        restartAfterExit = true;
        if (proc.running) proc.running = false; else proc.running = available;
    }
    property bool restartAfterExit: false

    function onLine(line) {
        let m;
        try { m = JSON.parse(line); } catch (e) { console.warn("greetd: unreadable answer"); return; }
        if (m.type === "ipc_ready") { connected = true; flushQueue(); return; }
        if (m.type === "ipc_error") { console.warn("greetd:", m.description); return; }
        const req = inflight.shift();
        if (!req || req.gen !== gen) return;   // a cancel, or before one
        if (m.type === "success") {
            if (state === "authenticating") { state = "ready"; readyToLaunch(); }
            else if (state === "launching") {
                state = "launched";
                launched();
                if (exitAfterLaunch) Qt.quit();
            }
        } else if (m.type === "error") {
            if (m.error_type === "error" && m.description === "a session is already being configured") {
                // A conversation left open (an earlier greeter): end it
                // and start again.
                const u = user;
                cancelSession();
                createSession(u);
                return;
            }
            const auth = m.error_type === "auth_error";
            // Errors end the conversation; greetd still holds a refused
            // one until it is cancelled.
            cancelSession();
            if (auth) authFailure(m.description || "");
            else error(m.description || "");
        } else if (m.type === "auth_message") {
            const kind = m.auth_message_type;
            const required = kind === "visible" || kind === "secret";
            responseRequired = required;
            authMessage(m.auth_message || "", kind === "error", required, kind !== "secret");
            if (!required) send({ type: "post_auth_message_response" });
        }
    }

    Process {
        id: proc
        command: [ipc.program]
        running: ipc.available
        stdinEnabled: true
        stdout: SplitParser { onRead: line => ipc.onLine(line) }
        onExited: (code, status) => {
            const asked = ipc.restartAfterExit;
            ipc.restartAfterExit = false;
            ipc.connected = false;
            ipc.inflight = [];
            ipc.responseRequired = false;
            const was = ipc.state;
            ipc.state = "inactive";
            if (was === "launched") return;
            if (!asked && was !== "inactive") ipc.error("the connection to greetd ended");
            restartTimer.restart();
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
