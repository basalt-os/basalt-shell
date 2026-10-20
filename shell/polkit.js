// Pure functions of the polkit dialog (PolkitDialog.qml): which request a
// password prompt is about, so the dialog can say it in plain words. The
// desktop applies a confirmed system assistant proposal by starting
// basalt-apply@ID_CODE.service, and polkit's own text for that is
// systemd's "Authentication is required to start
// 'basalt-apply@p-..._....service'": a unit name nobody should have to
// read. No QML or Quickshell types here, so the same file runs in the
// shell (imported as a JavaScript resource) and in the unit tests
// (shell/tests, Node).

const reApplyUnit = /'basalt-apply@(p-[0-9a-f]{6})_[0-9a-f]{8}\.service'/;
const reGateExecUnit = /'basalt-gate-exec@(g-[0-9a-f]{12})\.service'/;

// describe: what a polkit request is about. flow is Quickshell's
// AuthFlow (message, actionId); pending are the system assistant's
// waiting proposals (id, title). Kinds:
// - "apply": a proposal the person confirmed (id, and its title when it
//   is still in the list);
// - "gate-exec": a request approved through the gate (basalt-gated starts
//   that unit and polkit lets it without asking, so this should not
//   appear; if it does, it is still said in plain words);
// - "other": anything else, shown as polkit words it (message, actionId).
function describe(flow, pending) {
    if (!flow) return { kind: "none", id: "", title: "", message: "", actionId: "" };
    const msg = String(flow.message || "");
    const action = String(flow.actionId || "");
    if (action === "org.freedesktop.systemd1.manage-units") {
        let m = reApplyUnit.exec(msg);
        if (m) {
            const list = Array.isArray(pending) ? pending : [];
            const p = list.find(x => x && x.id === m[1]);
            const title = p && p.title ? String(p.title).trim() : "";
            return { kind: "apply", id: m[1], title: title, message: msg, actionId: action };
        }
        m = reGateExecUnit.exec(msg);
        if (m) return { kind: "gate-exec", id: m[1], title: "", message: msg, actionId: action };
    }
    return { kind: "other", id: "", title: "", message: msg, actionId: action };
}
