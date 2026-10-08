// Tests of shell/apps.js: the launcher's ranking and the names of app
// windows (the power menu's countdown). Run with Node's test runner:
// `make test-js` or `node --test shell/tests/`.
"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const shell = path.join(__dirname, "..");
const A = vm.createContext({});
vm.runInContext(fs.readFileSync(path.join(shell, "apps.js"), "utf8"), A);

// What the daemon's "apps" call returns for these entries (an English
// session; kinds come from the categories).
const apps = [
  { id: "Alacritty", name: "Alacritty", generic: "Terminal", comment: "A fast, cross-platform, OpenGL terminal emulator",
    kinds: ["terminal", "console", "command line", "terminal emulator", "linha de comando"] },
  { id: "foot", name: "Foot", generic: "Terminal", comment: "A wayland native terminal emulator",
    keywords: ["shell", "prompt", "command", "commandline"], kinds: ["terminal", "console", "command line", "terminal emulator", "linha de comando"] },
  { id: "org.xfce.mousepad", name: "Mousepad", generic: "Text Editor", comment: "Simple Text Editor",
    keywords: ["text", "editor", "notepad", "gtk"], kinds: ["text editor", "editor", "notepad", "editor de texto", "bloco de notas"] },
  { id: "firefox", name: "Firefox", generic: "Web Browser", comment: "Browse the Web", keywords: ["web", "browser", "internet"],
    kinds: ["browser", "web browser", "internet", "navegador", "navegador web"] },
  { id: "org.keepassxc.KeePassXC", name: "KeePassXC", generic: "Password Manager", comment: "Community-driven port of the Windows application KeePass Password Safe",
    kinds: ["password manager", "passwords", "senhas", "gerenciador de senhas"] },
];
const ids = list => Array.from(list, a => a.id);

test("a kind of app finds that kind first, never a terminal for an editor", () => {
  for (const [q, first] of [
    ["text editor", "org.xfce.mousepad"], ["Text Editor", "org.xfce.mousepad"], ["text", "org.xfce.mousepad"],
    ["editor", "org.xfce.mousepad"], ["editor de texto", "org.xfce.mousepad"], ["notepad", "org.xfce.mousepad"],
    ["terminal", "Alacritty"], ["browser", "firefox"], ["navegador", "firefox"], ["passwords", "org.keepassxc.KeePassXC"],
    ["mousepad", "org.xfce.mousepad"], ["fire", "firefox"], ["foot", "foot"],
  ]) {
    const r = ids(A.rank(apps, q));
    assert.equal(r[0], first, `${q}: ${r.join(", ")}`);
  }
  const r = ids(A.rank(apps, "text editor"));
  assert.ok(!r.includes("Alacritty") && !r.includes("foot"), `terminals matched "text editor": ${r.join(", ")}`);
});

test("letters in order still find an app, below every word match", () => {
  assert.equal(ids(A.rank(apps, "kpxc"))[0], "org.keepassxc.KeePassXC");
  // "ed" starts a word of the editor; a fuzzy hit never outranks that.
  assert.equal(ids(A.rank(apps, "ed"))[0], "org.xfce.mousepad");
  assert.ok(A.score(apps[3], "frfx") < 200, "a fuzzy match stays below the word tiers");
});

test("an empty query lists every app by name", () => {
  assert.deepEqual(ids(A.rank(apps, "")), ["Alacritty", "firefox", "foot", "org.keepassxc.KeePassXC", "org.xfce.mousepad"]);
  assert.deepEqual(ids(A.rank(apps, "   ")).length, apps.length);
});

test("accents and case do not matter", () => {
  const pt = [{ id: "org.gnome.Settings", name: "Configurações", generic: "", kinds: ["settings", "configuracoes"] }];
  assert.equal(ids(A.rank(pt, "configuracoes"))[0], "org.gnome.Settings");
  assert.equal(ids(A.rank(pt, "CONFIGURAÇÕES"))[0], "org.gnome.Settings");
});

test("an app window is named as the person knows it", () => {
  // The desktop entry's name (localized by Quickshell) wins.
  assert.equal(A.appLabel("org.xfce.mousepad", "Mousepad", "notes.txt - Mousepad"), "Mousepad");
  // No entry: a readable app id, never the reverse domain name.
  assert.equal(A.appLabel("org.xfce.mousepad", "", "notes.txt"), "Mousepad");
  assert.equal(A.appLabel("org.gnome.TextEditor.desktop", null, ""), "TextEditor");
  assert.equal(A.appLabel("gnome-calculator", "", ""), "Gnome calculator");
  // No app id (some XWayland windows): the title.
  assert.equal(A.appLabel("", "", "IntelliJ IDEA"), "IntelliJ IDEA");
  assert.equal(A.appLabel("", "", ""), "");
});
