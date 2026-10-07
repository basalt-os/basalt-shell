// Pure functions of the greeter: parsing the system files it reads, picking
// the user and session, mapping PAM messages to friendly text. No QML or
// Quickshell types here, so the same file runs in the greeter (imported as
// a JavaScript resource) and in the unit tests (greeter/tests, Node).
//
// Nothing here ever sees a password: the password goes from the text field
// straight to greetd.

// parseKeyValue reads KEY=value files (os-release, locale.conf,
// vconsole.conf, /etc/basalt/greeter.conf): comments and blank lines are
// skipped, single or double quotes around the value are removed.
function parseKeyValue(text) {
    const out = {};
    for (const raw of String(text || "").split("\n")) {
        const line = raw.trim();
        if (line === "" || line[0] === "#" || line[0] === ";") continue;
        const eq = line.indexOf("=");
        if (eq <= 0) continue;
        const key = line.slice(0, eq).trim();
        let val = line.slice(eq + 1).trim();
        if (val.length >= 2 && (val[0] === '"' || val[0] === "'") && val[val.length - 1] === val[0])
            val = val.slice(1, -1);
        out[key] = val;
    }
    return out;
}

// parseLoginDefs returns the range of regular (human) user ids from
// /etc/login.defs; Fedora's defaults when the file says nothing.
function parseLoginDefs(text) {
    let uidMin = 1000, uidMax = 60000;
    for (const raw of String(text || "").split("\n")) {
        const f = raw.trim().split(/\s+/);
        if (f.length < 2 || f[0][0] === "#") continue;
        const n = parseInt(f[1], 10);
        if (isNaN(n)) continue;
        if (f[0] === "UID_MIN") uidMin = n;
        if (f[0] === "UID_MAX") uidMax = n;
    }
    return { uidMin: uidMin, uidMax: uidMax };
}

const noLoginShells = ["/sbin/nologin", "/usr/sbin/nologin", "/bin/false", "/usr/bin/false", "/bin/sync", ""];

// parsePasswd lists the people who can log in: user ids in the regular
// range, a login shell, not hidden by the greeter's configuration. Sorted
// by the name people see.
function parsePasswd(text, range, hidden) {
    const r = range || { uidMin: 1000, uidMax: 60000 };
    const hide = hidden || [];
    const users = [];
    for (const raw of String(text || "").split("\n")) {
        const line = raw.trim();
        if (line === "" || line[0] === "#" || line[0] === "+" || line[0] === "-") continue;
        const f = line.split(":");
        if (f.length < 7) continue;
        const uid = parseInt(f[2], 10);
        if (isNaN(uid) || uid < r.uidMin || uid > r.uidMax) continue;
        if (noLoginShells.indexOf(f[6]) >= 0) continue;
        if (hide.indexOf(f[0]) >= 0) continue;
        const gecos = (f[4] || "").split(",")[0].trim();
        users.push({ name: f[0], uid: uid, realName: gecos, home: f[5] });
    }
    users.sort((a, b) => displayName(a).localeCompare(displayName(b)));
    return users;
}

// displayName is what the login screen shows for a user: the full name
// when there is one, the user name otherwise.
function displayName(user) {
    if (!user) return "";
    return user.realName && user.realName !== "" ? user.realName : user.name;
}

// initials: one or two letters for an avatar without a picture
// ("Ana Souza" -> "AS", "edimar" -> "E"), by code point, so accented and
// non-Latin letters work.
function initials(name) {
    const words = String(name || "").trim().split(/\s+/).filter(w => w !== "");
    if (words.length === 0) return "?";
    const first = Array.from(words[0])[0] || "";
    const last = words.length > 1 ? (Array.from(words[words.length - 1])[0] || "") : "";
    return (first + last).toLocaleUpperCase();
}

// parseDesktopEntry reads a wayland-sessions .desktop file: the
// [Desktop Entry] group only, with the name in the given language when the
// file has it (Name[pt_BR], then Name[pt]).
function parseDesktopEntry(text, lang) {
    const e = { name: "", comment: "", exec: "", desktopNames: [], hidden: false, type: "" };
    const names = {}, comments = {};
    let inGroup = false;
    for (const raw of String(text || "").split("\n")) {
        const line = raw.trim();
        if (line === "" || line[0] === "#") continue;
        if (line[0] === "[") { inGroup = line === "[Desktop Entry]"; continue; }
        if (!inGroup) continue;
        const eq = line.indexOf("=");
        if (eq <= 0) continue;
        const key = line.slice(0, eq).trim();
        const val = line.slice(eq + 1).trim();
        let m = /^Name(\[(.+)\])?$/.exec(key);
        if (m) { names[m[2] || ""] = val; continue; }
        m = /^Comment(\[(.+)\])?$/.exec(key);
        if (m) { comments[m[2] || ""] = val; continue; }
        if (key === "Exec") e.exec = val;
        else if (key === "Type") e.type = val;
        else if (key === "DesktopNames") e.desktopNames = val.split(";").filter(s => s !== "");
        else if (key === "Hidden" || key === "NoDisplay") e.hidden = e.hidden || val === "true";
    }
    const pick = (map) => {
        const l = String(lang || "");
        const base = l.split("_")[0];
        if (l && map[l] !== undefined) return map[l];
        if (base && map[base] !== undefined) return map[base];
        return map[""] || "";
    };
    e.name = pick(names);
    e.comment = pick(comments);
    return e;
}

