import QtQuick
import Quickshell
import Quickshell.Wayland

// The window menu: minimize, maximize, snap, float or tile, move to a
// workspace, close. Opened by a right click on a window's title bar
// (compositor title bars; a middle click there closes the window), on its
// entry in the panel's window list, or with Super+Alt+Space. Every entry is a typed action, as from an agent
// or the command bar, but run directly because the person clicked it.
PanelWindow {
    id: win
    readonly property var menu: Ui.windowMenu
    readonly property var target: menu ? Ui.windowById(menu.win) : null
    // The output that holds the menu's position.
    readonly property var output: {
        if (!menu) return null;
        const outs = Bus.desktop.outputs || [];
        return outs.find(o => menu.x >= o.rect.x && menu.x < o.rect.x + o.rect.width
                             && menu.y >= o.rect.y && menu.y < o.rect.y + o.rect.height) || outs[0] || null;
    }
    screen: {
        const name = output ? output.name : "";
        const list = Quickshell.screens;
        for (let i = 0; i < list.length; i++)
            if (list[i].name === name) return list[i];
        return list.length > 0 ? list[0] : null;
    }
    visible: target !== null && !Ui.modal
    anchors { top: true; bottom: true; left: true; right: true }
    color: "transparent"
    exclusionMode: ExclusionMode.Ignore
    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.namespace: "basalt-window-menu"
    WlrLayershell.keyboardFocus: visible ? WlrKeyboardFocus.Exclusive : WlrKeyboardFocus.None

    onTargetChanged: if (menu && !target) Ui.windowMenu = null

    // A click outside closes the menu.
    MouseArea {
        anchors.fill: parent
        acceptedButtons: Qt.AllButtons
        onClicked: Ui.windowMenu = null
    }

    function run(op) {
        const id = win.menu ? win.menu.win : "";
        Ui.windowMenu = null;
        if (op.startsWith("ws:")) {
            Bus.act("window.to_workspace", { window: id, workspace: op.slice(3) });
            return;
        }
        Ui.windowOp(op, id);
    }

    readonly property string st: target ? (target.state || "") : ""
    readonly property var entries: [
        { op: "minimize", label: Tr.t("Minimize"), key: "Super+H", show: (Bus.desktop.caps || {}).minimize === true },
        { op: "maximize", label: st === "maximized" ? Tr.t("Restore size") : Tr.t("Maximize"), key: "Super+Up", show: true },
        { op: "left", label: st === "left" ? Tr.t("Restore size") : Tr.t("Snap left"), key: "Super+Left", show: true },
        { op: "right", label: st === "right" ? Tr.t("Restore size") : Tr.t("Snap right"), key: "Super+Right", show: true },
        { op: "float", label: target && target.floating ? Tr.t("Tile") : Tr.t("Float"), key: "Super+T", show: true },
    ]

    Surface {
        id: card
        width: 260
        height: col.implicitHeight + Theme.s2 * 2
        radius: Theme.radiusMd
        // Keep the menu on the screen.
        x: win.output && win.menu ? Math.max(Theme.s2, Math.min(win.menu.x - win.output.rect.x, win.width - width - Theme.s2)) : 0
        y: win.output && win.menu ? Math.max(Theme.s2, Math.min(win.menu.y - win.output.rect.y, win.height - height - Theme.s2)) : 0
        opacity: win.visible ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: Theme.fast } }

        // Swallow clicks on the card itself.
        MouseArea { anchors.fill: parent }

        Column {
            id: col
            anchors.left: parent.left
            anchors.right: parent.right
            anchors.top: parent.top
            anchors.margins: Theme.s2
            spacing: 2

            Txt {
                width: parent.width
                leftPadding: Theme.s2
                rightPadding: Theme.s2
                bottomPadding: Theme.s1
                text: win.target ? (win.target.title || win.target.app_id) : ""
                role: "small"
                color: Theme.textMuted
            }

            Repeater {
                model: win.entries.filter(e => e.show)
                delegate: MenuRow {
                    required property var modelData
                    label: modelData.label
                    hint: modelData.key
                    e2e: "window-menu-" + modelData.op
                    onTriggered: win.run(modelData.op)
                }
            }

            Rectangle { width: parent.width; height: 1; color: Theme.border; opacity: 0.7 }

            // Move to a workspace: the numbered ones, as compact buttons.
            Row {
                spacing: Theme.s1
                leftPadding: Theme.s2
                height: Theme.fontSize * 2.6
                Txt { text: Tr.t("Move to"); role: "small"; color: Theme.textMuted; anchors.verticalCenter: parent.verticalCenter; rightPadding: Theme.s1 }
                Repeater {
                    model: [1, 2, 3, 4, 5]
                    delegate: Rectangle {
                        required property var modelData
                        property string e2e: "window-menu-ws-" + modelData
                        readonly property bool here: {
                            const ws = (Bus.desktop.workspaces || []).find(w => w.id === (win.target ? win.target.workspace : ""));
                            return ws ? ws.index === modelData : false;
                        }
                        anchors.verticalCenter: parent.verticalCenter
                        width: Theme.fontSize * 2.2
                        height: width
                        radius: Theme.radiusSm
                        color: here ? Theme.accentSoft : (wsMa.containsMouse ? Theme.hover : "transparent")
                        border.width: 1
                        border.color: Theme.border
                        Txt { anchors.centerIn: parent; text: modelData; role: "small"; color: parent.here ? Theme.accent : Theme.text }
                        MouseArea {
                            id: wsMa
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: win.run("ws:" + modelData)
                        }
                    }
                }
            }

            Rectangle { width: parent.width; height: 1; color: Theme.border; opacity: 0.7 }

            MenuRow {
                label: Tr.t("Close")
                hint: "Super+Q"
                e2e: "window-menu-close"
                danger: true
                onTriggered: win.run("close")
            }
        }
    }

    // Keyboard: Escape closes; N minimizes, X maximizes, C closes.
    Item {
        anchors.fill: parent
        focus: win.visible
        Keys.onEscapePressed: Ui.windowMenu = null
        Keys.onPressed: event => {
            switch (event.key) {
            case Qt.Key_N: win.run("minimize"); break;
            case Qt.Key_X: win.run("maximize"); break;
            case Qt.Key_C: win.run("close"); break;
            }
        }
    }

    component MenuRow: Rectangle {
        id: row
        property string label: ""
        property string hint: ""
        property string e2e: ""
        property bool danger: false
        signal triggered()
        width: parent ? parent.width : 0
        height: Theme.fontSize * 2.7
        radius: Theme.radiusSm
        color: ma.pressed ? Theme.pressed : (ma.containsMouse ? Theme.hover : "transparent")
        Txt {
            anchors.left: parent.left
            anchors.leftMargin: Theme.s2
            anchors.verticalCenter: parent.verticalCenter
            text: row.label
            color: row.danger ? Theme.danger : Theme.text
        }
        Txt {
            anchors.right: parent.right
            anchors.rightMargin: Theme.s2
            anchors.verticalCenter: parent.verticalCenter
            text: row.hint
            role: "small"
            color: Theme.textMuted
        }
        MouseArea {
            id: ma
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: row.triggered()
        }
    }
}
