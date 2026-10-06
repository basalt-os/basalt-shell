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
// dictation waits for a decision. It shows the speech language in force
// (the person's own setting, independent of the desktop's language) and,
// for a few seconds, a note after an answer (an answer shown and not
// spoken, for example). Its texts go through the UI catalog (Tr).
//
// Zero setup: when no speech model for the person's language is on the
// computer, pressing the key shows the download offer here instead
// (what, how big, from where; Download or Not now), then the download's
// progress; the card takes clicks (and the keyboard, on demand) while it
// asks or shows a download.
PanelWindow {
    id: hud
    readonly property var v: Bus.voice || ({ state: "idle" })
    readonly property string st: v.state || "idle"
    readonly property bool dictating: v.mode === "dictation"
    readonly property bool waiting: st === "dictation" && !!v.proposal
    // The speech model download: the offer, then the download.
    readonly property bool asking: (st === "offer" && !!v.offer) || (st === "download" && !!v.download)
    property bool showError: false
    property bool showNote: false
    visible: st === "listening" || st === "transcribing" || st === "thinking" || st === "speaking" || waiting || asking || showError || showNote
    anchors { top: true; left: true; right: true }
    implicitHeight: card.y + card.height + Theme.s4
    color: "transparent"
    exclusionMode: ExclusionMode.Ignore
    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.namespace: "basalt-voice"
    WlrLayershell.keyboardFocus: hud.asking ? WlrKeyboardFocus.OnDemand : WlrKeyboardFocus.None
    // Clicks pass through, except on the card while a dictation waits or
    // a download is offered or running.
    mask: Region { item: (hud.waiting || hud.asking) ? card : null }

    onStChanged: {
        if (st === "offer" || st === "download") { showError = false; showNote = false; return; }
        if (st === "error" || (st === "idle" && v.error)) { showError = true; errTimer.restart(); }
        else if (st === "idle" && v.note) { showNote = true; noteTimer.restart(); }
    }
    Timer { id: errTimer; interval: 5000; onTriggered: hud.showError = false }
    Timer { id: noteTimer; interval: 4500; onTriggered: hud.showNote = false }

    // The speech language, as the person reads it.
    function langLine() {
        if (!v.lang || v.lang === "auto") return Tr.t("Speech language: detected automatically");
        return Tr.t("Speech language: %1").arg(v.lang_name || v.lang);
    }

    property int held: 0
    Timer {
        interval: 100; repeat: true; running: hud.st === "listening"
        onTriggered: hud.held = Math.max(0, new Date() - new Date(hud.v.since))
    }

    function title() {
        if (showError) return v.error || Tr.t("Voice error");
        if (showNote && st === "idle") return v.note;
        if (st === "listening") {
            const t = (held / 1000).toFixed(1);
            return dictating ? Tr.t("Dictating into %1. Release to stop (%2 s)").arg(v.target || "").arg(t)
                             : Tr.t("Listening for the assistant. Release to send (%1 s)").arg(t);
        }
        if (st === "transcribing") return Tr.t("Turning speech into text, on this computer");
        if (st === "thinking") return Tr.t("Working on it");
        if (st === "dictation") return Tr.t("Type this into %1?").arg(v.target || "");
        return Tr.t("Speaking");
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
                Icon { anchors.centerIn: parent; name: hud.asking ? "download" : ((hud.showError || hud.showNote) ? "info" : (hud.waiting ? "list" : "mic")); size: Theme.fontSize * 1.5; color: hud.st === "listening" ? "#ffffff" : Theme.accent }
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
                // The speech model download (offer or progress).
                DownloadView {
                    visible: hud.asking && !hud.showError
                    width: parent.width
                    offer: hud.st === "offer" ? (hud.v.offer || null) : null
                    job: hud.st === "download" ? (hud.v.download || null) : null
                    langName: hud.v.lang_name || hud.v.lang || ""
                }
                Txt {
                    visible: !hud.asking || hud.showError
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
                    text: hud.v.note ? hud.v.note : Tr.t("To dictate into a text field, click in the field first. Say \"assistant\" first to ask the assistant from a text field.")
                }
                // The speech language in force, while the key is held and
                // when speech to text failed.
                Txt {
                    visible: !hud.asking && (hud.st === "listening" || hud.st === "transcribing" || hud.showError)
                    width: parent.width
                    wrapMode: Text.Wrap
                    role: "small"
                    color: Theme.textMuted
                    text: hud.langLine()
                }
                Txt {
                    visible: hud.st !== "listening" && (hud.v.text || "") !== ""
                    width: parent.width
                    wrapMode: Text.Wrap
                    color: hud.waiting ? Theme.text : Theme.textMuted
                    text: Tr.t("“%1”").arg(hud.v.text || "")
                }
                Txt {
                    visible: hud.st === "listening"
                    width: parent.width
                    wrapMode: Text.Wrap
                    role: "small"
                    color: Theme.textMuted
                    text: Tr.t("Microphone open only while you hold the key. Audio stays on this computer and is not kept.")
                }
                Txt {
                    visible: hud.waiting
                    width: parent.width
                    wrapMode: Text.Wrap
                    role: "small"
                    color: Theme.textMuted
                    text: Tr.t("Shown underlined in the field. Nothing is typed until you choose Insert (Super+Shift+Return).")
                }
                Row {
                    visible: hud.waiting
                    spacing: Theme.s2
                    Btn { text: Tr.t("Insert"); icon: "check"; variant: "primary"; e2e: "voice-insert"; onClicked: Bus.dictationDecide(true) }
                    Btn { text: Tr.t("Discard"); variant: "outline"; e2e: "voice-discard"; onClicked: Bus.dictationDecide(false) }
                }
            }
        }
    }
}
