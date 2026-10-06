pragma Singleton

import QtQuick
import Quickshell
import Quickshell.Services.Greetd
import "logic.js" as L

// The login conversation with greetd (through Quickshell's greetd client:
// create_session, answers to PAM's questions, start_session). The
// password goes from the field to greetd and nowhere else: it is never
// logged or written anywhere, and held in memory only when Enter comes
// before PAM's question (until the question arrives, then dropped).
Singleton {
    id: login

    // The person logging in: an entry of Sys.users, or null; "other user"
    // types a user name instead.
    property var user: null
    property bool other: false
    readonly property string userName: other ? otherName : (user ? user.name : "")
    property string otherName: ""
    property var session: null
    property string sessionFor: ""
    // Sessions are read after start: pick one for the chosen person then.
    Connections {
        target: Sys
        function onSessionsChanged() {
            if (login.userName !== "" && (!login.session || login.sessionFor !== login.userName)) {
                login.session = L.pickSession(Sys.sessions, Sys.state.sessions[login.userName], Sys.conf.DEFAULT_SESSION);
                login.sessionFor = login.userName;
            }
        }
    }

    // idle: nothing asked yet; prompt: PAM asks something (promptKind:
    // password, secret or visible); checking: an answer is with PAM;
    // ready: authenticated, waiting for the person to press Log in (an
    // account without a password); launching: fading into the session.
    property string phase: "idle"
    property string promptKind: "password"
    property string promptText: ""
    property string info: ""
    // The error shown: a message id of the catalog (translated when shown,
    // so it follows a change of language) with its argument, or PAM's own
    // text.
    property string errorId: ""
    property string errorArg: ""
    property string errorRaw: ""
    readonly property string error: errorRaw !== "" ? errorRaw
        : (errorId === "" ? "" : (errorArg !== "" ? I18n.t(errorId).arg(errorArg) : I18n.t(errorId)))
    function setError(id, arg) { errorRaw = ""; errorId = id; errorArg = arg || ""; }
    function setRawError(text) { errorId = ""; errorArg = ""; errorRaw = text; }
    function clearError() { errorId = ""; errorArg = ""; errorRaw = ""; }
    property int failures: 0
    readonly property bool available: Greetd.available
    readonly property bool busy: phase === "checking" || phase === "launching"
    property bool capsGuess: false
    readonly property bool capsOn: Sys.capsLed !== null ? Sys.capsLed : capsGuess

    // A press of Enter before PAM asked: answered as soon as it asks.
    property bool answerPending: false
    property var pendingAnswer: null

    // The last message PAM showed without asking anything.
    property string notice: ""

    signal failed()
    // A menu was used: the password field takes the keyboard again.
    signal focusRequested()

    // authFailed: the answer was refused. The message says why when PAM
    // gave a reason other than a wrong password (locked, expired).
    function authFailed(message) {
        failures++;
        const kind = L.failureKind(message);
        if (kind === "locked")
            setError(L.N_("Too many attempts. Wait a few minutes, then try again."));
        else if (kind === "expired")
            setRawError(message);
        else if (failures >= 3 && Sys.layout !== "")
            setError(L.N_("That password did not work. Check Caps Lock and the keyboard layout (%1)."), Sys.layout);
        else
            setError(L.N_("That password did not work. Try again."));
        notice = "";
        expectCancelReply = true;
        failed();
        phase = "idle";
        retry();
    }
    property bool expectCancelReply: false
    signal launching()

    function choose(u) {
        other = false;
        user = u;
        restart();
    }
    function chooseOther() {
        other = true;
        user = null;
        otherName = "";
        cancel();
    }
    function setOtherName(name) {
        otherName = name.trim();
        if (otherName !== "") restart();
    }
    function cancel() {
        if (Greetd.state !== GreetdState.Inactive) Greetd.cancelSession();
        phase = "idle"; promptText = ""; info = ""; notice = ""; answerPending = false; pendingAnswer = null;
    }
    // restart begins a new conversation for the chosen user (after a
    // failure PAM's conversation is over). greetd answers every request,
    // cancel included, and Quickshell's client takes a late "success" for
    // the cancel as the end of the next conversation's authentication: the
    // new conversation starts only after that answer is in (a short wait).
    function restart() {
        cancel();
        clearError();
        if (userName === "" || !available) return;
        // The person's last session, unless they just picked another one
        // for this login.
        if (sessionFor !== userName || !session) {
            session = L.pickSession(Sys.sessions, Sys.state.sessions[userName], Sys.conf.DEFAULT_SESSION);
            sessionFor = userName;
        }
        startTimer.restart();
    }
    Timer {
        id: startTimer
        interval: 350
        onTriggered: {
            if (login.userName === "" || Greetd.state !== GreetdState.Inactive) return;
            login.expectCancelReply = false;
            Greetd.createSession(login.userName);
        }
    }

    // submit answers PAM's current question with what the person typed.
    function submit(answer) {
        if (busy || userName === "") return;
        clearError();
        if (phase === "ready") { launch(); return; }
        if (phase !== "prompt") {
            // PAM has not asked yet: keep the answer until it does.
            if (Greetd.state === GreetdState.Inactive && !startTimer.running) restart();
            answerPending = true;
            pendingAnswer = answer;
            phase = "checking";
            return;
        }
        phase = "checking";
        Greetd.respond(answer);
    }

    // retry: a new conversation after a failure, keeping the message.
    function retry() {
        const id = errorId, arg = errorArg, raw = errorRaw;
        restart();
        errorId = id; errorArg = arg; errorRaw = raw;
    }

    function launch() {
        if (!session) { setError(L.N_("No desktop session is installed. Ask an administrator for help.")); return; }
        phase = "launching";
        Sys.saveState(L.remember(Sys.state, userName, session.file));
        login.launching();
        launchTimer.start();
    }
    // The screen fades into the session first (see GreeterScreen.qml).
    Timer {
        id: launchTimer
        interval: Math.max(G.slow * 1.6, 360)
        onTriggered: Greetd.launch(login.session.command, login.session.env, true)
    }

    Connections {
        target: Greetd
        function onAuthMessage(message, isError, responseRequired, echoResponse) {
            if (!responseRequired) {
                // Information from PAM ("your password expires in 3 days"),
                // shown as PAM wrote it.
                login.notice = message;
                if (isError) login.setRawError(message); else login.info = message;
                return;
            }
            login.promptKind = L.promptKind(message, echoResponse);
            login.promptText = message;
            if (login.answerPending && login.promptKind === "password") {
                const a = login.pendingAnswer;
                login.answerPending = false;
                login.pendingAnswer = null;
                login.phase = "checking";
                Greetd.respond(a);
                return;
            }
            login.answerPending = false;
            login.pendingAnswer = null;
            login.phase = "prompt";
        }
        function onAuthFailure(message) {
            login.authFailed(message);
        }
        function onReadyToLaunch() {
            // Only after the person pressed Enter or Log in: an account
            // without a password is never logged in by just being picked.
            if (login.phase === "checking") login.launch();
            else login.phase = "ready";
        }
        function onError(err) {
            // After a refused password Quickshell cancels the conversation,
            // and greetd 0.10 answers that cancel with an error (its worker
            // is already gone): expected, not a new problem.
            if (login.expectCancelReply) {
                login.expectCancelReply = false;
                login.retry();
                return;
            }
            login.setError(L.N_("Something went wrong while logging in. Try again."));
            login.phase = "idle";
            login.retry();
        }
    }
}
