// The login conversation's state machine for GreetdIpc.qml, kept free of
// QML so Node's test runner checks it (greeter/tests/greetd.test.js).
// Every function takes the state object, changes it, and returns the
// effects for the caller to carry out, in order:
//   { send: request }            a line for basalt-greetd-ipc's stdin
//   { emit: name, args: [...] }  a signal of GreetdIpc (authMessage,
//                                authFailure, readyToLaunch, launched, error)
//   { quit: true }               the session started: the greeter ends
//
// States: inactive, authenticating, ready, launching, launched. Answers
// come in the order of the requests; a cancel starts a new conversation
// (gen), and answers to anything asked before it, and to the cancel
// itself, are dropped.

function newState() {
    return { state: "inactive", connected: false, responseRequired: false, user: "",
             exitAfterLaunch: false, inflight: [], gen: 0, queue: [] };
}

// send queues a request until the helper said it is connected.
function send(s, req) {
    if (!s.connected) { s.queue.push(req); return []; }
    s.inflight.push({ type: req.type, gen: req.type === "cancel_session" ? -1 : s.gen });
    return [{ send: req }];
}

// connected: the helper's ipc_ready; the queued requests go out.
function connected(s) {
    s.connected = true;
    const q = s.queue;
    s.queue = [];
    let fx = [];
    for (const r of q) fx = fx.concat(send(s, r));
    return fx;
}

// disconnected: the helper ended. A conversation in progress is lost.
function disconnected(s, asked) {
    const was = s.state;
    s.connected = false;
    s.inflight = [];
    s.responseRequired = false;
    s.state = "inactive";
    if (was === "launched" || asked || was === "inactive") return [];
    return [{ emit: "error", args: ["the connection to greetd ended"] }];
}

function cancelSession(s) {
    if (s.state === "inactive" && s.inflight.length === 0 && s.queue.length === 0) return [];
    s.gen++;
    s.responseRequired = false;
    s.state = "inactive";
    return send(s, { type: "cancel_session" });
}

function createSession(s, name) {
    s.user = name;
    let fx = [];
    if (s.state !== "inactive") fx = cancelSession(s);
    s.state = "authenticating";
    return fx.concat(send(s, { type: "create_session", username: name }));
}

function respond(s, answer) {
    if (!s.responseRequired) return [];
    s.responseRequired = false;
    return send(s, { type: "post_auth_message_response", response: answer });
}

function launch(s, command, env, exit) {
    if (s.state !== "ready") return [];
    s.state = "launching";
    s.exitAfterLaunch = !!exit;
    return send(s, { type: "start_session", cmd: command, env: env });
}

// reset drops everything (the caller closes the connection and opens a
// new one).
function reset(s) {
    s.gen++;
    s.inflight = [];
    s.queue = [];
    s.responseRequired = false;
    s.state = "inactive";
    s.connected = false;
    return [];
}

// answer handles one answer from greetd (a parsed JSON line).
function answer(s, m) {
    if (m.type === "ipc_ready") return connected(s);
    if (m.type === "ipc_error") return [];
    const req = s.inflight.shift();
    if (!req || req.gen !== s.gen) return [];
    if (m.type === "success") {
        if (s.state === "authenticating") {
            s.state = "ready";
            return [{ emit: "readyToLaunch", args: [] }];
        }
        if (s.state === "launching") {
            s.state = "launched";
            const fx = [{ emit: "launched", args: [] }];
            if (s.exitAfterLaunch) fx.push({ quit: true });
            return fx;
        }
        return [];
    }
    if (m.type === "error") {
        if (m.error_type === "error" && m.description === "a session is already being configured") {
            // A conversation left open (an earlier greeter): end it and
            // start again.
            const u = s.user;
            return cancelSession(s).concat(createSession(s, u));
        }
        const auth = m.error_type === "auth_error";
        // Errors end the conversation; greetd still holds a refused one
        // until it is cancelled.
        const fx = cancelSession(s);
        fx.push({ emit: auth ? "authFailure" : "error", args: [m.description || ""] });
        return fx;
    }
    if (m.type === "auth_message") {
        const kind = m.auth_message_type;
        const required = kind === "visible" || kind === "secret";
        s.responseRequired = required;
        let fx = [{ emit: "authMessage", args: [m.auth_message || "", kind === "error", required, kind !== "secret"] }];
        if (!required) fx = fx.concat(send(s, { type: "post_auth_message_response" }));
        return fx;
    }
    return [];
}
