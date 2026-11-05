// Tests of the login conversation's state machine (greeter/greetd.js,
// used by GreetdIpc.qml), run with Node's test runner: `make
// test-greeter`. Each test plays greetd 0.10's answers, quirks included.
"use strict";
const test = require("node:test");
const assert = require("node:assert/strict");
const fs = require("node:fs");
const path = require("node:path");
const vm = require("node:vm");

const G = vm.createContext({});
vm.runInContext(fs.readFileSync(path.join(__dirname, "..", "greetd.js"), "utf8"), G);
const plain = (v) => JSON.parse(JSON.stringify(v));
const sends = (fx) => plain(fx).filter((e) => e.send).map((e) => e.send.type);
const emits = (fx) => plain(fx).filter((e) => e.emit).map((e) => e.emit);
const ready = () => { const s = G.newState(); G.connected(s); return s; };

test("a password login: create, answer, start, quit", () => {
  const s = ready();
  assert.deepEqual(sends(G.createSession(s, "ana")), ["create_session"]);
  assert.equal(s.state, "authenticating");
  let fx = G.answer(s, { type: "auth_message", auth_message_type: "secret", auth_message: "Password: " });
  assert.deepEqual(emits(fx), ["authMessage"]);
  assert.deepEqual(plain(fx)[0].args, ["Password: ", false, true, false]);
  assert.deepEqual(sends(G.respond(s, "pw")), ["post_auth_message_response"]);
  assert.deepEqual(sends(G.respond(s, "again")), [], "a second answer without a question is not sent");
  assert.deepEqual(emits(G.answer(s, { type: "success" })), ["readyToLaunch"]);
  assert.equal(s.state, "ready");
  assert.deepEqual(sends(G.launch(s, ["basalt-session", "sway"], ["LANG=pt_BR.UTF-8"], true)), ["start_session"]);
  fx = plain(G.answer(s, { type: "success" }));
  assert.deepEqual(emits(fx), ["launched"]);
  assert.ok(fx.some((e) => e.quit));
  assert.equal(s.state, "launched");
});

test("requests made before the helper is connected wait for ipc_ready", () => {
  const s = G.newState();
  assert.deepEqual(sends(G.createSession(s, "ana")), []);
  assert.deepEqual(sends(G.answer(s, { type: "ipc_ready" })), ["create_session"]);
});

test("a refused password: cancel sent, authFailure, greetd's error for the cancel dropped", () => {
  const s = ready();
  G.createSession(s, "ana");
  G.answer(s, { type: "auth_message", auth_message_type: "secret", auth_message: "Password: " });
  G.respond(s, "wrong");
  const fx = G.answer(s, { type: "error", error_type: "auth_error", description: "pam_authenticate: AUTH_ERR" });
  assert.deepEqual(sends(fx), ["cancel_session"]);
  assert.deepEqual(emits(fx), ["authFailure"]);
  assert.equal(s.state, "inactive");
  // greetd 0.10 answers that cancel with an error: not news.
  assert.deepEqual(plain(G.answer(s, { type: "error", error_type: "error", description: "unable to send message" })), []);
  // A new conversation works.
  assert.deepEqual(sends(G.createSession(s, "ana")), ["create_session"]);
  assert.deepEqual(emits(G.answer(s, { type: "auth_message", auth_message_type: "secret", auth_message: "Password: " })), ["authMessage"]);
});

test("answers to requests made before a cancel are dropped", () => {
  const s = ready();
  G.createSession(s, "ana");
  G.cancelSession(s);
  G.createSession(s, "bia");
  // The late answer to the first create_session, the cancel's, then bia's.
  assert.deepEqual(plain(G.answer(s, { type: "auth_message", auth_message_type: "secret", auth_message: "old" })), []);
  assert.deepEqual(plain(G.answer(s, { type: "success" })), []);
  const fx = plain(G.answer(s, { type: "auth_message", auth_message_type: "secret", auth_message: "Password: " }));
  assert.deepEqual(emits(fx), ["authMessage"]);
  assert.equal(s.user, "bia");
});

test("a notice is acknowledged without an answer", () => {
  const s = ready();
  G.createSession(s, "ana");
  const fx = G.answer(s, { type: "auth_message", auth_message_type: "info", auth_message: "Your password expires in 3 days" });
  assert.deepEqual(emits(fx), ["authMessage"]);
  assert.deepEqual(plain(fx)[1].send, { type: "post_auth_message_response" });
  assert.equal(s.responseRequired, false);
});

test("a conversation left open by an earlier greeter is cancelled and started again", () => {
  const s = ready();
  G.createSession(s, "ana");
  const fx = G.answer(s, { type: "error", error_type: "error", description: "a session is already being configured" });
  assert.deepEqual(sends(fx), ["cancel_session", "create_session"]);
  assert.deepEqual(emits(fx), []);
  assert.equal(s.state, "authenticating");
});

test("other errors and a lost connection are reported; reset starts clean", () => {
  let s = ready();
  G.createSession(s, "ana");
  assert.deepEqual(emits(G.answer(s, { type: "error", error_type: "error", description: "boom" })), ["error"]);
  s = ready();
  G.createSession(s, "ana");
  assert.deepEqual(emits(G.disconnected(s, false)), ["error"]);
  assert.equal(s.state, "inactive");
  s = ready();
  G.createSession(s, "ana");
  G.reset(s);
  assert.deepEqual(emits(G.disconnected(s, true)), [], "a reset asked for is not an error");
  assert.deepEqual(sends(G.createSession(s, "ana")), [], "queued until the new connection is ready");
  assert.deepEqual(sends(G.answer(s, { type: "ipc_ready" })), ["create_session"]);
  assert.equal(s.inflight.length, 1, "nothing of the old connection is waited for");
});
