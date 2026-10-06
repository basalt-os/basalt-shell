import QtQuick
import Quickshell
import Quickshell.Wayland

// A question with fixed options asked by the system (for example which
// screen an application may share, from the screen-cast portal). The
// answer goes back through the daemon and is written to the activity log.
// Keyboard: Up and Down move between the options (focus starts on the
// first), Return chooses, Escape cancels.
PanelWindow {
    id: win
    property var current: null
    visible: current !== null
    anchors { top: true; bottom: true; left: true; right: true }
    color: "transparent"
    exclusionMode: ExclusionMode.Ignore
    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.namespace: "basalt-chooser"
    WlrLayershell.keyboardFocus: current !== null && !Ui.polkitActive ? WlrKeyboardFocus.Exclusive : WlrKeyboardFocus.None
    Binding { target: Ui; property: "chooserActive"; value: win.current !== null }

    function answer(v) {
        if (!current) return;
        Bus.call("chosen", { id: current.id, choice: v }, () => {});
        current = null;
    }

    Connections {
        target: Bus
        function onChooseRequested(req) { win.current = req; }
        function onChooseDone(id) { if (win.current && win.current.id === id) win.current = null; }
    }

    Rectangle { anchors.fill: parent; color: Theme.scrim; MouseArea { anchors.fill: parent } }

    onCurrentChanged: if (current !== null) { Ui.focusVisible = true; Qt.callLater(() => Nav.initial(options, "")); }

    Surface {
        width: Math.min(560, parent.width - Theme.s6 * 2)
        height: col.implicitHeight + Theme.s6 * 2
        anchors.centerIn: parent
        Accessible.role: Accessible.Dialog
        Accessible.name: win.current ? win.current.title : ""
        Keys.onEscapePressed: win.answer("")
        Column {
            id: col
            anchors.fill: parent
            anchors.margins: Theme.s6
            spacing: Theme.s4
            Row {
                spacing: Theme.s3
                Icon { name: "screen"; size: Theme.fontTitle * 1.8; color: Theme.accent }
                Txt { text: win.current ? win.current.title : ""; role: "title"; anchors.verticalCenter: parent.verticalCenter }
            }
            Txt { width: parent.width; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted; text: win.current ? win.current.body : "" }
            NavColumn {
                id: options
                width: parent.width
                spacing: Theme.s4
                Repeater {
                    model: win.current ? win.current.options : []
                    delegate: Btn {
                        required property var modelData
                        width: col.width
                        alignLeft: true
                        variant: "outline"
                        icon: "screen"
                        e2e: "chooser-" + modelData.id
                        accessibleRole: Accessible.RadioButton
                        text: modelData.label + (modelData.hint ? "   " + modelData.hint : "")
                        onClicked: win.answer(modelData.id)
                    }
                }
            }
            Btn { text: "Cancel"; variant: "ghost"; e2e: "chooser-cancel"; anchors.right: parent.right; onClicked: win.answer("") }
        }
    }
}
