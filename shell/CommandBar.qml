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
    readonly property var skill: result && result.skill ? result.skill : null
    readonly property bool isGrant: proposal !== null && proposal.calls && proposal.calls.length > 0 && proposal.calls[0].action === "grant.add"
    property string retry: ""        // run again after a grant is applied
    property var voiceData: null     // a spoken request's answer, shown when the bar opens

    // A spoken request: show what was heard and the answer.
    Connections {
        target: Bus
        function onVoiceResult(d) {
            win.voiceData = d;
            if (Ui.commandBar) win.showVoice(); else Ui.open("commandbar", "");
        }
    }
    function showVoice() {
        if (!voiceData) return;
        field.text = voiceData.request;
        result = voiceData.result;
        retry = result && result.retry ? result.retry : "";
        busy = false; status = "";
        voiceData = null;
    }
    function skillIcon(s) { return s === "files" || s === "open" ? "search" : (s === "mail" ? "bell" : (s === "web" ? "network" : "shield")); }
    function skillTitle(s) { return ({ files: qsTr("Files"), mail: qsTr("Mail"), web: qsTr("Web page"), open: qsTr("Open a file"), grant: qsTr("Permission"), revoke: qsTr("Permissions") })[s] || qsTr("Assistant"); }
    function grantLeft(g) {
        const s = Math.max(0, Math.round((new Date(g.expires) - new Date()) / 1000));
        return s >= 3600 ? qsTr("%n h left", "", Math.round(s / 3600)) : (s >= 60 ? qsTr("%n min left", "", Math.round(s / 60)) : qsTr("%n s left", "", s));
    }

    function submit() {
        const t = field.text.trim();
        if (!t || busy) return;
        // A new request replaces the previous proposal: ignore it.
        if (proposal && proposal.status === "pending") Bus.decide(proposal.id, false, () => {});
        busy = true; result = null; status = "";
        Bus.ask(t, (ok, res) => {
            busy = false;
            win.result = ok ? res : { kind: "error", error: res };
            win.retry = ok && res.retry ? res.retry : "";
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
            if (ok && res.status === "applied" && win.retry !== "") {
                // A permission the request needed: run the request again.
                field.text = win.retry; win.retry = "";
                status = "";
                win.submit();
                return;
            }
            win.retry = "";
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

    // Closing the bar with a proposal still open ignores it: nothing is
    // left waiting (agents' input waits while a request is open).
    Connections {
        target: Ui
        function onCommandBarChanged() {
            if (!Ui.commandBar && win.proposal && win.proposal.status === "pending") {
                Bus.decide(win.proposal.id, false, () => {});
                win.result = null;
            }
        }
    }

    onVisibleChanged: if (Ui.commandBar) {
        if (voiceData) { showVoice(); field.focusInput(); return; }
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
                        Txt { text: win.isGrant ? qsTr("Permission") : qsTr("Proposal"); role: "large"; font.weight: Font.DemiBold }
                        Rectangle {
                            radius: height / 2; height: Theme.fontSize * 1.8; width: tag.implicitWidth + Theme.s3
                            color: Theme.accentSoft
                            anchors.verticalCenter: parent.verticalCenter
                            Txt { id: tag; anchors.centerIn: parent; role: "small"; color: Theme.accent
                                  text: win.proposal ? (win.proposal.backend === "skill" ? (win.isGrant ? qsTr("read only, expires by itself") : qsTr("from your request")) : (win.proposal.backend === "model" ? qsTr("understood by the local model") : qsTr("understood by the fixed phrases"))) : "" }
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
                        Btn { text: win.isGrant ? qsTr("Allow") : qsTr("Apply"); icon: "check"; variant: "primary"; focusable: true; onClicked: win.decide(true) }
                        Btn { text: win.isGrant ? qsTr("Don't allow") : qsTr("Ignore"); variant: "outline"; focusable: true; onClicked: win.decide(false) }
                    }
                }

                // A read-only skill's answer: the summary, the results, and
                // what the guard found in the content.
                Column {
                    visible: win.skill !== null && win.result.kind === "skill"
                    width: parent.width
                    spacing: Theme.s3
                    Row {
                        spacing: Theme.s2
                        Icon { name: win.skill ? win.skillIcon(win.skill.skill) : "spark"; size: Theme.fontLarge * 1.4; color: Theme.accent; anchors.verticalCenter: parent.verticalCenter }
                        Txt { text: win.skill ? win.skillTitle(win.skill.skill) : ""; role: "large"; font.weight: Font.DemiBold; anchors.verticalCenter: parent.verticalCenter }
                        Rectangle {
                            radius: height / 2; height: Theme.fontSize * 1.8; width: rtag.implicitWidth + Theme.s3
                            color: Theme.accentSoft
                            anchors.verticalCenter: parent.verticalCenter
                            Txt { id: rtag; anchors.centerIn: parent; role: "small"; color: Theme.accent; text: qsTr("read only") }
                        }
                    }
                    // Warnings first: content that tried to instruct the assistant.
                    Rectangle {
                        visible: win.skill && win.skill.warnings && win.skill.warnings.length > 0
                        width: parent.width
                        height: warnCol.implicitHeight + Theme.s3 * 2
                        radius: Theme.radiusMd
                        color: Theme.alpha(Theme.warning, 0.12)
                        border.width: 1; border.color: Theme.warning
                        Column {
                            id: warnCol
                            anchors.fill: parent; anchors.margins: Theme.s3
                            spacing: Theme.s1
                            Repeater {
                                model: win.skill && win.skill.warnings ? win.skill.warnings : []
                                delegate: Row {
                                    required property var modelData
                                    spacing: Theme.s2
                                    Icon { name: "warning"; size: Theme.fontSize * 1.4; color: Theme.warning }
                                    Txt { text: modelData; width: warnCol.width - Theme.s6; wrapMode: Text.Wrap; elide: Text.ElideNone }
                                }
                            }
                        }
                    }
                    Txt {
                        width: parent.width
                        wrapMode: Text.Wrap
                        elide: Text.ElideNone
                        text: win.skill ? (win.skill.text || win.skill.error || "") : ""
                    }
                    Repeater {
                        model: win.skill && win.skill.items ? win.skill.items : []
                        delegate: Rectangle {
                            required property var modelData
                            width: content.width
                            height: itemCol.implicitHeight + Theme.s3 * 2
                            radius: Theme.radiusMd
                            color: Theme.bg
                            border.width: 1
                            border.color: modelData.warning ? Theme.warning : Theme.border
                            Column {
                                id: itemCol
                                anchors.left: parent.left; anchors.right: openBtn.left; anchors.top: parent.top
                                anchors.margins: Theme.s3
                                spacing: Theme.s1
                                Txt { text: modelData.n + ". " + modelData.title; font.weight: Font.DemiBold; width: parent.width }
                                Txt { visible: !!modelData.meta; text: modelData.meta || ""; role: "small"; color: Theme.textMuted; width: parent.width }
                                Txt { visible: !!modelData.summary; text: modelData.summary || ""; width: parent.width; wrapMode: Text.Wrap; elide: Text.ElideRight; maximumLineCount: 4 }
                                Row {
                                    visible: !!modelData.warning
                                    spacing: Theme.s1
                                    Icon { name: "warning"; size: Theme.fontSize * 1.2; color: Theme.warning }
                                    Txt { text: modelData.warning || ""; role: "small"; color: Theme.warning; width: itemCol.width - Theme.s6; wrapMode: Text.Wrap; elide: Text.ElideNone }
                                }
                            }
                            Btn {
                                id: openBtn
                                visible: !!modelData.path
                                anchors.right: parent.right; anchors.rightMargin: Theme.s3
                                anchors.verticalCenter: parent.verticalCenter
                                text: qsTr("Open"); variant: "outline"
                                onClicked: { field.text = "open result " + modelData.n; win.submit(); }
                            }
                        }
                    }
                    Txt {
                        visible: win.skill && win.skill.model && win.skill.model.length > 0
                        width: parent.width
                        wrapMode: Text.Wrap
                        elide: Text.ElideNone
                        role: "small"
                        color: Theme.textMuted
                        text: win.skill && win.skill.model ? (win.skill.timing ? qsTr("%1 · total %2 s").arg(win.skill.model.join("; ")).arg((win.skill.timing.total / 1000).toFixed(1)) : win.skill.model.join("; ")) : ""
                    }
                }

                // Permissions in force, with End.
                Column {
                    visible: !win.result && !win.busy && Bus.grants.length > 0
                    width: parent.width
                    spacing: Theme.s2
                    Row {
                        spacing: Theme.s2
                        Icon { name: "shield"; size: Theme.fontSize * 1.4; color: Theme.accent }
                        Txt { text: qsTr("The assistant may read (read only)"); font.weight: Font.DemiBold }
                    }
                    Repeater {
                        model: Bus.grants
                        delegate: Row {
                            required property var modelData
                            spacing: Theme.s3
                            Txt { text: qsTr("%1: %2").arg(modelData.kind === "folder" ? qsTr("folder") : (modelData.kind === "mailbox" ? qsTr("mailbox") : qsTr("site"))).arg(modelData.label); anchors.verticalCenter: parent.verticalCenter }
                            Txt { text: win.grantLeft(modelData); role: "small"; color: Theme.textMuted; anchors.verticalCenter: parent.verticalCenter }
                            Btn { text: qsTr("End"); variant: "outline"; onClicked: Bus.revokeGrant(modelData.id) }
                        }
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
