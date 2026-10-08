import QtQuick
import Quickshell
import Quickshell.Wayland
import "apps.js" as A

// The power menu: Lock screen, Log out, Suspend, Restart, Power off.
// Opened by the power button at the right end of the panel, the one in
// quick settings, or Super+Shift+E. Keyboard: Up and Down move (Home,
// End, and the first letter too), Return chooses, Escape closes (or
// cancels a countdown) and returns to the button that opened it.
//
// Log out, Restart and Power off ask first: the card counts down 60
// seconds and then goes ahead by itself (as GNOME does), with the button
// to do it now and Cancel; it lists the open app windows so the person
// saves their work first. The countdown opens focused on Cancel, so a
// second Return never ends the session at once (Left reaches the button
// to do it now). Lock and Suspend run at once.
//
// The same countdown restarts into a staged offline update (Settings,
// Updates and channels, "Restarting to install updates"): when it ends, or
// with Restart now, the daemon starts the assistant's
// basalt-offline-reboot.service; Cancel keeps the update staged.
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
    // Open app windows: their work may be lost. Named as the person
    // knows them: the desktop entry's name in their language (Mousepad,
    // not org.xfce.mousepad), else a readable form of the app id, else
    // the window title.
    readonly property var openApps: {
        const seen = {};
        const out = [];
        for (const w of (Bus.desktop.windows || [])) {
            // The shell's own windows (Settings) hold no unsaved work.
            if (w.app_id === "org.quickshell") continue;
            const entry = w.app_id ? DesktopEntries.heuristicLookup(w.app_id) : null;
            const n = A.appLabel(w.app_id || "", entry ? entry.name : "", w.title || "");
            if (n && !seen[n]) { seen[n] = true; out.push(n); }
        }
        return out;
    }

    onVisibleChanged: {
        if (visible) {
            confirming = ""; error = "";
            if (Ui.powerStart !== "") { const op = Ui.powerStart; Ui.powerStart = ""; choose(op); }
            else focusTimer.restart();
        }
        else { tick.stop(); confirming = ""; }
    }
    // Focus the first entry once the surface has the keyboard.
    Timer { id: focusTimer; interval: 30; onTriggered: Nav.initial(menu, "") }

    function close() { tick.stop(); confirming = ""; Ui.powerMenu = false; }
    // Escape: close, and put the focus back on the button that opened it.
    function dismiss() { tick.stop(); confirming = ""; Ui.dismiss(); }
    function choose(op) {
        error = "";
        if (op === "logout" || op === "restart" || op === "poweroff" || op === "update") {
            confirming = op; left = 60; tick.restart();
            nowTimer.restart();
            return;
        }
        run(op);
    }
    function run(op) {
        tick.stop();
        if (op === "update") {
            Bus.call("updates.restart", {}, (ok, res) => {
                if (ok) { win.close(); return; }
                win.confirming = ""; win.error = String(res);
                focusTimer.restart();
            });
            return;
        }
        Bus.act("session.power", { op: op }, (ok, res) => {
            if (ok) { win.close(); return; }
            win.confirming = ""; win.error = String(res);
            focusTimer.restart();
        });
    }
    // The confirmation opens on Cancel: a stray Return never confirms.
    Timer { id: nowTimer; interval: 30; onTriggered: cancelBtn.forceActiveFocus(Qt.TabFocusReason) }
    Timer {
        id: tick
        interval: 1000; repeat: true
        onTriggered: {
            win.left -= 1;
            if (win.left <= 0) win.run(win.confirming);
        }
    }

    function title(op) {
        if (op === "update") return Tr.t("Restarting to install updates");
        if (op === "logout") return Tr.t("Log out now?");
        if (op === "restart") return Tr.t("Restart the computer now?");
        return Tr.t("Power off the computer now?");
    }
    function countdown(op, n) {
        if (op === "update") return Tr.t("The computer restarts in %1 s and installs the updates before the desktop starts. Cancel keeps them ready for later.").arg(n);
        if (op === "logout") return Tr.t("You will be logged out in %1 s.").arg(n);
        if (op === "restart") return Tr.t("The computer restarts in %1 s.").arg(n);
        return Tr.t("The computer powers off in %1 s.").arg(n);
    }
    function nowLabel(op) {
        if (op === "update") return Tr.t("Restart now");
        if (op === "logout") return Tr.t("Log out");
        if (op === "restart") return Tr.t("Restart");
        return Tr.t("Power off");
    }

    // A click outside closes the menu (and cancels a countdown).
    MouseArea { anchors.fill: parent; acceptedButtons: Qt.AllButtons; onClicked: win.close() }

    // 8 px from the panel's edge and the side (the layer covers the whole
    // screen, so the panel's zone is counted here).
    Surface {
        id: card
        width: Math.min(360, parent.width - Theme.popoverGap * 2)
        height: col.implicitHeight + Theme.s3 * 2
        anchors.right: parent.right
        anchors.rightMargin: Theme.popoverGap
        y: Theme.panelPosition === "bottom" ? parent.height - height - Theme.panelZone - Theme.popoverGap : Theme.panelZone + Theme.popoverGap
        opacity: win.visible ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: Theme.fast } }
        MouseArea { anchors.fill: parent }
        Keys.onEscapePressed: win.dismiss()

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
            NavColumn {
                id: menu
                width: parent.width
                spacing: Theme.s1
                typeAhead: true
                accessibleRole: Accessible.PopupMenu
                Accessible.name: Tr.t("Power")
                Repeater {
                    model: win.confirming === "" ? win.entries : []
                    delegate: Btn {
                        required property var modelData
                        width: col.width
                        alignLeft: true
                        accessibleRole: Accessible.MenuItem
                        icon: modelData.icon
                        text: modelData.label
                        e2e: "power-" + modelData.op
                        onClicked: win.choose(modelData.op)
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
                    e2e: "power-confirm"
                    onClicked: win.run(win.confirming)
                    KeyNavigation.right: cancelBtn
                }
                Btn {
                    id: cancelBtn
                    text: Tr.t("Cancel")
                    variant: "outline"
                    e2e: "power-cancel"
                    onClicked: win.dismiss()
                    KeyNavigation.left: nowBtn
                }
            }
        }
    }
}
