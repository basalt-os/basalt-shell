import QtQuick
import Quickshell
import Quickshell.Io
import Quickshell.Wayland
import Quickshell.Services.Pam
import "lock.js" as L

// The lock screen (docs/design.md, Lock screen). The session is locked
// through the compositor's ext-session-lock-v1 (WlSessionLock): while it
// is locked the compositor draws only these surfaces and sends input only
// to them, and it keeps the session locked if the shell stops (basalt-lock
// then lets swaylock take over). One surface per output (LockSurface).
//
// The password goes from the field to PAM (Quickshell's PamContext, service
// basalt-lock, which checks it through pam_unix and unix_chkpwd) and
// nowhere else: not to the daemon, never logged, cleared as soon as it is
// sent. The session is unlocked in one place only, finishUnlock(), reached
// only after PAM reported success for an answer the person sent (L.mayUnlock).
// No IPC call unlocks: the IPC target "lock" can lock and report the state,
// nothing else (shell/tests/lock.test.js checks both rules).
Scope {
    id: lockCtl

    // The lock state survives a reload of the shell's QML: the new
    // generation starts locked when the old one was. (The files are not
    // watched while locked, below; this is the second line.)
    PersistentProperties {
        id: persist
        reloadableId: "basalt-lock-state"
        property bool locked: false
        property string mainOutput: ""
    }
    readonly property bool locked: persist.locked
    // The output that had the focus when the screen was locked: its surface
    // shows the password card (an output name that matches no screen
    // never leaves the lock without its card: the first one shows it).
    readonly property string mainOutput: persist.mainOutput
    readonly property string cardOutput: {
        const names = Quickshell.screens.map(s => s.name);
        if (names.indexOf(mainOutput) >= 0) return mainOutput;
        return names.length > 0 ? names[0] : "";
    }
    // What the person typed, shared by every output's surface: the
    // compositor gives the keyboard to one lock surface (the first that
    // appeared, or the one clicked), which may not be the card's, so keys
    // typed on any output go into the card's field. Cleared as soon as it
    // is sent, on a refusal and on unlock.
    property string typed: ""

    // idle: waiting for the person; checking: PAM has the answer;
    // unlocking: PAM said yes, the screen fades, then unlocks.
    property string phase: "idle"
    readonly property bool busy: phase === "checking" || phase === "unlocking"
    // PAM's current question: password, secret, visible or info.
    property string promptKind: "password"
    property string promptText: ""
    // A message from PAM that asks nothing (a fingerprint reader's prompt,
    // "your password expires in 3 days"): shown as PAM wrote it.
    property string info: ""
    // What went wrong: an id QML turns into a translated sentence
    // (LockSurface), or PAM's own error text.
    property string errorId: ""
    property string errorRaw: ""
    property int failures: 0
    // Bumped after each refusal: the card shakes and the field is cleared.
    property int refusals: 0
    // Enter before PAM asked: answered as soon as it asks.
    property bool answerPending: false
    property string pendingAnswer: ""

    // Caps Lock: the keyboards' LEDs when the kernel has them, else a
    // guess from the typed letters.
    property bool capsGuess: false
    property var capsLed: null
    readonly property bool capsOn: capsLed !== null ? capsLed : capsGuess

    // The keyboard layouts and the active one: the shell daemon's keyboard
    // state (Bus.keyboard, the person's layouts from Settings, Keyboard or
    // the system's), the same as the panel's indicator; asked of the
    // compositor directly only while the daemon does not answer.
    property var probed: ({ names: [], active: 0 })
    readonly property bool shellLayouts: Bus.connected && Bus.keyboard && (Bus.keyboard.labels || []).length > 0
    readonly property var layouts: shellLayouts ? L.layoutFromIndicator(Bus.keyboard) : probed
    readonly property string layoutLabel: L.layoutLabel(layouts, Quickshell.env("XKB_DEFAULT_LAYOUT") || "")
    readonly property string layoutName: layouts.names[layouts.active] || ""
    readonly property bool canSwitchLayout: layouts.names.length > 1
    readonly property bool niri: (Quickshell.env("NIRI_SOCKET") || "") !== ""

    // The person: user name, real name, picture.
    readonly property string userName: L.userName(Quickshell.env("USER") || Quickshell.env("LOGNAME") || "", passwdFile.text(), Quickshell.env("HOME") || "")
    readonly property string realName: L.realName(passwdFile.text(), userName)
    readonly property string displayName: realName !== "" ? realName : userName
    FileView { id: passwdFile; path: "/etc/passwd"; blockLoading: true; printErrors: false }
    readonly property string osName: {
        const m = /^NAME="?([^"\n]*)"?/m.exec(osRelease.text() || "");
        return m ? m[1] : "Basalt OS";
    }
    FileView { id: osRelease; path: "/etc/os-release"; blockLoading: true; printErrors: false }

    // The PAM service: basalt-lock (packaging), else swaylock's (the
    // fallback locker is always installed), else login.
    readonly property string pamService: pamOwn.loaded ? "basalt-lock" : (pamSwaylock.loaded ? "swaylock" : "login")
    FileView { id: pamOwn; path: "/etc/pam.d/basalt-lock"; blockLoading: true; printErrors: false }
    FileView { id: pamSwaylock; path: "/etc/pam.d/swaylock"; blockLoading: true; printErrors: false }

    // The state for basalt-lock's guard ($XDG_RUNTIME_DIR/basalt-lock.state):
    // "locked" while this shell holds the lock, "unlocked" after finishUnlock.
    // The guard reads it to know when to ask whether it may stop watching
    // (it asks this shell over IPC too); writing it unlocks nothing.
    FileView {
        id: stateFile
        path: (Quickshell.env("XDG_RUNTIME_DIR") || "/tmp") + "/basalt-lock.state"
        printErrors: false
    }
    function writeState() { stateFile.setText(persist.locked ? "locked\n" : "unlocked\n"); }

    // No reload of the shell's files while locked (an update installed
    // meanwhile): Quickshell rebuilding the lock surfaces under a held
    // session lock is not something to trust the lock to. The files are
    // watched again once unlocked.
    Binding { target: Quickshell; property: "watchFiles"; value: false; when: persist.locked }

    // ------------------------------------------------------------ lock
    WlSessionLock {
        id: sessionLock
        reloadableId: "basalt-session-lock"
        locked: persist.locked
        LockSurface { ctl: lockCtl }
    }
    // secure: the compositor confirmed that every output shows the lock.
    readonly property bool secure: sessionLock.secure

    // lock: lock the session (Super+L, the power menu, idle, logind's lock
    // request, through basalt-lock). Locking again does nothing.
    function lock() {
        if (!persist.locked) {
            const out = (Bus.desktop.outputs || []).find(o => o.focused);
            persist.mainOutput = out ? out.name : (Quickshell.screens.length > 0 ? Quickshell.screens[0].name : "");
            typed = "";
            resetConversation();
            failures = 0;
            clearError();
            info = "";
            persist.locked = true;
            // The surfaces of the desktop close: after unlocking, the
            // person comes back to the windows, not to an open launcher.
            Ui.closeAll();
        }
        // The compositor refused or dropped an earlier lock (another locker
        // held it): ask again.
        if (!sessionLock.locked) sessionLock.locked = true;
        writeState();
        startPam();
        refreshLayouts();
        checkCaps();
    }

    // finishUnlock ends the lock. Reached only from unlockTimer, which only
    // runs after PAM's success for the person's answer (pamDone).
    function finishUnlock() {
        persist.locked = false;
        sessionLock.locked = false;
        typed = "";
        pendingAnswer = "";
        phase = "idle";
        failures = 0;
        clearError();
        info = "";
        writeState();
    }
    Timer {
        id: unlockTimer
        // The card and the blur fade first (LockSurface).
        interval: Math.max(Theme.normal, 1)
        onTriggered: if (lockCtl.phase === "unlocking") lockCtl.finishUnlock()
    }

    // ------------------------------------------------------------ PAM
    PamContext {
        id: pam
        config: lockCtl.pamService
        onPamMessage: {
            const kind = L.promptKind(pam.message, pam.responseRequired, pam.responseVisible);
            if (kind === "info") {
                if (pam.messageIsError) lockCtl.errorRaw = pam.message; else lockCtl.info = pam.message;
                return;
            }
            lockCtl.promptKind = kind;
            lockCtl.promptText = pam.message;
            if (lockCtl.answerPending) {
                const a = lockCtl.pendingAnswer;
                lockCtl.answerPending = false;
                lockCtl.pendingAnswer = "";
                lockCtl.phase = "checking";
                pam.respond(a);
                return;
            }
        }
        onCompleted: result => lockCtl.pamDone(L.pamResultName(result, PamResult))
        onError: err => {
            lockCtl.errorId = "error";
            lockCtl.pamFailed();
        }
    }
    function startPam() {
        if (!persist.locked || pam.active) return;
        if (!pam.start()) {
            errorId = "error";
            retryTimer.restart();
        }
    }
    function resetConversation() {
        if (pam.active) pam.abort();
        phase = "idle";
        answerPending = false;
        pendingAnswer = "";
        promptKind = "password";
        promptText = "";
    }
    // pamDone: the conversation ended. Unlock only when L.mayUnlock agrees.
    function pamDone(result) {
        if (L.mayUnlock(result, phase)) {
            phase = "unlocking";
            unlockTimer.restart();
            return;
        }
        failures++;
        errorRaw = "";
        errorId = L.failureMessage(result, failures);
        pamFailed();
    }
    function pamFailed() {
        typed = "";
        answerPending = false;
        pendingAnswer = "";
        phase = "idle";
        info = "";
        refusals++;
        // A new conversation for the next try (PAM's is over).
        retryTimer.restart();
    }
    Timer {
        id: retryTimer
        interval: 300
        onTriggered: lockCtl.startPam()
    }

    // submit answers PAM's question with what the person typed.
    function submit(text) {
        if (!persist.locked || busy) return;
        clearError();
        if (!pam.active || !pam.responseRequired) {
            // PAM has not asked yet: keep the answer until it does.
            answerPending = true;
            pendingAnswer = text;
            phase = "checking";
            startPam();
            return;
        }
        phase = "checking";
        pam.respond(text);
    }
    function clearError() { errorId = ""; errorRaw = ""; }
    // Return in a field (on any output): send what was typed, when there
    // is something (a stray Return on an empty field does nothing).
    function submitTyped() {
        if (busy || typed === "") return;
        const t = typed;
        typed = "";
        submit(t);
    }
    // Escape: clear the field and the message.
    function clearTyped() {
        typed = "";
        clearError();
    }

    // ------------------------------------------------------------ keyboard
    Process {
        id: layoutProc
        command: lockCtl.niri ? ["niri", "msg", "--json", "keyboard-layouts"] : ["swaymsg", "-t", "get_inputs", "-r"]
        stdout: StdioCollector {
            onStreamFinished: lockCtl.probed = lockCtl.niri ? L.layoutFromNiri(text) : L.layoutFromSway(text)
        }
    }
    function refreshLayouts() {
        if (Bus.connected) {
            Bus.call("keyboard.indicator", {}, (ok, res) => { if (ok && res) Bus.keyboard = res; });
            return;
        }
        if (!layoutProc.running) layoutProc.running = true;
    }
    Process {
        id: switchProc
        command: lockCtl.niri ? ["niri", "msg", "action", "switch-layout", "next"] : ["swaymsg", "input", "type:keyboard", "xkb_switch_layout", "next"]
        onExited: lockCtl.refreshLayouts()
    }
    function nextLayout() {
        if (!canSwitchLayout) return;
        if (shellLayouts) { Bus.switchLayout(); return; }
        if (!switchProc.running) switchProc.running = true;
    }
    Process {
        id: capsProc
        command: ["/bin/sh", "-c", "cat /sys/class/leds/*::capslock/brightness 2>/dev/null; true"]
        stdout: StdioCollector { onStreamFinished: lockCtl.capsLed = L.capsFromLeds(text) }
    }
    function checkCaps() { if (!capsProc.running) capsProc.running = true; }
    // A key in the field: Caps Lock from the letter and Shift (when there
    // is no LED), the LED itself, and the layout (it may have been switched
    // with the keyboard's own key).
    function noteKey(event) {
        if (event.key === Qt.Key_CapsLock) capsGuess = !capsGuess;
        const g = L.capsFromKey(event.text, (event.modifiers & Qt.ShiftModifier) !== 0);
        if (g !== null) capsGuess = g;
        checkCaps();
    }
    Timer {
        interval: 2000
        running: persist.locked
        repeat: true
        onTriggered: { lockCtl.checkCaps(); lockCtl.refreshLayouts(); }
    }

    // ------------------------------------------------------------ state
    // The daemon refuses push to talk and agent input while locked.
    Binding { target: Ui; property: "locked"; value: persist.locked }
    Component.onCompleted: {
        // A reload while locked: still locked. "unlocked" is written only
        // by finishUnlock.
        if (persist.locked) { writeState(); startPam(); refreshLayouts(); checkCaps(); }
    }

    // `basalt-shell-ui ipc call lock lock` (basalt-lock) and `... state`.
    // There is no unlock call, on purpose.
    IpcHandler {
        target: "lock"
        // Lock the session; returns at once (ask state for the result).
        function lock(): string { lockCtl.lock(); return "locking"; }
        // unlocked, locking (asked, not yet confirmed by the compositor)
        // or locked.
        function state(): string { return !persist.locked ? "unlocked" : (sessionLock.secure ? "locked" : "locking"); }
    }
}
