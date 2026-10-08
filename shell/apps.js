// Pure functions about applications: the launcher's ranking and the
// name a person reads for an app window. No QML or Quickshell types
// here, so the same file runs in the shell (imported as a JavaScript
// resource) and in the unit tests (shell/tests, Node).
//
// The apps come from the daemon (the "apps" call): name, generic name and
// comment in the session's language, the untranslated words (aliases),
// keywords, and kinds (the plain words of the categories: "text editor"
// for TextEditor). The tiers follow internal/apps Score, so the launcher
// and "open text editor" agree.

// fold lowercases, drops the accents and collapses spaces.
function fold(s) {
    let t = String(s || "").toLowerCase();
    if (typeof t.normalize === "function") t = t.normalize("NFD").replace(/[\u0300-\u036f]/g, "");
    return t.replace(/\s+/g, " ").trim();
}

// words: the runs of letters and digits (Latin letters, and the other
// scripts' letters above U+00C0).
function words(s) {
    return fold(s).split(/[^a-z0-9\u00c0-\uffff]+/).filter(w => w !== "");
}

// fuzzy: every character of q appears in s in order; consecutive and
// word-start matches score higher. null when it does not match.
function fuzzy(q, s) {
    s = fold(s);
    let qi = 0, sc = 0, prev = -2;
    for (let i = 0; i < s.length && qi < q.length; i++) {
        if (s[i] === q[qi]) {
            sc += 1;
            if (i === prev + 1) sc += 3;
            if (i === 0 || " -._".indexOf(s[i - 1]) >= 0) sc += 5;
            prev = i; qi++;
        }
    }
    return qi === q.length ? sc - s.length * 0.01 : null;
}

// score ranks an app for a query (higher is better; null: no match).
// The tiers, best first: the desktop id, the name, the generic name, the
// kind of app, a name or generic name that starts with the query, a
// keyword, every word of the query in what describes the app, a
// substring; last, a fuzzy match (letters in order), which never beats a
// word match. So "text editor" puts Mousepad (generic name Text Editor,
// category TextEditor) first, and never a terminal.
function score(app, query) {
    const q = fold(query);
    if (q === "") return 0;
    const id = fold(app.id);
    const last = id.lastIndexOf(".") >= 0 ? id.slice(id.lastIndexOf(".") + 1) : id;
    const eq = list => (list || []).some(s => s && fold(s) === q);
    const name = fold(app.name), generic = fold(app.generic);
    if (id === q || last === q) return 1000;
    if (name === q) return 900;
    if (eq(app.aliases)) return 880;
    if (generic !== "" && generic === q) return 800;
    if (eq(app.kinds)) return 700;
    if (name.startsWith(q)) return 600;
    if (generic !== "" && generic.startsWith(q)) return 560;
    if (eq(app.keywords)) return 500;
    const strong = words([app.name, app.generic].concat(app.aliases || [], app.kinds || [], app.keywords || []).join(" "));
    const weak = words(app.comment);
    const qw = words(q);
    if (qw.length > 0) {
        const inStrong = qw.every(w => strong.some(x => x.startsWith(w)));
        if (inStrong) return 400;
    }
    if (name.indexOf(q) >= 0 || id.indexOf(q) >= 0 || (generic !== "" && generic.indexOf(q) >= 0)) return 300;
    if (qw.length > 0 && qw.every(w => strong.some(x => x.startsWith(w)) || weak.some(x => x.startsWith(w)))) return 200;
    // Letters in order ("fx" for Firefox): below every word match.
    let best = null;
    const fields = [app.name, app.generic, (app.keywords || []).join(" "), app.id];
    for (let i = 0; i < fields.length; i++) {
        const s = fuzzy(q, fields[i] || "");
        if (s !== null) {
            const w = Math.min(99, s) * (i === 0 ? 1 : 0.6);
            if (best === null || w > best) best = w;
        }
    }
    return best;
}

// rank: the apps that match, best first (ties by name). An empty query
// lists every app by name.
function rank(apps, query) {
    const out = [];
    for (const a of apps || []) {
        const s = score(a, query);
        if (s !== null) out.push({ app: a, s: s });
    }
    out.sort((x, y) => y.s - x.s || String(x.app.name).localeCompare(String(y.app.name)));
    return out.map(x => x.app);
}

// prettyId turns an app id into a readable name when no desktop entry
// names it: "org.xfce.mousepad" gives "Mousepad", "gnome-calculator"
// gives "Gnome calculator".
function prettyId(appId) {
    let s = String(appId || "").replace(/\.desktop$/, "");
    if (s.indexOf(".") >= 0) s = s.slice(s.lastIndexOf(".") + 1);
    s = s.replace(/[-_]+/g, " ").trim();
    return s === "" ? "" : s.charAt(0).toUpperCase() + s.slice(1);
}

// appLabel is the name a person reads for an app window: its desktop
// entry's name (in their language), else a readable form of the app id,
// else the window title.
function appLabel(appId, entryName, title) {
    if (entryName) return String(entryName);
    const p = prettyId(appId);
    if (p !== "") return p;
    return String(title || "");
}
