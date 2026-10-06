import QtQuick
import Quickshell
import Quickshell.Wayland

// The power menu: Lock screen, Log out, Suspend, Restart, Power off.
// Opened by the power button at the right end of the panel, the one in
// quick settings, or Super+Shift+E. Keyboard: Up and Down move, Return
// chooses, Escape closes (or cancels a countdown).
//
// Log out, Restart and Power off ask first: the card counts down 60
// seconds and then goes ahead by itself (as GNOME does), with the button
// to do it now and Cancel; it lists the open app windows so the person
// saves their work first. Lock and Suspend run at once.
//
// Each entry is the typed action session.power, run directly because the
// person chose it here (the daemon runs it through logind with the
// system's polkit rules). An agent can never run or propose it.
PanelWindow {
    id: win
    visible: Ui.powerMenu && !Ui.modal
    anchors { top: true; bottom: true; left: true; right: true }
    color: "transparent"
    exclusionMode: ExclusionMode.Ignore
    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.namespace: "basalt-power-menu"
    WlrLayershell.keyboardFocus: visible ? WlrKeyboardFocus.Exclusive : WlrKeyboardFocus.None

    // "" (the menu) or the operation waiting for its countdown.
    property string confirming: ""
    property int left: 60
    property string error: ""

    readonly property var entries: [
        { op: "lock", label: Tr.t("Lock screen"), icon: "lock", key: "Super+L" },
        { op: "logout", label: Tr.t("Log out"), icon: "logout", key: "" },
        { op: "suspend", label: Tr.t("Suspend"), icon: "moon", key: "" },
        { op: "restart", label: Tr.t("Restart"), icon: "restart", key: "" },
        { op: "poweroff", label: Tr.t("Power off"), icon: "power", key: "" }
    ]
    // Open app windows: their work may be lost.
    readonly property var openApps: {
        const seen = {};
        const out = [];
        for (const w of (Bus.desktop.windows || [])) {
            const n = w.app_id || w.title || "";
            if (n && !seen[n]) { seen[n] = true; out.push(n); }
        }
        return out;
    }

    onVisibleChanged: {
        if (visible) { confirming = ""; error = ""; focusTimer.restart(); }
        else { tick.stop(); confirming = ""; }
    }
    // Focus the first entry once the surface has the keyboard.
    Timer { id: focusTimer; interval: 30; onTriggered: if (list.count > 0) list.itemAt(0).forceActiveFocus() }

    function close() { tick.stop(); confirming = ""; Ui.powerMenu = false; }
    function choose(op) {
        error = "";
        if (op === "logout" || op === "restart" || op === "poweroff") {
            confirming = op; left = 60; tick.restart();
            nowTimer.restart();
            return;
        }
        run(op);
    }
    function run(op) {
        tick.stop();
        Bus.act("session.power", { op: op }, (ok, res) => {
            if (ok) { win.close(); return; }
            win.confirming = ""; win.error = String(res);
            focusTimer.restart();
        });
    }
    Timer { id: nowTimer; interval: 30; onTriggered: nowBtn.forceActiveFocus() }
    Timer {
        id: tick
        interval: 1000; repeat: true
        onTriggered: {
            win.left -= 1;
            if (win.left <= 0) win.run(win.confirming);
        }
    }

    function title(op) {
        if (op === "logout") return Tr.t("Log out now?");
        if (op === "restart") return Tr.t("Restart the computer now?");
        return Tr.t("Power off the computer now?");
    }
    function countdown(op, n) {
        if (op === "logout") return Tr.t("You will be logged out in %1 s.").arg(n);
        if (op === "restart") return Tr.t("The computer restarts in %1 s.").arg(n);
        return Tr.t("The computer powers off in %1 s.").arg(n);
    }
    function nowLabel(op) {
        if (op === "logout") return Tr.t("Log out");
        if (op === "restart") return Tr.t("Restart");
        return Tr.t("Power off");
    }

    // A click outside closes the menu (and cancels a countdown).
    MouseArea { anchors.fill: parent; acceptedButtons: Qt.AllButtons; onClicked: win.close() }

    Surface {
        id: card
        width: 300
        height: col.implicitHeight + Theme.s3 * 2
        anchors.right: parent.right
        anchors.rightMargin: Theme.s2
        y: Theme.panelPosition === "bottom" ? parent.height - height - Theme.panelHeight - Theme.s2 : Theme.panelHeight + Theme.s2
        opacity: win.visible ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: Theme.fast } }
        MouseArea { anchors.fill: parent }
        Keys.onEscapePressed: win.close()

        Column {
            id: col
            anchors.left: parent.left
            anchors.right: parent.right
            anchors.top: parent.top
            anchors.margins: Theme.s3
            spacing: Theme.s1

            Txt {
                visible: win.error !== ""
                width: parent.width
                wrapMode: Text.Wrap
                role: "small"
                color: Theme.danger
                text: win.error
            }

            // The menu.
            Repeater {
                id: list
                model: win.confirming === "" ? win.entries : []
                delegate: Btn {
                    required property var modelData
                    required property int index
                    width: col.width
                    alignLeft: true
                    focusable: true
                    icon: modelData.icon
                    text: modelData.label
                    e2e: "power-" + modelData.op
                    onClicked: win.choose(modelData.op)
                    Keys.onUpPressed: { const p = list.itemAt((index + list.count - 1) % list.count); if (p) p.forceActiveFocus(); }
                    Keys.onDownPressed: { const n = list.itemAt((index + 1) % list.count); if (n) n.forceActiveFocus(); }
                    Keys.onEscapePressed: win.close()
                    Txt {
                        visible: modelData.key !== ""
                        anchors.right: parent.right
                        anchors.rightMargin: Theme.s3
                        anchors.verticalCenter: parent.verticalCenter
                        text: modelData.key
                        role: "small"
                        color: Theme.textMuted
                    }
                }
            }

            // The confirmation with its countdown.
            Txt {
                visible: win.confirming !== ""
                width: parent.width
                wrapMode: Text.Wrap
                font.weight: Font.DemiBold
                text: win.confirming !== "" ? win.title(win.confirming) : ""
            }
            Txt {
                visible: win.confirming !== ""
                width: parent.width
                wrapMode: Text.Wrap
                color: Theme.textMuted
                text: win.confirming !== "" ? win.countdown(win.confirming, win.left) : ""
            }
            Txt {
                visible: win.confirming !== "" && win.openApps.length > 0
                width: parent.width
                wrapMode: Text.Wrap
                role: "small"
                color: Theme.warning
                text: Tr.t("Open apps: %1. Save your work first: unsaved changes in them may be lost.").arg(win.openApps.join(", "))
            }
            Row {
                visible: win.confirming !== ""
                spacing: Theme.s2
                topPadding: Theme.s2
                Btn {
                    id: nowBtn
                    text: win.confirming !== "" ? win.nowLabel(win.confirming) : ""
                    variant: win.confirming === "logout" ? "primary" : "danger"
                    focusable: true
                    e2e: "power-confirm"
                    onClicked: win.run(win.confirming)
                    KeyNavigation.right: cancelBtn
                    Keys.onEscapePressed: win.close()
                }
                Btn {
                    id: cancelBtn
                    text: Tr.t("Cancel")
                    variant: "outline"
                    focusable: true
                    e2e: "power-cancel"
                    onClicked: win.close()
                    KeyNavigation.left: nowBtn
                    Keys.onEscapePressed: win.close()
                }
            }
        }
    }
}
