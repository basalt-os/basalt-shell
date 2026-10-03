import QtQuick
import Quickshell
import Quickshell.Wayland

// A question with fixed options asked by the system (for example which
// screen an application may share, from the screen-cast portal). The
// answer goes back through the daemon and is written to the activity log.
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

    Surface {
        width: Math.min(560, parent.width - Theme.s6 * 2)
        height: col.implicitHeight + Theme.s6 * 2
        anchors.centerIn: parent
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
            Repeater {
                model: win.current ? win.current.options : []
                delegate: Btn {
                    required property var modelData
                    required property int index
                    width: col.width
                    alignLeft: true
                    focusable: true
                    variant: "outline"
                    icon: "screen"
                    text: modelData.label + (modelData.hint ? "   " + modelData.hint : "")
                    onClicked: win.answer(modelData.id)
                    Component.onCompleted: if (index === 0) forceActiveFocus()
                    Keys.onEscapePressed: win.answer("")
                }
            }
            Btn { text: "Cancel"; variant: "ghost"; anchors.right: parent.right; onClicked: win.answer("") }
        }
    }
}
