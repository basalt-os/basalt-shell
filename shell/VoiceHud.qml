import QtQuick
import Quickshell
import Quickshell.Wayland

// Push to talk, on screen: while the person holds the key (or the panel's
// microphone button) a card under the panel says the microphone is open;
// then what was heard, and that the answer is coming or being spoken.
// It never takes the keyboard or clicks (input passes through).
PanelWindow {
    id: hud
    readonly property var v: Bus.voice || ({ state: "idle" })
    readonly property string st: v.state || "idle"
    property bool showError: false
    visible: st === "listening" || st === "transcribing" || st === "thinking" || st === "speaking" || showError
    anchors { top: true; left: true; right: true }
    implicitHeight: card.y + card.height + Theme.s4
    color: "transparent"
    exclusionMode: ExclusionMode.Ignore
    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.namespace: "basalt-voice"
    WlrLayershell.keyboardFocus: WlrKeyboardFocus.None
    mask: Region {}

    onStChanged: if (st === "error" || (st === "idle" && v.error)) { showError = true; errTimer.restart(); }
    Timer { id: errTimer; interval: 3500; onTriggered: hud.showError = false }

    property int held: 0
    Timer {
        interval: 100; repeat: true; running: hud.st === "listening"
        onTriggered: hud.held = Math.max(0, new Date() - new Date(hud.v.since))
    }

    Surface {
        id: card
        anchors.horizontalCenter: parent.horizontalCenter
        y: Theme.panelHeight + Theme.s4
        width: Math.min(620, hud.width - Theme.s6 * 2)
        height: row.implicitHeight + Theme.s3 * 2
        Row {
            id: row
            anchors.left: parent.left
            anchors.right: parent.right
            anchors.verticalCenter: parent.verticalCenter
            anchors.margins: Theme.s4
            spacing: Theme.s3
            Rectangle {
                width: Theme.fontSize * 2.6; height: width; radius: width / 2
                anchors.verticalCenter: parent.verticalCenter
                color: hud.st === "listening" ? Theme.danger : Theme.accentSoft
                Icon { anchors.centerIn: parent; name: hud.showError ? "info" : "mic"; size: Theme.fontSize * 1.5; color: hud.st === "listening" ? "#ffffff" : Theme.accent }
                SequentialAnimation on scale {
                    running: hud.st === "listening" && Theme.normal > 0
                    loops: Animation.Infinite
                    NumberAnimation { from: 1; to: 1.12; duration: 520; easing.type: Easing.InOutSine }
                    NumberAnimation { from: 1.12; to: 1; duration: 520; easing.type: Easing.InOutSine }
                }
            }
            Column {
                width: row.width - Theme.fontSize * 2.6 - row.spacing
                anchors.verticalCenter: parent.verticalCenter
                spacing: Theme.s1
                Txt {
                    font.weight: Font.DemiBold
                    text: hud.showError ? (hud.v.error || qsTr("Voice error"))
                        : hud.st === "listening" ? qsTr("Listening. Release to send (%1 s)").arg((hud.held / 1000).toFixed(1))
                        : hud.st === "transcribing" ? qsTr("Turning speech into text, on this computer")
                        : hud.st === "thinking" ? qsTr("Working on it")
                        : qsTr("Speaking")
                }
                Txt {
                    visible: hud.st !== "listening" && (hud.v.text || "") !== ""
                    width: parent.width
                    wrapMode: Text.Wrap
                    color: Theme.textMuted
                    text: qsTr("“%1”").arg(hud.v.text || "")
                }
                Txt {
                    visible: hud.st === "listening"
                    role: "small"
                    color: Theme.textMuted
                    text: qsTr("Microphone open only while you hold the key. Audio stays on this computer and is not kept.")
                }
            }
        }
    }
}
