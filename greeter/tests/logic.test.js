// Unit tests of the greeter's logic (greeter/logic.js), run with Node's
// test runner: `make test-greeter` or `node --test greeter/tests/`.
// logic.js is a QML JavaScript resource (plain functions, no imports); it
// is loaded here into its own context, the way QML loads it.
"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const L = vm.createContext({});
vm.runInContext(fs.readFileSync(path.join(__dirname, "..", "logic.js"), "utf8"), L);
// Values from the context compare by structure across realms.
const same = (a, b) => assert.deepEqual(JSON.parse(JSON.stringify(a)), b);

const passwd = [
  "root:x:0:0:root:/root:/bin/bash",
  "greetd:x:995:995:greetd daemon:/var/lib/greetd:/usr/sbin/nologin",
  "basalt:x:1000:1000:Basalt desktop:/home/basalt:/bin/bash",
  "ana:x:1001:1001:Ana Souza,,,:/home/ana:/bin/bash",
  "svc:x:1002:1002::/home/svc:/sbin/nologin",
  "zed:x:1003:1003::/home/zed:/usr/bin/zsh",
  "nobody:x:65534:65534:Kernel Overflow User:/:/sbin/nologin",
  "",
  "# a comment"
].join("\n");

test("parsePasswd lists people who can log in, sorted by name", () => {
  const users = L.parsePasswd(passwd, L.parseLoginDefs("UID_MIN 1000\nUID_MAX 60000\n"), []);
  same(users.map(u => u.name), ["ana", "basalt", "zed"]);
  assert.equal(users[0].realName, "Ana Souza");
  assert.equal(L.displayName(users[2]), "zed");
});

test("parsePasswd hides configured users and honors login.defs", () => {
  same(L.parsePasswd(passwd, { uidMin: 1000, uidMax: 60000 }, ["zed"]).map(u => u.name), ["ana", "basalt"]);
  same(L.parsePasswd(passwd, L.parseLoginDefs("# none\nUID_MIN\t1001\n"), []).map(u => u.name), ["ana", "zed"]);
  same(L.parseLoginDefs(""), { uidMin: 1000, uidMax: 60000 });
});

test("initials by code point", () => {
  assert.equal(L.initials("Ana Souza"), "AS");
  assert.equal(L.initials("edimar"), "E");
  assert.equal(L.initials("  élise   du  pont "), "ÉP");
  assert.equal(L.initials(""), "?");
});

test("parseKeyValue reads os-release style files", () => {
  const kv = L.parseKeyValue('# c\nNAME="Basalt OS"\nLANG=pt_BR.UTF-8\nTHEME=\'tide\'\n bad line\nEMPTY=\n');
  assert.equal(kv.NAME, "Basalt OS");
  assert.equal(kv.LANG, "pt_BR.UTF-8");
  assert.equal(kv.THEME, "tide");
  assert.equal(kv.EMPTY, "");
});

const swayEntry = "[Desktop Entry]\nName=Basalt\nName[pt_BR]=Basalt (Sway)\nComment=Basalt shell on sway\nExec=basalt-session sway\nType=Application\nDesktopNames=sway;Basalt\n[Desktop Action x]\nName=Other\n";

test("parseDesktopEntry: group, localized name, DesktopNames", () => {
  const e = L.parseDesktopEntry(swayEntry, "pt_BR");
  assert.equal(e.name, "Basalt (Sway)");
  assert.equal(L.parseDesktopEntry(swayEntry, "en").name, "Basalt");
  assert.equal(L.parseDesktopEntry(swayEntry, "pt").name, "Basalt");
  same(e.desktopNames, ["sway", "Basalt"]);
  assert.equal(L.parseDesktopEntry("[Desktop Entry]\nName=X\nExec=x\nNoDisplay=true\n", "").hidden, true);
});

test("splitExec follows the Desktop Entry quoting rules", () => {
  same(L.splitExec("basalt-session sway"), ["basalt-session", "sway"]);
  same(L.splitExec('sh -c "exec \\"$0\\" --x" %U'), ["sh", "-c", 'exec "$0" --x']);
  same(L.splitExec('run ""  100%%'), ["run", "", "100%"]);
  same(L.splitExec(""), []);
});

test("makeSession: command, environment, known names", () => {
  const s = L.makeSession("basalt-sway.desktop", L.parseDesktopEntry(swayEntry, "en"));
  same(s.command, ["basalt-session", "sway"]);
  same(s.env, ["XDG_SESSION_TYPE=wayland", "XDG_SESSION_DESKTOP=sway", "XDG_CURRENT_DESKTOP=sway:Basalt"]);
  assert.equal(s.known, "Basalt on Sway");
  assert.equal(L.makeSession("x.desktop", L.parseDesktopEntry("[Desktop Entry]\nName=X\n", "")), null);
  assert.equal(L.makeSession("y.desktop", L.parseDesktopEntry("[Desktop Entry]\nName=Y\nExec=y\nHidden=true\n", "")), null);
});