// splitExec turns a desktop entry's Exec value into an argument list (the
// Desktop Entry Specification's quoting: double quotes, backslash escapes
// inside them), dropping field codes such as %f or %U.
function splitExec(exec) {
    const args = [];
    let cur = "", inQuote = false, have = false;
    const s = String(exec || "");
    for (let i = 0; i < s.length; i++) {
        const c = s[i];
        if (inQuote) {
            if (c === "\\" && i + 1 < s.length) { cur += s[++i]; continue; }
            if (c === '"') { inQuote = false; continue; }
            cur += c;
            continue;
        }
        if (c === '"') { inQuote = true; have = true; continue; }
        if (c === " " || c === "\t") {
            if (have || cur !== "") args.push(cur);
            cur = ""; have = false;
            continue;
        }
        cur += c;
    }
    if (have || cur !== "") args.push(cur);
    return args.filter(a => !/^%[a-zA-Z]$/.test(a)).map(a => a.replace(/%%/g, "%"));
}

// N_ marks a message id for the catalog without translating it (the
// gettext convention); the greeter translates it where it is shown.
function N_(s) { return s; }

// knownSessions: friendlier names for Basalt's own sessions (message ids
// of the greeter's catalog); other sessions keep the name of their file.
const knownSessions = {
    "basalt-sway.desktop": N_("Basalt on Sway"),
    "basalt-niri.desktop": N_("Basalt on niri")
};

// makeSession builds what the greeter needs from a sessions file: its
// file name (the stable id), the label (message id when known), the
// command for greetd and the session environment greetd's other greeters
// set from DesktopNames.
function makeSession(file, entry) {
    if (!entry || entry.hidden || entry.exec === "") return null;
    const argv = splitExec(entry.exec);
    if (argv.length === 0) return null;
    const names = entry.desktopNames || [];
    const env = ["XDG_SESSION_TYPE=wayland"];
    if (names.length > 0) {
        env.push("XDG_SESSION_DESKTOP=" + names[0].toLowerCase());
        env.push("XDG_CURRENT_DESKTOP=" + names.join(":"));
    }
    return {
        file: file,
        label: entry.name !== "" ? entry.name : file.replace(/\.desktop$/, ""),
        known: knownSessions[file] || "",
        command: argv,
        env: env
    };
}

// sortSessions: Basalt on Sway first (the default), then niri, then the
// others by label.
function sortSessions(list) {
    const rank = s => s.file === "basalt-sway.desktop" ? 0 : (s.file === "basalt-niri.desktop" ? 1 : 2);
    return list.slice().sort((a, b) => rank(a) - rank(b) || a.label.localeCompare(b.label));
}

// pickSession: the session the person used last, if it is still
// installed; otherwise the default (Basalt on Sway), otherwise the first.
function pickSession(sessions, remembered, fallback) {
    if (!sessions || sessions.length === 0) return null;
    for (const want of [remembered, fallback, "basalt-sway.desktop"]) {
        if (!want) continue;
        const s = sessions.find(x => x.file === want);
        if (s) return s;
    }
    return sessions[0];
}

// pickUser: the person who logged in last, if still a user; a single user
// is always picked; with several and no memory, nobody (the list shows).
function pickUser(users, remembered) {
    if (!users || users.length === 0) return null;
    if (remembered) {
        const u = users.find(x => x.name === remembered);
        if (u) return u;
    }
    return users.length === 1 ? users[0] : null;
}

// uiLanguage: the greeter's language from the system locale (locale.conf,
// LANG, LC_MESSAGES) when a catalog exists for it; English otherwise.
function uiLanguage(locale, available) {
    const l = String(locale || "").split(".")[0].split("@")[0];
    if (l === "" || l === "C" || l === "POSIX") return "en";
    const have = available || [];
    if (have.indexOf(l) >= 0) return l;
    const base = l.split("_")[0];
    const sameBase = have.find(a => a.split("_")[0] === base);
    if (sameBase) return sameBase;
    return "en";
}

