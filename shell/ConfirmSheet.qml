import QtQuick
import Quickshell
import Quickshell.Wayland

// Confirmation sheet for requests from agents (MCP or IPC clients): the
// person sees who asks, every step and the token diff, and confirms or
// declines. The daemon refuses confirmations from anyone but this UI.
PanelWindow {
    id: win
    readonly property var queue: Bus.pending.filter(p => p.origin !== "commandbar")
    readonly property var current: queue.length > 0 ? queue[0] : null
    property var shown: null
    visible: current !== null || sheet.opacity > 0
    anchors { top: true; bottom: true; left: true; right: true }
    color: "transparent"
    exclusionMode: ExclusionMode.Ignore
    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.namespace: "basalt-confirm"
    readonly property bool hasKeyboard: current !== null && !Ui.polkitActive && !Ui.chooserActive
    WlrLayershell.keyboardFocus: hasKeyboard ? WlrKeyboardFocus.Exclusive : WlrKeyboardFocus.None
    Binding { target: Ui; property: "confirmActive"; value: win.current !== null }

    onCurrentChanged: if (current !== null) { shown = current; confirmBtn.forceActiveFocus(); }
    onHasKeyboardChanged: if (hasKeyboard) confirmBtn.forceActiveFocus()

    property int remaining: 0
    Timer {
        interval: 1000; repeat: true; running: win.current !== null
        onTriggered: win.remaining = win.current ? Math.max(0, Math.round((new Date(win.current.expires) - new Date()) / 1000)) : 0
    }

    function decide(approve) {
        if (!current) return;
        Bus.decide(current.id, approve, () => {});
    }

    Rectangle {
        anchors.fill: parent
        color: Theme.scrim
        opacity: win.current !== null ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        MouseArea { anchors.fill: parent }
    }

    Surface {
        id: sheet
        width: Math.min(640, parent.width - Theme.s6 * 2)
        height: Math.min(col.implicitHeight + Theme.s6 * 2, parent.height * 0.85)
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.verticalCenter: parent.verticalCenter
        anchors.verticalCenterOffset: win.current !== null ? 0 : Theme.s6
        opacity: win.current !== null ? 1 : 0
        scale: win.current !== null ? 1 : 0.97
        Behavior on opacity { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        Behavior on scale { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        Behavior on anchors.verticalCenterOffset { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }

        Column {
            id: col
            anchors.fill: parent
            anchors.margins: Theme.s6
            spacing: Theme.s4

            Row {
                spacing: Theme.s3
                Rectangle {
                    width: Theme.fontTitle * 2.4; height: width; radius: Theme.radiusMd
                    color: Theme.accentSoft
                    Icon { anchors.centerIn: parent; name: "spark"; color: Theme.accent; size: parent.width * 0.55 }
                }
                Column {
                    anchors.verticalCenter: parent.verticalCenter
                    Txt { text: "An assistant wants to change your desktop"; role: "title" }
                    Txt {
                        text: win.shown ? ("Requested by " + win.shown.actor + " through " + (win.shown.origin === "mcp" ? "MCP" : "local IPC") +
                              (win.queue.length > 1 ? "  (" + (win.queue.length - 1) + " more waiting)" : "")) : ""
                        color: Theme.textMuted; role: "small"
                    }
                }
            }

            ProposalView { width: parent.width; proposal: win.shown }

            Txt {
                width: parent.width
                wrapMode: Text.Wrap
                role: "small"
                color: Theme.textMuted
                text: "Nothing runs unless you confirm. Your decision is recorded in the activity log." +
                      (win.remaining > 0 ? "  Expires in " + Math.floor(win.remaining / 60) + ":" + ("0" + win.remaining % 60).slice(-2) + "." : "")
            }

            Row {
                spacing: Theme.s2
                anchors.right: parent.right
                Btn { text: "Decline"; variant: "outline"; focusable: true; onClicked: win.decide(false); Keys.onEscapePressed: win.decide(false) }
                Btn { id: confirmBtn; text: "Confirm"; icon: "check"; variant: "primary"; focusable: true; onClicked: win.decide(true); Keys.onEscapePressed: win.decide(false) }
            }
        }
    }
}
