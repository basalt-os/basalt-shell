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
// "Press to start and stop" (the person's push_to_talk = toggle, and
// always on niri, which has no key-release bindings): the card says to
// press Super+V again to stop, shows the mode, the limit and the silence
// that ends it, and offers Cancel. Escape cancels too: on sway the
// compositor binds it while the microphone is open (esc_key); elsewhere
// the card takes the keyboard while the words go to the assistant (no
// text field to keep focused), and only Cancel is offered while dictating
// (taking the keyboard would end the field's input method session).
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
    // "Press to start and stop" while the microphone is open.
    readonly property bool toggleListening: st === "listening" && v.push_to_talk === "toggle"
    // Escape through the card: no compositor binding, words for the
    // assistant, and the command bar (which has its own Escape) closed.
    readonly property bool grabEsc: toggleListening && !v.esc_key && !dictating && !Ui.commandBar
    readonly property bool escWorks: toggleListening && (!!v.esc_key || grabEsc)
    property bool showError: false
    property bool showNote: false
    visible: st === "listening" || st === "transcribing" || st === "thinking" || st === "speaking" || waiting || asking || showError || showNote
    anchors { top: true; left: true; right: true }
    implicitHeight: card.y + card.height + Theme.s4
    color: "transparent"
    exclusionMode: ExclusionMode.Ignore
    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.namespace: "basalt-voice"
    // The download offer is a question: it takes the keyboard (Not now is
    // focused, so a stray Return downloads nothing; Escape is Not now too)
    // unless a surface or a sheet has it.
    readonly property bool offerKeys: st === "offer" && !!v.offer && v.offer.ask !== "" && !Ui.surfaceOpen && !Ui.modal
    WlrLayershell.keyboardFocus: (hud.grabEsc || hud.offerKeys) ? WlrKeyboardFocus.Exclusive : (hud.asking ? WlrKeyboardFocus.OnDemand : WlrKeyboardFocus.None)
    onOfferKeysChanged: if (offerKeys) { Ui.focusVisible = true; Qt.callLater(() => Nav.initial(offerView, "model-not-now")); }
    // Clicks pass through, except on the card while a dictation waits, a
    // download is offered or running, or Cancel is offered.
    mask: Region { item: (hud.waiting || hud.asking || hud.toggleListening) ? card : null }

    onStChanged: {
        // A new utterance (or the next step of one) clears what the last
        // one left on the card: an error or a note must never stay on top
        // of "Listening" (the owner's 0.5.1 report: "Voice error" over the
        // listening hints after a previous "nothing heard").
        if (st === "offer" || st === "download" || st === "listening" || st === "transcribing" || st === "thinking" || st === "speaking" || st === "dictation") {
            showError = false; showNote = false; errTimer.stop(); noteTimer.stop();
            if (st === "offer" || st === "download" || st === "listening") return;
        }
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
        if (toggleListening) {
            const t = (held / 1000).toFixed(1);
            return dictating ? Tr.t("Dictating into %1. Press Super+V again to stop (%2 s)").arg(v.target || "").arg(t)
                             : Tr.t("Listening, press Super+V again to stop (%1 s)").arg(t);
        }
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

    // Escape while the card holds the keyboard (grabEsc).
    Item {
        id: escCatcher
        focus: hud.grabEsc
        Keys.onEscapePressed: Bus.voiceCancel()
    }
    onGrabEscChanged: if (grabEsc) escCatcher.forceActiveFocus()

    Surface {
        id: card
        anchors.horizontalCenter: parent.horizontalCenter
        y: Theme.panelHeight + Theme.s4
        width: Math.min(660, hud.width - Theme.s6 * 2)
        height: row.implicitHeight + Theme.s3 * 2
        Accessible.role: hud.asking ? Accessible.Dialog : Accessible.AlertMessage
        Accessible.name: hud.asking ? offerView.titleText : hud.title()
        Keys.onEscapePressed: {
            if (hud.st === "offer" && hud.v.offer) Bus.modelsDismiss(hud.v.offer.id);
            else Bus.voiceCancel();
        }
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
                // Red only for an error; the open microphone is the accent
                // (hints and notes are neutral).
                color: hud.showError ? Theme.danger : (hud.st === "listening" ? Theme.accent : Theme.accentSoft)
                Icon { anchors.centerIn: parent; name: hud.asking ? "download" : ((hud.showError || hud.showNote) ? "info" : (hud.waiting ? "list" : "mic")); size: Theme.fontSize * 1.5; color: (hud.st === "listening" || hud.showError) ? "#ffffff" : Theme.accent }
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
                    id: offerView
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
                    color: hud.showError ? Theme.danger : Theme.text
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
                    visible: hud.st === "listening" && !hud.toggleListening
                    width: parent.width
                    wrapMode: Text.Wrap
                    role: "small"
                    color: Theme.textMuted
                    text: Tr.t("Microphone open only while you hold the key. Audio stays on this computer and is not kept.")
                }
                // Press to start and stop: the mode, how it ends, Cancel.
                Txt {
                    visible: hud.toggleListening
                    width: parent.width
                    wrapMode: Text.Wrap
                    role: "small"
                    color: Theme.textMuted
                    text: Tr.t("Mode: press to start and stop.") + " " +
                          (hud.v.auto_stop_ms > 0 ? Tr.t("It also stops after %1 s of silence, and at %2 s.").arg((hud.v.auto_stop_ms / 1000).toLocaleString(Qt.locale(), "f", hud.v.auto_stop_ms % 1000 ? 1 : 0)).arg(hud.v.max_hold_s || 30)
                                                  : Tr.t("It stops at %1 s at the latest.").arg(hud.v.max_hold_s || 30)) + " " +
                          (hud.escWorks ? Tr.t("Esc cancels.") : Tr.t("Cancel drops the words."))
                }
                Txt {
                    visible: hud.toggleListening
                    width: parent.width
                    wrapMode: Text.Wrap
                    role: "small"
                    color: Theme.textMuted
                    text: Tr.t("Audio stays on this computer and is not kept.")
                }
                Btn {
                    visible: hud.toggleListening
                    text: Tr.t("Cancel"); variant: "outline"; e2e: "voice-cancel"
                    onClicked: Bus.voiceCancel()
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
