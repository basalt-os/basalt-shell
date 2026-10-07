// Pure functions of the lock screen (Lock.qml): the rule that decides when
// the session may be unlocked, PAM messages, the person's name, Caps Lock
// and the keyboard layout. No QML or Quickshell types here, so the same
// file runs in the shell (imported as a JavaScript resource) and in the
// unit tests (shell/tests, Node).
//
// Nothing here ever sees a password: the password goes from the text field
// straight to PAM (Quickshell's PamContext).

// pamResultName maps Quickshell's PamResult value to a name, given the
// enum object (PamResult in QML, a plain object in the tests).
function pamResultName(result, names) {
    if (result === names.Success) return "success";
    if (result === names.Failed) return "failed";
    if (result === names.MaxTries) return "maxtries";
    if (result === names.Error) return "error";
    return "unknown";
}

// mayUnlock is the only rule that ends the lock: PAM reported success for
// a conversation in which the person answered (phase "checking"). Any
// other result, or a success nobody asked for (no answer was sent), keeps
// the session locked.
function mayUnlock(resultName, phase) {
    return resultName === "success" && phase === "checking";
}

// failureMessage picks what to say after a refused attempt: an id the QML
// turns into a translated sentence. After three wrong passwords it points
// at Caps Lock and the keyboard layout.
function failureMessage(resultName, failures) {
    if (resultName === "error") return "error";
    if (resultName === "maxtries") return "maxtries";
    return failures >= 3 ? "wrong-hint" : "wrong";
}

// promptKind classifies PAM's question: the password, another secret (a
// one-time code), something to type in the clear, or no question at all
// (information such as "Place your finger on the reader").
function promptKind(message, responseRequired, responseVisible) {
    if (!responseRequired) return "info";
    if (responseVisible) return "visible";
    const m = String(message || "").trim().replace(/:$/, "").trim().toLowerCase();
    if (m === "" || m === "password" || m === "senha" || m === "passwort" || m === "contraseña" || m === "mot de passe")
        return "password";
    return "secret";
}

// realName reads the person's name from /etc/passwd (the first field of
// GECOS), or "" when there is none.
function realName(passwd, user) {
    for (const line of String(passwd || "").split("\n")) {
        const f = line.split(":");
        if (f.length < 7 || f[0] !== user) continue;
        return (f[4] || "").split(",")[0].trim();
    }
    return "";
}

// userName: the person logged in, from the environment ($USER, $LOGNAME),
// else the /etc/passwd entry whose home is $HOME.
function userName(env, passwd, home) {
    if (env) return env;
    if (!home) return "";
    for (const line of String(passwd || "").split("\n")) {
        const f = line.split(":");
        if (f.length >= 7 && f[5] === home) return f[0];
    }
    return "";
}

// initials: up to two letters of a name, for a person without a picture.
function initials(name) {
    const words = String(name || "").trim().split(/\s+/).filter(w => w !== "");
    if (words.length === 0) return "?";
    const first = Array.from(words[0])[0] || "";
    const last = words.length > 1 ? (Array.from(words[words.length - 1])[0] || "") : "";
    return (first + last).toLocaleUpperCase();
}

// capsFromKey guesses Caps Lock from one typed letter and the Shift state:
// an upper case letter typed without Shift (or lower case with Shift) means
// Caps Lock is on. null when the key says nothing (digits, symbols).
function capsFromKey(text, shift) {
    const c = String(text || "");
    if (c.length !== 1) return null;
    const up = c.toLocaleUpperCase(), low = c.toLocaleLowerCase();
    if (up === low) return null;
    const isUpper = c === up;
    return shift ? !isUpper : isUpper;
}

// capsFromLeds reads the Caps Lock LEDs (/sys/class/leds/*::capslock/
// brightness, one value per line): on when any keyboard shows it on;
// null when there is no LED to read.
function capsFromLeds(text) {
    const vals = String(text || "").split("\n").map(s => s.trim()).filter(s => s !== "");
    if (vals.length === 0) return null;
    return vals.some(v => parseInt(v, 10) > 0);
}

// layoutFromSway reads `swaymsg -t get_inputs -r`: the names of the first
// keyboard's layouts and the active one. Keyboards without named layouts
// (a virtual keyboard with its own keymap) are skipped.
function layoutFromSway(json) {
    let list = [];
    try { list = JSON.parse(String(json || "[]")); } catch (e) { list = []; }
    const named = i => i && i.type === "keyboard" && Array.isArray(i.xkb_layout_names) &&
        i.xkb_layout_names.length > 0 && i.xkb_layout_names.every(n => typeof n === "string" && n !== "");
    const kb = (Array.isArray(list) ? list : []).find(named);
    if (!kb) return { names: [], active: 0 };
    const active = typeof kb.xkb_active_layout_index === "number" ? kb.xkb_active_layout_index : 0;
    return { names: kb.xkb_layout_names, active: active };
}

// layoutFromNiri reads `niri msg --json keyboard-layouts`.
function layoutFromNiri(json) {
    let d = {};
    try { d = JSON.parse(String(json || "{}")) || {}; } catch (e) { d = {}; }
    const names = Array.isArray(d.names) ? d.names : [];
    const active = typeof d.current_idx === "number" ? d.current_idx : 0;
    return { names: names, active: active };
}

// layoutFromIndicator reads the shell daemon's keyboard indicator
// (keyboard.indicator: the labels of the layouts the session uses, the
// person's own from Settings, Keyboard or the system's, and the active
// one; names when a real keyboard reports them). The same state as the
// Keyboard page and the panel's indicator.
function layoutFromIndicator(ind) {
    const labels = ind && Array.isArray(ind.labels) ? ind.labels.filter(l => typeof l === "string" && l !== "") : [];
    if (labels.length === 0) return { names: [], active: 0, labels: [] };
    const names = ind.live && Array.isArray(ind.names) && ind.names.length === labels.length ? ind.names : labels;
    let active = typeof ind.current === "number" ? ind.current : 0;
    if (active < 0 || active >= labels.length) active = 0;
    return { names: names, active: active, labels: labels };
}

// layoutLabel: the short label of the active layout: the daemon's label
// (layoutFromIndicator), else from the system's
// layout codes (XKB_DEFAULT_LAYOUT, "br,us") when they line up with the
// names, else the start of the name ("English (US)" -> "EN").
function layoutLabel(layouts, codes) {
    if (!layouts || layouts.names.length === 0) return "";
    if (Array.isArray(layouts.labels) && layouts.labels[layouts.active]) return layouts.labels[layouts.active];
    const c = String(codes || "").split(",").map(s => s.trim()).filter(s => s !== "");
    if (c.length === layouts.names.length && c[layouts.active]) return c[layouts.active].toUpperCase();
    const name = layouts.names[layouts.active] || "";
    return name.slice(0, 2).toUpperCase();
}
