import QtQuick
import Quickshell
import Quickshell.Wayland

// Push to talk, on screen: while the person holds the key (or the panel's
// microphone button) a card under the panel says the microphone is open
// and where the words will go (dictation into the focused text field, or
// the assistant); then what was heard, and that the answer is coming or
// being spoken. Dictated text waits on the card (and in the field, as
// underlined pre-edit text) until the person presses Insert or Discard.
// The card never takes the keyboard; it takes clicks only while a
// dictation waits for a decision.
PanelWindow {
    id: hud
    readonly property var v: Bus.voice || ({ state: "idle" })
    readonly property string st: v.state || "idle"
    readonly property bool dictating: v.mode === "dictation"
    readonly property bool waiting: st === "dictation" && !!v.proposal
    property bool showError: false
    visible: st === "listening" || st === "transcribing" || st === "thinking" || st === "speaking" || waiting || showError
    anchors { top: true; left: true; right: true }
    implicitHeight: card.y + card.height + Theme.s4
    color: "transparent"
    exclusionMode: ExclusionMode.Ignore
    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.namespace: "basalt-voice"
    WlrLayershell.keyboardFocus: WlrKeyboardFocus.None
    // Clicks pass through, except on the card while a dictation waits.
    mask: Region { item: hud.waiting ? card : null }

    onStChanged: if (st === "error" || (st === "idle" && v.error)) { showError = true; errTimer.restart(); }
    Timer { id: errTimer; interval: 3500; onTriggered: hud.showError = false }

    property int held: 0
    Timer {
        interval: 100; repeat: true; running: hud.st === "listening"
        onTriggered: hud.held = Math.max(0, new Date() - new Date(hud.v.since))
    }

    function title() {
        if (showError) return v.error || qsTr("Voice error");
        if (st === "listening") {
            const t = (held / 1000).toFixed(1);
            return dictating ? qsTr("Dictating into %1. Release to stop (%2 s)").arg(v.target || "").arg(t)
                             : qsTr("Listening for the assistant. Release to send (%1 s)").arg(t);
        }
        if (st === "transcribing") return qsTr("Turning speech into text, on this computer");
        if (st === "thinking") return qsTr("Working on it");
        if (st === "dictation") return qsTr("Type this into %1?").arg(v.target || "");
        return qsTr("Speaking");
    }

    Surface {
        id: card
        anchors.horizontalCenter: parent.horizontalCenter
        y: Theme.panelHeight + Theme.s4
        width: Math.min(660, hud.width - Theme.s6 * 2)
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
                anchors.top: parent.top
                color: hud.st === "listening" ? Theme.danger : Theme.accentSoft
                Icon { anchors.centerIn: parent; name: hud.showError ? "info" : (hud.waiting ? "list" : "mic"); size: Theme.fontSize * 1.5; color: hud.st === "listening" ? "#ffffff" : Theme.accent }
                SequentialAnimation on scale {
                    running: hud.st === "listening" && Theme.normal > 0
                    loops: Animation.Infinite
                    NumberAnimation { from: 1; to: 1.12; duration: 520; easing.type: Easing.InOutSine }
                    NumberAnimation { from: 1.12; to: 1; duration: 520; easing.type: Easing.InOutSine }
                }
            }
            Column {
                id: col
                width: row.width - Theme.fontSize * 2.6 - row.spacing
                spacing: Theme.s1
                Txt {
                    width: parent.width
                    wrapMode: Text.Wrap
                    font.weight: Font.DemiBold
                    text: hud.title()
                }
                // Where the words go, while the key is held.
                Txt {
                    visible: hud.st === "listening" && !hud.dictating
                    width: parent.width
                    wrapMode: Text.Wrap
                    role: "small"
                    color: Theme.textMuted
                    text: hud.v.note ? hud.v.note : qsTr("To dictate into a text field, click in the field first. Say \"assistant\" first to ask the assistant from a text field.")
                }
                Txt {
                    visible: hud.st !== "listening" && (hud.v.text || "") !== ""
                    width: parent.width
                    wrapMode: Text.Wrap
                    color: hud.waiting ? Theme.text : Theme.textMuted
                    text: qsTr("“%1”").arg(hud.v.text || "")
                }
                Txt {
                    visible: hud.st === "listening"
                    width: parent.width
                    wrapMode: Text.Wrap
                    role: "small"
                    color: Theme.textMuted
                    text: qsTr("Microphone open only while you hold the key. Audio stays on this computer and is not kept.")
                }
                Txt {
                    visible: hud.waiting
                    width: parent.width
                    wrapMode: Text.Wrap
                    role: "small"
                    color: Theme.textMuted
                    text: qsTr("Shown underlined in the field. Nothing is typed until you choose Insert (Super+Shift+Return).")
                }
                Row {
                    visible: hud.waiting
                    spacing: Theme.s2
                    Btn { text: qsTr("Insert"); icon: "check"; variant: "primary"; e2e: "voice-insert"; onClicked: Bus.dictationDecide(true) }
                    Btn { text: qsTr("Discard"); variant: "outline"; e2e: "voice-discard"; onClicked: Bus.dictationDecide(false) }
                }
            }
        }
    }
}
