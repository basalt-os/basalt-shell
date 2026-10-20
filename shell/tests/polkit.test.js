// Tests of shell/polkit.js: the polkit dialog names a confirmed change in
// plain words, never by its systemd unit. Run with Node's test runner:
// `make test-js` or `node --test shell/tests/`.
"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const shell = path.join(__dirname, "..");
const P = vm.createContext({});
vm.runInContext(fs.readFileSync(path.join(shell, "polkit.js"), "utf8"), P);

const systemd = "org.freedesktop.systemd1.manage-units";

test("a confirmed proposal is named by its title, not by its unit", () => {
  const flow = { actionId: systemd, message: "Authentication is required to start 'basalt-apply@p-727bf8_dd67660a.service'." };
  const d = P.describe(flow, [{ id: "p-000000", title: "other" }, { id: "p-727bf8", title: "run the AI audit suite on this computer" }]);
  assert.equal(d.kind, "apply");
  assert.equal(d.id, "p-727bf8");
  assert.equal(d.title, "run the AI audit suite on this computer");
  // Not in the list (applied from the command bar): the id only.
  const e = P.describe(flow, []);
  assert.equal(e.kind, "apply");
  assert.equal(e.title, "");
  assert.equal(P.describe(flow, null).id, "p-727bf8");
});

test("the gate's executor is named as an approved request", () => {
  const d = P.describe({ actionId: systemd, message: "Authentication is required to start 'basalt-gate-exec@g-0123456789ab.service'." }, []);
  assert.equal(d.kind, "gate-exec");
  assert.equal(d.id, "g-0123456789ab");
});

test("anything else is shown as polkit words it", () => {
  const gate = { actionId: "org.basalt-os.gate.decide-admin", message: "Authentication is required to approve a system change" };
  assert.equal(P.describe(gate, []).kind, "other");
  assert.equal(P.describe(gate, []).message, gate.message);
  // Another unit, or the same text from another action: not rewritten.
  assert.equal(P.describe({ actionId: systemd, message: "Authentication is required to start 'nginx.service'." }, []).kind, "other");
  assert.equal(P.describe({ actionId: "org.example.x", message: "start 'basalt-apply@p-727bf8_dd67660a.service'" }, []).kind, "other");
  assert.equal(P.describe(null, []).kind, "none");
});