// promptKind tells a password prompt from other PAM questions: Linux-PAM
// and pam_unix ask "Password: " (translated by PAM in other languages);
// anything else (a one-time code, a new password) is shown as PAM wrote it.
function promptKind(message, echo) {
    if (echo) return "visible";
    const m = String(message || "").trim().replace(/:$/, "").trim().toLowerCase();
    if (m === "" || m === "password" || m === "senha" || m === "passwort" || m === "contraseña" || m === "mot de passe")
        return "password";
    return "secret";
}

// failureKind classifies greetd's auth error text: a plain wrong password,
// an account locked by pam_faillock, an expired account, or other.
function failureKind(message) {
    const m = String(message || "").toLowerCase();
    if (m.indexOf("locked") >= 0 || m.indexOf("bloquead") >= 0) return "locked";
    if (m.indexOf("expired") >= 0 || m.indexOf("expirad") >= 0) return "expired";
    return "wrong";
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

// parseState and serializeState: what the greeter remembers between
// boots (/var/cache/basalt-greeter/state.json): the last user, each
// user's session, and the accessibility choices. Never a password.
function parseState(text) {
    let s = {};
    try { s = JSON.parse(String(text || "{}")) || {}; } catch (e) { s = {}; }
    if (typeof s !== "object" || Array.isArray(s)) s = {};
    return {
        lastUser: typeof s.lastUser === "string" ? s.lastUser : "",
        sessions: (s.sessions && typeof s.sessions === "object" && !Array.isArray(s.sessions)) ? s.sessions : {},
        largeText: s.largeText === true,
        highContrast: s.highContrast === true,
        language: typeof s.language === "string" ? s.language : ""
    };
}

function serializeState(state) {
    const s = parseState(JSON.stringify(state || {}));
    return JSON.stringify(s, null, 1) + "\n";
}

// remember records a successful login: the user and their session.
function remember(state, user, sessionFile) {
    const s = parseState(JSON.stringify(state || {}));
    s.lastUser = user;
    if (sessionFile) s.sessions[user] = sessionFile;
    return s;
}

// parseNmcli reads `nmcli -t -f TYPE,STATE,CONNECTION device`: Wi-Fi
// wins over a cable when both are connected.
function parseNmcli(text) {
    let kind = "none", name = "";
    for (const line of String(text || "").split("\n")) {
        const f = line.split(":");
        if (f.length < 3 || f[1] !== "connected") continue;
        if (f[0] === "wifi" || (f[0] === "ethernet" && kind !== "wifi")) { kind = f[0]; name = f.slice(2).join(":"); }
    }
    return { kind: kind, name: name };
}

// parseInputs reads `swaymsg -t get_inputs -r`: the keyboard layouts the
// compositor has and the active one. codes are the configured layout codes
// (XKB_DEFAULT_LAYOUT, comma separated) in the same order.
function parseInputs(json, codes) {
    let list = [];
    try { list = JSON.parse(String(json || "[]")); } catch (e) { list = []; }
    // Virtual keyboards (wtype, an agent's) keep their own keymap: a real
    // keyboard's layouts are the system's (as the shell's indicator reads them).
    const real = i => !/virtual/.test(String(i.identifier || "") + String(i.name || ""));
    const kb = (Array.isArray(list) ? list : []).find(i => i && i.type === "keyboard" && real(i) && Array.isArray(i.xkb_layout_names) && i.xkb_layout_names.length > 0);
    const c = String(codes || "").split(",").map(s => s.trim()).filter(s => s !== "");
    if (!kb) return { names: [], codes: c, active: 0 };
    const active = typeof kb.xkb_active_layout_index === "number" ? kb.xkb_active_layout_index : 0;
    return { names: kb.xkb_layout_names, codes: c, active: active };
}

// layoutLabels: the labels of the system's layout codes, as the shell's
// Settings, Keyboard and panel show them: the code in capitals, and its
// position when a layout is listed twice ("us,us" gives US1, US2).
function layoutLabels(codes) {
    const c = (codes || []).map(x => String(x).toUpperCase());
    const count = {}, seen = {};
    for (const x of c) count[x] = (count[x] || 0) + 1;
    return c.map(x => count[x] > 1 ? x + (seen[x] = (seen[x] || 0) + 1) : x);
}

// layoutCode: the short label of the active layout ("BR", "US").
function layoutCode(inputs) {
    if (!inputs) return "";
    const labels = layoutLabels(inputs.codes);
    const code = labels[inputs.active] || labels[0] || "";
    if (code !== "") return code;
    const name = inputs.names[inputs.active] || "";
    return name.slice(0, 2).toUpperCase();
}
