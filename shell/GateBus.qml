pragma Singleton

import QtQuick
import Quickshell
import Quickshell.Io

// Connection to the approval gate (basalt-gate, newline-delimited JSON over
// /run/basalt-gate/gate.sock). On Basalt OS the gate is the one place where
// a request becomes a decision; this UI (SELinux domain basalt_shell_ui_t)
// is its desktop decider. Where the gate decides a path, the sheets send
// the person's decision here, straight to the gate: the shell daemon only
// runs what the gate allowed. Without a gate nothing connects, and the
// sheets decide through the daemon as before.
Singleton {
    id: gate

    // What the daemon found out with its own hello (present, enforce, socket).
    readonly property var info: Bus.gate || ({ present: false })
    readonly property bool available: !!info.present
    property bool connected: false
    property bool ready: false        // the gate took this UI as a decider
    property var _queue: []           // callbacks: replies come in order

    readonly property string socketPath: info.socket || "/run/basalt-gate/gate.sock"

    function enforced(path) {
        const e = info.enforce || [];
        return available && (e.indexOf(path) >= 0 || e.indexOf("all") >= 0);
    }

    // A request sent before the connection is up (the first decision after
    // the session starts, while the socket is still connecting) waits for
    // it, a few seconds at most, instead of failing at once: the first
    // Download on a model offer was lost that way, with nothing shown.
    property var _waiting: []         // { req, cb, until } until connected
    readonly property int connectWait: 8000

    function _send(req, cb) {
        if (!sock.connected) {
            // Only the daemon's word that the gate decides leads here, so
            // the gate is there even when this UI has not heard of it yet:
            // ask the daemon again and wait for the connection.
            gate._waiting.push({ req: req, cb: cb || null, until: Date.now() + gate.connectWait });
            waitTimer.start();
            if (!gate.available) Bus.refreshGate();
            else gate.ensure();
            return;
        }
        gate._queue.push(cb || null);
        sock.write(JSON.stringify(req) + "\n");
        sock.flush();
    }

    // ensure connects now where the gate is present (the shell calls it
    // once its state arrives, so the connection is up before the person
    // decides anything).
    function ensure() {
        if (gate.available && !sock.connected) { sock.connected = false; sock.connected = true; }
    }

    function _flushWaiting() {
        const list = gate._waiting;
        gate._waiting = [];
        for (const w of list) gate._send(w.req, w.cb);
    }

    Timer {
        id: waitTimer
        interval: 500; repeat: true
        onTriggered: {
            if (gate._waiting.length === 0) { stop(); return; }
            const now = Date.now();
            const keep = [];
            for (const w of gate._waiting) {
                if (now < w.until) keep.push(w);
                else if (w.cb) w.cb(false, Tr.t("The approvals service is not connected."));
            }
            gate._waiting = keep;
        }
    }

    function _handle(line) {
        let m;
        try { m = JSON.parse(line); } catch (e) { console.warn("basalt-gate: bad reply", e); return; }
        if (m.event !== undefined) return;
        const cb = gate._queue.shift();
        if (cb) cb(!!m.ok, m.ok ? m : m.error);
    }

    // decide approves or declines one request; remember: "Approve and
    // remember" (only where the gate offered it). An approval that needs an
    // administrator waits for polkit (the shell's own authentication dialog).
    function decide(id, approve, remember, cb) {
        _send({ op: "decide", id: id, approve: approve, remember: !!remember }, cb);
    }
    function status(id, cb) { _send({ op: "status", id: id }, cb); }

    // follow polls a request until its executor reported a result (the
    // system assistant's proposals, applied by basalt-gate-exec), then calls
    // cb(ok, view). Up to 30 minutes.
    property var _follow: []
    function follow(id, cb) {
        gate._follow.push({ id: id, cb: cb, until: Date.now() + 30 * 60 * 1000 });
        followTimer.start();
    }
    Timer {
        id: followTimer
        interval: 1500; repeat: true
        onTriggered: {
            if (gate._follow.length === 0) { stop(); return; }
            const list = gate._follow.slice();
            gate._follow = [];
            for (const f of list) {
                gate.status(f.id, (ok, res) => {
                    const v = ok && res.request ? res.request : null;
                    if (v && v.result) { f.cb(true, v); return; }
                    if (v && v.decision !== "allowed" && v.decision !== "asked") { f.cb(false, v.reason || v.decision); return; }
                    if (Date.now() > f.until) { f.cb(false, Tr.t("No result yet from the program that applies it.")); return; }
                    gate._follow.push(f);
                });
            }
        }
    }

    Socket {
        id: sock
        path: gate.socketPath
        connected: gate.available
        onConnectedChanged: {
            gate.connected = connected;
            // Requests sent on the connection that went away get no reply:
            // say so instead of dropping their callbacks.
            const lost = gate._queue;
            gate._queue = [];
            for (const cb of lost) if (cb) cb(false, Tr.t("The approvals service is not connected."));
            gate.ready = false;
            if (connected) {
                gate._send({ op: "hello", role: "decider", client: "basalt-shell-ui/" + Bus.version, protocol: "gate/1" }, (ok, res) => {
                    gate.ready = ok && (res.roles || []).indexOf("decider") >= 0;
                    if (!gate.ready) console.warn("basalt-gate: this UI is not a decider here:", JSON.stringify(res));
                    gate._flushWaiting();
                });
            }
        }
        parser: SplitParser {
            onRead: data => gate._handle(data)
        }
    }

    // Reconnect when the gate restarts.
    Timer {
        interval: 2000
        running: gate.available && !sock.connected
        repeat: true
        onTriggered: { sock.connected = false; sock.connected = true; }
    }
}
