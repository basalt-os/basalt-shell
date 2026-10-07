// Tests of the lock screen: its logic (shell/lock.js) and the rules of
// Lock.qml that keep the session locked until PAM says yes. Run with
// Node's test runner: `make test-js` or `node --test shell/tests/`.
"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const shell = path.join(__dirname, "..");
const L = vm.createContext({});
vm.runInContext(fs.readFileSync(path.join(shell, "lock.js"), "utf8"), L);
const same = (a, b) => assert.deepEqual(JSON.parse(JSON.stringify(a)), b);

// Quickshell's PamResult values, as a plain object.
const PamResult = { Success: 0, Failed: 1, Error: 2, MaxTries: 3 };

test("only PAM's success for an answer the person sent unlocks", () => {
  for (const [r, phase, want] of [
    ["success", "checking", true],
    ["success", "idle", false],          // nobody answered: a success out of nowhere
    ["success", "unlocking", false],
    ["failed", "checking", false],
    ["error", "checking", false],
    ["maxtries", "checking", false],
    ["unknown", "checking", false],
    ["", "checking", false],
    [undefined, "checking", false],
  ]) assert.equal(L.mayUnlock(r, phase), want, `${r} in ${phase}`);
});

test("pamResultName maps Quickshell's results", () => {
  assert.equal(L.pamResultName(PamResult.Success, PamResult), "success");
  assert.equal(L.pamResultName(PamResult.Failed, PamResult), "failed");
  assert.equal(L.pamResultName(PamResult.Error, PamResult), "error");
  assert.equal(L.pamResultName(PamResult.MaxTries, PamResult), "maxtries");
  assert.equal(L.pamResultName(42, PamResult), "unknown");
  // Only a real success unlocks: an unknown result never maps to success.
  assert.equal(L.mayUnlock(L.pamResultName(42, PamResult), "checking"), false);
});

test("failureMessage points at Caps Lock and the layout after three tries", () => {
  assert.equal(L.failureMessage("failed", 1), "wrong");
  assert.equal(L.failureMessage("failed", 2), "wrong");
  assert.equal(L.failureMessage("failed", 3), "wrong-hint");
  assert.equal(L.failureMessage("maxtries", 1), "maxtries");
  assert.equal(L.failureMessage("error", 5), "error");
});

test("promptKind tells the password from other questions and information", () => {
  assert.equal(L.promptKind("Password: ", true, false), "password");
  assert.equal(L.promptKind("Senha:", true, false), "password");
  assert.equal(L.promptKind("", true, false), "password");
  assert.equal(L.promptKind("Verification code:", true, false), "secret");
  assert.equal(L.promptKind("User name:", true, true), "visible");
  assert.equal(L.promptKind("Place your finger on the fingerprint reader", false, false), "info");
});

test("userName, realName and initials", () => {
  const passwd = "root:x:0:0:root:/root:/bin/bash\nana:x:1001:1001:Ana Souza,,,:/home/ana:/bin/bash\nzed:x:1003:1003::/home/zed:/bin/zsh\n";
  assert.equal(L.userName("ana", passwd, "/home/zed"), "ana");
  assert.equal(L.userName("", passwd, "/home/zed"), "zed");
  assert.equal(L.userName("", passwd, "/nowhere"), "");
  assert.equal(L.userName("", passwd, ""), "");
  assert.equal(L.realName(passwd, "ana"), "Ana Souza");
  assert.equal(L.realName(passwd, "zed"), "");
  assert.equal(L.realName(passwd, "nobody"), "");
  assert.equal(L.initials("Ana Souza"), "AS");
  assert.equal(L.initials("basalt"), "B");
  assert.equal(L.initials(""), "?");
});

test("Caps Lock from letters and LEDs", () => {
  assert.equal(L.capsFromKey("A", false), true);
  assert.equal(L.capsFromKey("a", false), false);
  assert.equal(L.capsFromKey("a", true), true);
  assert.equal(L.capsFromKey("1", false), null);
  assert.equal(L.capsFromLeds(""), null);
  assert.equal(L.capsFromLeds("0\n0\n"), false);
  assert.equal(L.capsFromLeds("0\n1\n"), true);
});

test("keyboard layouts from sway and niri", () => {
  const sway = JSON.stringify([
    { type: "keyboard", xkb_layout_names: [null], xkb_active_layout_index: 0 },
    { type: "pointer" },
    { type: "keyboard", xkb_layout_names: ["Portuguese (Brazil)", "English (US)"], xkb_active_layout_index: 1 },
  ]);
  const s = L.layoutFromSway(sway);
  same(s, { names: ["Portuguese (Brazil)", "English (US)"], active: 1 });
  assert.equal(L.layoutLabel(s, "br,us"), "US");
  assert.equal(L.layoutLabel(s, ""), "EN");
  const n = L.layoutFromNiri(JSON.stringify({ names: ["English (US)"], current_idx: 0 }));
  assert.equal(L.layoutLabel(n, "us"), "US");
  same(L.layoutFromSway("not json"), { names: [], active: 0 });
  assert.equal(L.layoutLabel(L.layoutFromNiri(""), "us"), "");
});