test("sessions: Basalt first, the remembered one wins when installed", () => {
  const mk = (f, n) => L.makeSession(f, L.parseDesktopEntry("[Desktop Entry]\nName=" + n + "\nExec=" + n + "\n", ""));
  const list = L.sortSessions([mk("sway.desktop", "Sway"), mk("basalt-niri.desktop", "Basalt (niri)"), mk("basalt-sway.desktop", "Basalt"), mk("a.desktop", "Alpha")]);
  same(list.map(s => s.file), ["basalt-sway.desktop", "basalt-niri.desktop", "a.desktop", "sway.desktop"]);
  assert.equal(L.pickSession(list, "basalt-niri.desktop", "").file, "basalt-niri.desktop");
  assert.equal(L.pickSession(list, "gone.desktop", "").file, "basalt-sway.desktop");
  assert.equal(L.pickSession(list, "", "sway.desktop").file, "sway.desktop");
  assert.equal(L.pickSession([], "x", "y"), null);
});

test("pickUser: remembered, single, or nobody", () => {
  const users = [{ name: "ana" }, { name: "basalt" }];
  assert.equal(L.pickUser(users, "basalt").name, "basalt");
  assert.equal(L.pickUser(users, ""), null);
  assert.equal(L.pickUser(users, "gone"), null);
  assert.equal(L.pickUser([{ name: "solo" }], "").name, "solo");
  assert.equal(L.pickUser([], "x"), null);
});

test("uiLanguage picks a catalog or English", () => {
  const have = ["en", "pt_BR"];
  assert.equal(L.uiLanguage("pt_BR.UTF-8", have), "pt_BR");
  assert.equal(L.uiLanguage("pt_PT.UTF-8", have), "pt_BR");
  assert.equal(L.uiLanguage("de_DE.UTF-8", have), "en");
  assert.equal(L.uiLanguage("C.UTF-8", have), "en");
  assert.equal(L.uiLanguage("", have), "en");
});

test("promptKind and failureKind", () => {
  assert.equal(L.promptKind("Password: ", false), "password");
  assert.equal(L.promptKind("Senha:", false), "password");
  assert.equal(L.promptKind("Verification code: ", false), "secret");
  assert.equal(L.promptKind("login:", true), "visible");
  assert.equal(L.failureKind("pam_authenticate: AUTH_ERR"), "wrong");
  assert.equal(L.failureKind("The account is locked due to 3 failed logins."), "locked");
  assert.equal(L.failureKind("Your account has expired; please contact your system administrator."), "expired");
});

test("Caps Lock from letters and from the LEDs", () => {
  assert.equal(L.capsFromKey("A", false), true);
  assert.equal(L.capsFromKey("a", false), false);
  assert.equal(L.capsFromKey("a", true), true);
  assert.equal(L.capsFromKey("A", true), false);
  assert.equal(L.capsFromKey("1", false), null);
  assert.equal(L.capsFromKey("", false), null);
  assert.equal(L.capsFromLeds("0\n1\n"), true);
  assert.equal(L.capsFromLeds("0\n0\n"), false);
  assert.equal(L.capsFromLeds(""), null);
});

test("state: never more than the known fields, survives garbage", () => {
  same(L.parseState("not json"), { lastUser: "", sessions: {}, largeText: false, highContrast: false, language: "" });
  const s = L.remember(L.parseState('{"lastUser":"x","password":"leak","largeText":true}'), "ana", "basalt-niri.desktop");
  assert.equal(s.lastUser, "ana");
  assert.equal(s.sessions.ana, "basalt-niri.desktop");
  assert.equal(s.largeText, true);
  assert.equal("password" in JSON.parse(L.serializeState(s)), false);
});

test("network and keyboard status", () => {
  same(L.parseNmcli("ethernet:connected:Wired 1\nwifi:connected:Casa:5G\nloopback:connected (externally):lo\n"), { kind: "wifi", name: "Casa:5G" });
  same(L.parseNmcli("ethernet:unavailable:\n"), { kind: "none", name: "" });
  const inputs = L.parseInputs(JSON.stringify([{ type: "pointer" }, { type: "keyboard", xkb_layout_names: ["Portuguese (Brazil)", "English (US)"], xkb_active_layout_index: 1 }]), "br,us");
  assert.equal(L.layoutCode(inputs), "US");
  assert.equal(L.layoutCode(L.parseInputs("[]", "br")), "BR");
  assert.equal(L.layoutCode(L.parseInputs("garbage", "")), "");
});
