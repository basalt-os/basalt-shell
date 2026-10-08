// Tests of shell/updates.js: which channels can be turned on here, and
// the one line a person reads when a change did not work. Run with
// Node's test runner: `make test-js` or `node --test shell/tests/`.
"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const shell = path.join(__dirname, "..");
const U = vm.createContext({});
vm.runInContext(fs.readFileSync(path.join(shell, "updates.js"), "utf8"), U);

test("a driver channel is not available until basalt-nonfree-release is published", () => {
  const testing = { id: "basalt-nonfree-testing", defined: false, enabled: false, toggle: true, testing: true, published: true };
  // The owner's VM (shell 0.9.1): the package was not in any repository.
  assert.equal(U.channelAvailable(testing, {}, { state: { release_package: false, nonfree_available: false } }), false);
  // No drivers report (not read yet, or an older assistant): not available.
  assert.equal(U.channelAvailable(testing, {}, null), false);
  assert.equal(U.channelAvailable(testing, {}, { coming_soon: true }), false);
  // Offered by the repositories (dnf's cached metadata), or installed.
  assert.equal(U.channelAvailable(testing, {}, { state: { nonfree_available: true } }), true);
  assert.equal(U.channelAvailable(testing, {}, { state: { release_package: true } }), true);
  // Already defined: available; already on: always (it can be turned off).
  assert.equal(U.channelAvailable(Object.assign({}, testing, { defined: true }), {}, null), true);
  assert.equal(U.channelAvailable(Object.assign({}, testing, { enabled: true }), {}, null), true);
  // Channels defined by basalt-release are always available.
  assert.equal(U.channelAvailable({ id: "basalt-testing", defined: true }, {}, null), true);
  assert.equal(U.channelAvailable({ id: "basalt-tools", defined: false }, {}, null), true);
});

test("a driver channel is not available until its own repository is published", () => {
  // The owner's VM (shell 0.10.0): basalt-nonfree-release 1-3 installed
  // from basalt-testing, so basalt-nonfree-testing is defined, but nothing
  // is published at its URL (repomd.xml answers 404).
  const defined = { id: "basalt-nonfree-testing", defined: true, enabled: false, toggle: true, testing: true };
  const drv = { state: { release_package: true, nonfree_available: true } };
  assert.equal(U.channelAvailable(Object.assign({}, defined, { published: false }), {}, drv), false);
  // A daemon without the check (no published field): not available.
  assert.equal(U.channelAvailable(defined, {}, drv), false);
  // Published: available.
  assert.equal(U.channelAvailable(Object.assign({}, defined, { published: true }), {}, drv), true);
  // Not defined and not published: not available, whatever the drivers report says.
  assert.equal(U.channelAvailable({ id: "basalt-nonfree-testing", defined: false, published: false }, {}, drv), false);
  // On but unpublished: it can still be turned off.
  assert.equal(U.channelAvailable(Object.assign({}, defined, { enabled: true, published: false }), {}, null), true);
});

test("a failed change gets a one line reason, never the raw dnf text", () => {
  const report = [
    "Turn on basalt-nonfree-testing",
    "$ dnf -y install basalt-nonfree-release",
    "Updating and loading repositories:",
    "Repositories loaded.",
    "Failed to resolve the transaction:",
    "No match for argument: basalt-nonfree-release",
    "That command failed (exit code 1), so the remaining ones were not run.",
  ].join("\n");
  assert.equal(U.failureKind(report), "unpublished");
  assert.equal(U.failureKind("Curl error (6): Could not resolve host: obpkg.org"), "network");
  assert.equal(U.failureKind("Failed to download metadata for repo 'basalt'"), "network");
  assert.equal(U.failureKind("GPG check FAILED"), "signature");
  assert.equal(U.failureKind("Error: No space left on device"), "space");
  assert.equal(U.failureKind("Error executing command as another user: Request dismissed"), "refused");
  assert.equal(U.failureKind("something else entirely"), "");
  assert.equal(U.failureKind(null), "");
});

test("an error's first line, cut at a word", () => {
  assert.equal(U.firstLine("\n  first line  \nsecond"), "first line");
  const long = "word ".repeat(60);
  const s = U.firstLine(long, 40);
  assert.ok(s.length <= 40 && !s.endsWith(" ") && s.startsWith("word"), s);
  assert.equal(U.firstLine(""), "");
});