test("keyboard layouts from the shell daemon (the Keyboard page's state)", () => {
  const live = L.layoutFromIndicator({ labels: ["BR", "US"], current: 1, live: true, names: ["Portuguese (Brazil)", "English (US)"] });
  same(live, { names: ["Portuguese (Brazil)", "English (US)"], active: 1, labels: ["BR", "US"] });
  // The person's labels win over the system's codes (XKB_DEFAULT_LAYOUT).
  assert.equal(L.layoutLabel(live, "us"), "US");
  assert.equal(L.layoutLabel(L.layoutFromIndicator({ labels: ["BR", "US1", "US2"], current: 2, live: false, names: [] }), "us"), "US2");
  same(L.layoutFromIndicator({ labels: [], current: 0 }), { names: [], active: 0, labels: [] });
  same(L.layoutFromIndicator(null), { names: [], active: 0, labels: [] });
  assert.equal(L.layoutFromIndicator({ labels: ["BR"], current: 7 }).active, 0);
});

// ------------------------------------------------------------ Lock.qml rules

const qml = fs.readFileSync(path.join(shell, "Lock.qml"), "utf8");
// The code without comments (rules are about code, not words).
const code = qml.split("\n").map(l => l.replace(/^\s*\/\/.*$/, "")).join("\n");

// body returns the text of the block that starts at the first match of re.
function body(re) {
  const m = re.exec(code);
  assert.ok(m, `not found: ${re}`);
  let i = code.indexOf("{", m.index + m[0].length - 1), depth = 0;
  for (let j = i; j < code.length; j++) {
    if (code[j] === "{") depth++;
    else if (code[j] === "}" && --depth === 0) return code.slice(i, j + 1);
  }
  throw new Error("unbalanced block");
}

test("the IPC target 'lock' can lock and report, never unlock", () => {
  const ipc = body(/IpcHandler\s*{/);
  assert.match(ipc, /target:\s*"lock"/);
  const fns = [...ipc.matchAll(/function\s+(\w+)\s*\(/g)].map(m => m[1]).sort();
  same(fns, ["lock", "state"]);
  assert.doesNotMatch(ipc, /finishUnlock|locked\s*=\s*false|unlocking|pamDone|respond/);
  // No other IPC target of the shell reaches the lock.
  for (const f of fs.readdirSync(shell).filter(f => f.endsWith(".qml") && f !== "Lock.qml")) {
    const t = fs.readFileSync(path.join(shell, f), "utf8");
    assert.doesNotMatch(t, /finishUnlock|sessionLock|WlSessionLock\b(?!Surface)/, f);
  }
});

test("the session is unlocked in finishUnlock only", () => {
  // Writes that end the lock: the persistent state and the lock object.
  const unlockWrites = [...code.matchAll(/(persist\.locked|sessionLock\.locked)\s*=\s*([^;\n]+)/g)]
    .filter(m => m[2].trim() !== "true");
  assert.equal(unlockWrites.length, 2, JSON.stringify(unlockWrites.map(m => m[0])));
  const fin = body(/function\s+finishUnlock\s*\(\)\s*{/);
  for (const m of unlockWrites) assert.ok(fin.includes(m[0]), `${m[0]} outside finishUnlock`);
  // The lock object follows the persistent state, which starts unlocked
  // only for a shell that was never locked.
  assert.match(code, /WlSessionLock\s*{[^}]*locked:\s*persist\.locked/);
  assert.match(code, /PersistentProperties\s*{[^}]*reloadableId:[^}]*property bool locked: false/);
  // No reload of the shell's files while locked.
  assert.match(code, /Binding\s*{\s*target:\s*Quickshell;\s*property:\s*"watchFiles";\s*value:\s*false;\s*when:\s*persist\.locked\s*}/);
  // "unlocked" goes to the guard's state file from finishUnlock only.
  assert.doesNotMatch(code.replace(body(/function\s+finishUnlock\s*\(\)\s*{/), ""), /writeState\(\)[^\n]*\n?[^\n]*persist\.locked = false/);
});

test("finishUnlock runs only after PAM's success for the person's answer", () => {
  // Called from one place: the timer, guarded by the unlocking phase.
  const calls = [...code.matchAll(/finishUnlock\s*\(\)/g)].length;
  assert.equal(calls, 2, "one definition and one call");
  assert.match(code, /onTriggered:\s*if\s*\(lockCtl\.phase === "unlocking"\)\s*lockCtl\.finishUnlock\(\)/);
  // The unlocking phase is entered in one place, inside L.mayUnlock.
  const sets = [...code.matchAll(/phase\s*=\s*"unlocking"/g)].length;
  assert.equal(sets, 1);
  const done = body(/function\s+pamDone\s*\(result\)\s*{/);
  assert.match(done, /if\s*\(L\.mayUnlock\(result,\s*phase\)\)\s*{\s*phase = "unlocking";\s*unlockTimer\.restart\(\);\s*return;\s*}/);
  // pamDone gets PAM's own result, from the PamContext only.
  const pdCalls = [...code.matchAll(/pamDone\(/g)].length;
  assert.equal(pdCalls, 2, "one definition and one call");
  assert.match(code, /onCompleted:\s*result\s*=>\s*lockCtl\.pamDone\(L\.pamResultName\(result, PamResult\)\)/);
  // The unlock timer is started from pamDone only.
  assert.equal([...code.matchAll(/unlockTimer\.(restart|start)\(\)/g)].length, 1);
  // The checking phase means the person sent an answer: it is set only
  // where submit() or a pending answer reaches PAM.
  const checking = [...code.matchAll(/phase\s*=\s*"checking"/g)].length;
  assert.equal(checking, 3);
});
