pragma Singleton

import QtQuick
import Quickshell
import Quickshell.Io

// Connection to the basalt-shell daemon (newline-delimited JSON over a
// Unix socket). The UI takes the "ui" role: it receives events (desktop,
// theme, proposals, activity) and is the only client allowed to confirm
// proposals or act directly.
Singleton {
    id: bus

    property bool connected: false
    property bool ready: false
    property var desktop: ({ compositor: "", windows: [], workspaces: [], outputs: [], caps: {} })
    property var themeState: null
    property var tokens: themeState ? themeState.tokens : ({})
    property var pending: []          // shell proposals waiting for a decision
    property var activity: []         // audit records, newest last
    property var actions: []
    property bool assistantAvailable: false
    property bool translatorAvailable: false
    property var assistantPending: []
    property string version: ""
    property var control: null        // an agent's control session, or null
    property var uiCheck: null        // how the daemon recognizes this UI (selinux, exe)
    property var agentIO: ({})        // last-resort capabilities (screen capture, input)
    property var lastAgentActivity: null
    property var voice: ({ state: "idle", enabled: false })   // push to talk state
    property var grants: []           // scopes the person gave the read-only skills
    // Model downloads (zero setup): offers waiting for Download or Not
    // now, and the downloads the person agreed to.
    property var models: ({ offers: [], jobs: [], ask: "person" })
    property bool skillsAvailable: false
    // The UI's language is the session's (uiCatalog translates it, see
    // Tr.qml); the voice and the answers follow the person's own
    // settings (Settings, Voice and assistant).
    property string uiLang: "en"
    property var uiCatalog: ({})

    signal notify(var data)           // notification.show from an agent
    signal openRequested(string surface, string page)
    signal proposalChanged(var proposal)
    signal chooseRequested(var req)
    signal chooseDone(string id)
    signal agentActivity(var data)    // a screenshot or an input step by an agent
    signal voiceResult(var data)      // the answer to a spoken request
    signal voiceSettings(var data)    // the person's voice and assistant settings changed

    property int _next: 1
    property var _callbacks: ({})

    readonly property string socketPath: {
        const p = Quickshell.env("BASALT_SHELL_SOCKET");
        if (p) return p;
        return Quickshell.env("XDG_RUNTIME_DIR") + "/basalt-shell/shell.sock";
    }

    function call(op, args, cb) {
        if (!sock.connected) {
            if (cb) cb(false, "not connected to basalt-shell");
            return;
        }
        const id = bus._next++;
        if (cb) bus._callbacks[id] = cb;
        sock.write(JSON.stringify({ id: id, op: op, args: args || {} }) + "\n");
        sock.flush();
    }

    // Direct actions started by the person in the UI (no proposal).
    function execute(calls, cb) { call("execute", { calls: calls }, cb); }
    function act(action, args, cb) { execute([{ action: action, args: args || {} }], cb); }
    function decide(id, approve, cb) { call("decide", { id: id, approve: approve }, cb); }
    // Confirm with the person's edits of the editable fields (an e-mail draft's text).
    function decideEdited(id, edits, cb) { call("decide", { id: id, approve: true, edits: edits }, cb); }
    // The dictation shown on the voice card: type it, or drop it.
    function dictationDecide(approve) {
        const id = bus.voice ? bus.voice.proposal : "";
        if (id) call("decide", { id: id, approve: approve });
    }
    function ask(text, cb) { call("ask", { text: text }, cb); }
    // Push to talk: only this UI may open the microphone.
    function voicePress() { call("voice.press", { commandbar: Ui.commandBar }, (ok, res) => { if (!ok) bus.voice = { state: "error", error: res, enabled: bus.voice.enabled }; }); }
    function voiceRelease() { call("voice.release", {}); }
    function voiceCancel() { call("voice.cancel", {}); }
    // The person's voice and assistant settings: the whole settings with
    // one change, through voice.settings.set (shell UI only; the daemon
    // checks it against the administrator's policy). The Settings page
    // and the quick settings tile both save through here.
    function saveVoiceSettings(prefs, change, cb) {
        const p = Object.assign({}, prefs, change);
        p.voices = Object.assign({}, prefs.voices || {}, change.voices || {});
        call("voice.settings.set", p, cb);
    }
    function revokeGrant(id) { call("grant.revoke", { id: id || "" }); }
    // Model downloads: only this UI agrees to one (models.download) or
    // removes a model; the daemon refuses them from anyone else.
    function modelsDownload(id, cb) { call("models.download", { id: id }, cb); }
    function modelsStart(kind, target, cb) { call("models.download", { kind: kind, target: target }, cb); }
    function modelsDismiss(id) { call("models.dismiss", { id: id }); }
    function modelsRetry(id) { call("models.retry", { id: id }); }
    function modelsCancel(id) { call("models.cancel", { id: id }); }
    function modelsRemove(kind, name, cb) { call("models.remove", { kind: kind, target: name }, cb); }
    function setTokens(obj) { act("theme.set_tokens", { tokens: obj }); }
    function setToken(key, value) { const t = {}; t[key] = value; setTokens(t); }
    // Whether the person overrides a token (colors: in the current mode).
    function overridden(key) {
        const s = themeState ? themeState.settings : null;
        if (!s) return false;
        const o = s.overrides || {}, l = s.light || {}, d = s.dark || {};
        return key in o || (s.mode === "dark" ? key in d : key in l);
    }

    function refreshAssistant() {
        if (!assistantAvailable) return;
        call("assistant.pending", {}, (ok, res) => {
            if (ok && Array.isArray(res)) bus.assistantPending = res;
        });
    }

    function _upsertPending(p) {
        let list = bus.pending.filter(x => x.id !== p.id);
        if (p.status === "pending") list.push(p);
        bus.pending = list;
        bus.proposalChanged(p);
    }

    function _handle(line) {
        let m;
        try { m = JSON.parse(line); } catch (e) { console.warn("basalt-shell: bad message", e); return; }
        if (m.event !== undefined) {
            switch (m.event) {
            case "desktop": bus.desktop = m.data; break;
            case "theme": bus.themeState = m.data; break;
            case "proposal": bus._upsertPending(m.data); break;
            case "activity": {
                let a = bus.activity.slice();
                a.push(m.data);
                if (a.length > 200) a = a.slice(a.length - 200);
                bus.activity = a;
                break;
            }
            case "notify": bus.notify(m.data); break;
            case "ui": bus.openRequested(m.data.open || "", m.data.page || ""); break;
            case "choose": bus.chooseRequested(m.data); break;
            case "choice-done": bus.chooseDone(m.data.id); break;
            case "control": bus.control = m.data; break;
            case "agent-activity": bus.lastAgentActivity = m.data; bus.agentActivity(m.data); break;
            case "voice": bus.voice = m.data; break;
            case "voice-result": bus.voiceResult(m.data); break;
            case "grants": bus.grants = m.data || []; break;
            case "voice-settings": bus.voiceSettings(m.data); break;
            case "models": bus.models = m.data || ({ offers: [], jobs: [], ask: "person" }); break;
            case "translator": bus.translatorAvailable = !!(m.data && m.data.available); break;
            }
            return;
        }
        const cb = bus._callbacks[m.id];
        if (cb) {
            delete bus._callbacks[m.id];
            cb(m.ok, m.ok ? m.result : m.error);
        }
    }

    function _onConnected() {
        call("hello", { role: "ui", client: "quickshell" }, (ok, res) => {
            if (!ok) {
                // Without the ui role the shell cannot confirm anything:
                // reconnect and ask again in a moment.
                console.warn("basalt-shell: hello refused:", res);
                helloRetry.start();
            }
        });
        call("state", {}, (ok, s) => {
            if (!ok) return;
            bus.desktop = s.desktop;
            bus.themeState = s.theme;
            bus.pending = s.pending || [];
            bus.activity = s.activity || [];
            bus.actions = s.actions || [];
            bus.assistantAvailable = s.assistant;
            bus.translatorAvailable = s.translator;
            bus.version = s.version;
            bus.control = s.control || null;
            bus.uiCheck = s.ui_check || null;
            bus.agentIO = s.agent_io || ({});
            bus.voice = s.voice || ({ state: "idle", enabled: false });
            bus.grants = s.grants || [];
            bus.models = s.models || ({ offers: [], jobs: [], ask: "person" });
            bus.skillsAvailable = !!s.skills;
            bus.uiLang = s.ui_lang || "en";
            bus.uiCatalog = s.ui_catalog || ({});
            bus.ready = true;
            bus.call("ui.state", { modal: Ui.modal });
            bus.refreshAssistant();
        });
    }

    Socket {
        id: sock
        path: bus.socketPath
        connected: true
        onConnectedChanged: {
            bus.connected = connected;
            if (connected) bus._onConnected();
        }
        parser: SplitParser {
            onRead: data => bus._handle(data)
        }
    }

    // The daemon refuses agent input while the person has a dialog open.
    Connections {
        target: Ui
        function onModalChanged() { bus.call("ui.state", { modal: Ui.modal }); }
    }

    function stopControl() { call("control.stop", {}); }

    Timer { id: helloRetry; interval: 3000; onTriggered: { sock.connected = false; sock.connected = true; } }

    // Reconnect when the daemon restarts.
    Timer {
        interval: 1500
        running: !sock.connected
        repeat: true
        onTriggered: { sock.connected = false; sock.connected = true; }
    }

    Timer {
        interval: 60000
        running: bus.assistantAvailable
        repeat: true
        onTriggered: bus.refreshAssistant()
    }
}
