import QtQuick
import Quickshell
import Quickshell.Wayland

// "Ask the system": free text goes to the daemon, which understands it
// (local model if configured, else rules) and answers with a proposal of
// typed actions, or runs a read-only question through the system
// assistant. Nothing is applied until the person presses Apply.
PanelWindow {
    id: win
    visible: Ui.commandBar || card.opacity > 0
    anchors { top: true; bottom: true; left: true; right: true }
    color: "transparent"
    exclusionMode: ExclusionMode.Ignore
    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.namespace: "basalt-commandbar"
    WlrLayershell.keyboardFocus: Ui.commandBar && !Ui.modal ? WlrKeyboardFocus.Exclusive : WlrKeyboardFocus.None

    property var result: null      // AskResult from the daemon
    property bool busy: false
    property string status: ""     // after Apply / Ignore
    property bool statusOk: true
    readonly property var proposal: result && result.proposal ? result.proposal : null
    readonly property var assist: result && result.assistant ? result.assistant : null

    function submit() {
        const t = field.text.trim();
        if (!t || busy) return;
        busy = true; result = null; status = "";
        Bus.ask(t, (ok, res) => {
            busy = false;
            win.result = ok ? res : { kind: "error", error: res };
        });
    }
    function decide(approve) {
        if (!proposal) return;
        busy = true;
        Bus.decide(proposal.id, approve, (ok, res) => {
            busy = false;
            statusOk = ok && (res.status === "applied" || res.status === "declined");
            status = ok ? (res.status === "applied" ? "Applied." : (res.status === "declined" ? "Ignored. Nothing changed." : res.status + (res.error ? ": " + res.error : "")))
                        : "Failed: " + res;
            result = null;
            if (ok && res.status === "applied") closeTimer.restart();
        });
    }
    function assistantDecide(apply) {
        if (!assist) return;
        busy = true;
        status = apply ? "Waiting for authentication..." : "";
        Bus.call(apply ? "assistant.apply" : "assistant.ignore", apply ? { id: assist.id, code: assist.code } : { id: assist.id }, (ok, res) => {
            busy = false;
            statusOk = ok && res.ok;
            status = (statusOk ? (apply ? "Applied by the system assistant." : "Ignored.") : "Not applied.") ;
            if (ok && res.output) win.result = { kind: "system", text: res.output };
            Bus.refreshAssistant();
        });
    }

    Timer { id: closeTimer; interval: 1400; onTriggered: Ui.commandBar = false }

    onVisibleChanged: if (Ui.commandBar) {
        field.text = Ui.commandText;
        Ui.commandText = "";
        status = "";
        if (!busy) result = null;
        field.focusInput();
        if (field.text !== "") submit();
    }

    MouseArea { anchors.fill: parent; onClicked: Ui.commandBar = false }

    Surface {
        id: card
        width: Math.min(760, parent.width - Theme.s6 * 2)
        height: Math.min(content.implicitHeight + Theme.s4 * 2, parent.height * 0.8)
        anchors.horizontalCenter: parent.horizontalCenter
        y: parent.height * 0.12 + (Ui.commandBar ? 0 : Theme.s6)
        opacity: Ui.commandBar ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        Behavior on y { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        Behavior on height { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        MouseArea { anchors.fill: parent }

        Flickable {
            anchors.fill: parent
            anchors.margins: Theme.s4
            contentHeight: content.implicitHeight
            clip: true
            Column {
                id: content
                width: parent.width
                spacing: Theme.s3

                Field {
                    id: field
                    width: parent.width
                    icon: "spark"
                    placeholder: "Ask the system: \"make it darker with rounder corners\", \"why nginx\""
                    onAccepted: win.submit()
                    onEscapePressed: Ui.commandBar = false
                }

                Row {
                    spacing: Theme.s2
                    visible: !win.result && !win.busy && win.status === ""
                    Repeater {
                        model: ["make it darker with rounder corners", "light mode", "arrange windows side by side", "open text editor", "system status"]
                        delegate: Btn {
                            required property var modelData
                            text: modelData
                            variant: "outline"
                            onClicked: { field.text = modelData; win.submit(); }
                        }
                    }
                }

                Txt {
                    visible: win.busy
                    text: "Thinking..."
                    color: Theme.textMuted
                }

                // A desktop proposal.
                Column {
                    visible: win.proposal !== null
                    width: parent.width
                    spacing: Theme.s3
                    Row {
                        spacing: Theme.s2
                        Txt { text: "Proposal"; role: "large"; font.weight: Font.DemiBold }
                        Rectangle {
                            radius: height / 2; height: Theme.fontSize * 1.8; width: tag.implicitWidth + Theme.s3
                            color: Theme.accentSoft
                            anchors.verticalCenter: parent.verticalCenter
                            Txt { id: tag; anchors.centerIn: parent; role: "small"; color: Theme.accent
                                  text: win.proposal ? ("understood by " + (win.proposal.backend === "model" ? "the local model" : "rules")) : "" }
                        }
                    }
                    Txt {
                        visible: win.proposal && win.proposal.explain
                        text: win.proposal ? win.proposal.explain : ""
                        color: Theme.textMuted
                        width: parent.width
                        wrapMode: Text.Wrap
                    }
                    ProposalView { width: parent.width; proposal: win.proposal }
                    Txt {
                        visible: win.result && win.result.unknown && win.result.unknown.length > 0
                        text: win.result && win.result.unknown ? "Not understood: " + win.result.unknown.join(", ") : ""
                        color: Theme.warning; role: "small"
                    }
                    Row {
                        spacing: Theme.s2
                        Btn { text: "Apply"; icon: "check"; variant: "primary"; focusable: true; onClicked: win.decide(true) }
                        Btn { text: "Ignore"; variant: "outline"; focusable: true; onClicked: win.decide(false) }
                    }
                }

                // A system assistant answer, maybe with a proposal.
                Column {
                    visible: win.result !== null && win.result.kind === "system"
                    width: parent.width
                    spacing: Theme.s3
                    Row {
                        spacing: Theme.s2
                        Icon { name: "shield"; size: Theme.fontLarge * 1.4; color: Theme.accent }
                        Txt { text: win.assist ? (win.assist.title || "System assistant proposal " + win.assist.id) : "System assistant"; role: "large"; font.weight: Font.DemiBold }
                    }
                    Rectangle {
                        width: parent.width
                        height: Math.min(sysText.implicitHeight + Theme.s3 * 2, 340)
                        radius: Theme.radiusMd
                        color: Theme.bg
                        border.width: 1; border.color: Theme.border
                        Flickable {
                            anchors.fill: parent
                            anchors.margins: Theme.s3
                            contentHeight: sysText.implicitHeight
                            clip: true
                            Txt {
                                id: sysText
                                width: parent.width
                                role: "mono"
                                wrapMode: Text.Wrap
                                elide: Text.ElideNone
                                verticalAlignment: Text.AlignTop
                                text: win.result ? (win.assist && win.assist.report ? win.assist.report : (win.result.text || "")) : ""
                            }
                        }
                    }
                    Txt {
                        visible: win.assist !== null
                        width: parent.width
                        wrapMode: Text.Wrap
                        color: Theme.textMuted
                        role: "small"
                        text: win.assist ? "Apply runs the assistant's own confirmation (basalt apply " + win.assist.id + " with code " + win.assist.code + "): snapshot before and after, verification, audit. You will be asked to authenticate." : ""
                    }
                    Row {
                        visible: win.assist !== null
                        spacing: Theme.s2
                        Btn { text: "Apply"; icon: "check"; variant: "primary"; focusable: true; onClicked: win.assistantDecide(true) }
                        Btn { text: "Ignore"; variant: "outline"; focusable: true; onClicked: win.assistantDecide(false) }
                    }
                }

                Txt {
                    visible: win.result !== null && (win.result.kind === "error" || win.result.kind === "unknown")
                    width: parent.width
                    wrapMode: Text.Wrap
                    color: Theme.danger
                    text: win.result ? (win.result.error || "") : ""
                }

                Row {
                    visible: win.status !== ""
                    spacing: Theme.s2
                    Icon { name: win.statusOk ? "check" : "info"; color: win.statusOk ? Theme.success : Theme.warning; size: Theme.fontLarge * 1.3 }
                    Txt { text: win.status; color: win.statusOk ? Theme.success : Theme.warning; font.weight: Font.Medium }
                }
            }
        }
    }
}
