import QtQuick
import Quickshell
import Quickshell.Wayland

// "Ask the system": free text goes to the daemon, which understands it
// (local model if configured, else rules) and answers with a proposal of
// typed actions, or runs a read-only question through the system
// assistant. Nothing is applied until the person presses Apply.
// Keyboard: the text field has the focus; Tab goes on to the suggestions
// (Left, Right), the proposal's buttons and the answer (Up, Down, Page Up,
// Page Down scroll a long report); Escape closes it from anywhere. A
// proposal (a change, a permission, a reply, the system assistant's Apply)
// moves the focus to its negative button (Ignore, Don't allow, Discard),
// so a second Return declines and never applies anything.
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
    readonly property string actName: proposal !== null && proposal.calls && proposal.calls.length > 0 ? proposal.calls[0].action : ""
    readonly property bool isMail: actName === "mail.send"
    readonly property bool isMove: actName === "files.move"
    property string retry: ""        // run again after a grant is applied
    property var voiceData: null     // a spoken request's answer, shown when the bar opens
    property var appAsk: null        // an app's question being shown ({ id, text, from })

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
        // An app's question, unchanged: the daemon runs it in its narrow
        // mode for apps (never as the person's own words).
        // It runs once; asking again is the person's own request.
        const fromApp = appAsk !== null && !appAsk.used && t === appAsk.text;
        const appId = fromApp ? appAsk.id : "";
        if (fromApp) appAsk = Object.assign({}, appAsk, { used: true });
        const ask = fromApp ? (cb) => Bus.call("ask", { app: appId }, cb) : (cb) => Bus.ask(t, cb);
        ask((ok, res) => {
            busy = false;
            win.result = ok ? res : { kind: "error", error: res };
            win.retry = ok && res.retry ? res.retry : "";
            // A rule at the approval gate may have decided it already.
            if (ok && res.proposal && res.proposal.gate_mode === "enforce")
                Bus.call("proposal", { id: res.proposal.id }, (pok, p) => { if (pok && p.status !== "pending" && win.proposal && win.proposal.id === p.id) win._done(true, p); });
        });
    }
    // A proposal that ended without a click here (a rule of the person's
    // allowed or refused it at the approval gate, or it expired) shows its
    // outcome as if decided here.
    Connections {
        target: Bus
        function onProposalChanged(p) {
            if (win.proposal && p.id === win.proposal.id && p.status !== "pending" && !win.busy) win._done(true, p);
        }
    }
    function _done(ok, res) {
        busy = false;
        statusOk = ok && (res.status === "applied" || res.status === "declined");
        const applied = win.isMail ? qsTr("Sent.") : (win.isMove ? qsTr("Done. Say \"undo\" to put the files back.") : qsTr("Applied."));
        const declined = win.isMail ? qsTr("Not sent. The draft was discarded.") : qsTr("Ignored. Nothing changed.");
        status = ok ? (res.status === "applied" ? applied : (res.status === "declined" ? declined : res.status + (res.error ? ": " + res.error : "")))
                    : qsTr("Failed: %1").arg(res);
        if (ok && res.status === "applied" && String(res.decided_by || "").indexOf("gate:rule:") === 0)
            status = Tr.t("Done: one of your rules allowed it.");
        result = null;
        if (ok && res.status === "applied" && win.retry !== "") {
            // A permission the request needed: run the request again.
            field.text = win.retry; win.retry = "";
            status = "";
            win.submit();
            return;
        }
        win.retry = "";
        if (ok && res.status === "applied" && !win.isMail && !win.isMove) closeTimer.restart();
    }
    function decide(approve) {
        if (!proposal) return;
        busy = true;
        const done = (ok, res) => win._done(ok, res);
        if (approve && isMail) Bus.decideEdited(proposal.id, actionPreview.edits(), done);
        else Bus.decide(proposal.id, approve, done);
    }
    function assistantDecide(apply) {
        if (!assist) return;
        busy = true;
        status = apply ? "Waiting for authentication..." : "";
        const handle = (ok, res) => {
            busy = false;
            statusOk = ok && res.ok;
            status = (statusOk ? (apply ? "Applied by the system assistant." : "Ignored.") : "Not applied.") ;
            if (ok && res.output) win.result = { kind: "system", text: res.output };
            Bus.refreshAssistant();
        };
        if (apply) Bus.assistantApply(assist.id, assist.code, handle);
        else Bus.call("assistant.ignore", { id: assist.id }, handle);
    }

    // A proposal that appears takes the focus on its negative button
    // (docs/design.md, Keyboard and focus: approval surfaces open focused
    // on the negative action). Left or Shift+Tab reach the positive one.
    function focusDecline(btn) {
        if (!Ui.commandBar) return;
        Ui.focusVisible = true;
        Qt.callLater(() => { if (btn.visible) btn.forceActiveFocus(Qt.TabFocusReason); });
    }
    onProposalChanged: if (proposal && proposal.status === "pending") focusDecline(propNo)
    onAssistChanged: if (assist) focusDecline(asNo)

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

    // Takes an app's question from Ui (set by Bus on its "ask" event).
    function takeAppAsk() {
        appAsk = Ui.commandAsk;
        Ui.commandAsk = null;
    }
    Connections {
        target: Bus
        function onAskRequested() {
            if (!Ui.commandBar) return;
            win.takeAppAsk();
            field.text = Ui.commandText;
            Ui.commandText = "";
            win.status = "";
            if (!win.busy) win.result = null;
            field.focusInput();
            if (field.text !== "") win.submit();
        }
    }
    // Editing an app's question makes it the person's own request.
    Connections {
        target: field
        function onTextChanged() { if (win.appAsk && field.text !== win.appAsk.text) win.appAsk = null; }
    }

    onVisibleChanged: if (Ui.commandBar) {
        if (voiceData) { showVoice(); field.focusInput(); return; }
        takeAppAsk();
        field.text = Ui.commandText;
        Ui.commandText = "";
        status = "";
        if (!busy) result = null;
        field.focusInput();
        if (field.text !== "") submit();
    }

    MouseArea { anchors.fill: parent; onClicked: Ui.commandBar = false }

    // Under the panel: 8 px from its edge (the layer covers the whole
    // screen, so the panel's zone is counted here), 720 px at most.
    Surface {
        id: card
        width: Math.min(720, parent.width - Theme.popoverGap * 2)
        height: Math.min(content.implicitHeight + Theme.s4 * 2, parent.height - Theme.panelZone - Theme.popoverGap * 2)
        anchors.horizontalCenter: parent.horizontalCenter
        readonly property real slide: Ui.commandBar ? 0 : Theme.s6
        y: Theme.panelPosition === "bottom" ? parent.height - height - Theme.panelZone - Theme.popoverGap + slide
                                            : Theme.panelZone + Theme.popoverGap - slide
        opacity: Ui.commandBar ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        Behavior on y { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        Behavior on height { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        MouseArea { anchors.fill: parent }
        // Escape closes, from the field or any button.
        Keys.onEscapePressed: Ui.dismiss()

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
                    e2e: "commandbar-field"
                    accessibleName: Tr.t("Ask the system")
                    placeholder: "Ask the system: \"make it darker with rounder corners\", \"why nginx\""
                    onAccepted: win.submit()
                }

                // Where an app's question came from.
                Row {
                    visible: win.appAsk !== null
                    spacing: Theme.s2
                    Icon { name: "spark"; color: Theme.textMuted; size: Theme.fontSmall * 1.4; anchors.verticalCenter: parent.verticalCenter }
                    Txt {
                        role: "small"; color: Theme.textMuted
                        text: win.appAsk ? Tr.t("Asked from %1. It only reads; any change waits for your approval.").arg(win.appAsk.from) : ""
                    }
                }

                NavFlow {
                    width: parent.width
                    spacing: Theme.s2
                    visible: !win.result && !win.busy && win.status === "" && win.appAsk === null
                    Accessible.name: Tr.t("Suggestions")
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
                        Txt { text: win.isGrant ? qsTr("Permission") : (win.isMail ? qsTr("Reply") : (win.isMove ? qsTr("Files") : qsTr("Proposal"))); role: "large"; font.weight: Font.DemiBold }
                        Rectangle {
                            radius: height / 2; height: Theme.fontSize * 1.8; width: tag.implicitWidth + Theme.s3
                            color: Theme.accentSoft
                            anchors.verticalCenter: parent.verticalCenter
                            Txt { id: tag; anchors.centerIn: parent; role: "small"; color: Theme.accent
                                  text: win.proposal ? (win.proposal.backend === "skill" ? (win.isGrant ? qsTr("read only, expires by itself") : (win.isMail ? qsTr("not sent until you press Send") : qsTr("from your request"))) : (win.proposal.backend === "model" ? qsTr("understood by the local model") : qsTr("understood by the fixed phrases"))) : "" }
                        }
                    }
                    Txt {
                        visible: win.proposal && win.proposal.explain
                        text: win.proposal ? win.proposal.explain : ""
                        color: Theme.textMuted
                        width: parent.width
                        wrapMode: Text.Wrap
                    }
                    ProposalView { visible: !win.isMail; width: parent.width; proposal: win.proposal }
                    ActionPreview {
                        id: actionPreview
                        visible: win.isMail || win.isMove
                        width: parent.width
                        proposal: win.proposal
                        warnings: win.skill && win.skill.warnings ? win.skill.warnings : []
                    }
                    Txt {
                        visible: win.result && win.result.unknown && win.result.unknown.length > 0
                        text: win.result && win.result.unknown ? "Not understood: " + win.result.unknown.join(", ") : ""
                        color: Theme.warning; role: "small"
                    }
                    Row {
                        spacing: Theme.s2
                        Btn { id: propYes; text: win.isGrant ? qsTr("Allow") : (win.isMail ? qsTr("Send") : (win.isMove ? qsTr("Confirm") : qsTr("Apply"))); icon: "check"; variant: "primary"; e2e: "proposal-confirm"; KeyNavigation.right: propNo; onClicked: win.decide(true) }
                        Btn { id: propNo; text: win.isGrant ? qsTr("Don't allow") : (win.isMail ? qsTr("Discard") : qsTr("Ignore")); variant: "outline"; e2e: "proposal-decline"; KeyNavigation.left: propYes; onClicked: win.decide(false) }
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
                            Btn { text: qsTr("End"); variant: "outline"; accessibleName: Tr.t("End permission: %1").arg(modelData.label); onClicked: Bus.revokeGrant(modelData.id) }
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
                            id: sysFlick
                            anchors.fill: parent
                            anchors.margins: Theme.s3
                            contentHeight: sysText.implicitHeight
                            clip: true
                            // A long report scrolls from the keyboard.
                            activeFocusOnTab: contentHeight > height
                            Accessible.role: Accessible.StaticText
                            Accessible.name: sysText.text
                            Keys.onPressed: e => Nav.scrollKey(sysFlick, e)
                            FocusRing { parent: sysFlick; target: sysFlick; inside: true }
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
                        Btn { id: asYes; text: "Apply"; icon: "check"; variant: "primary"; e2e: "assistant-apply"; KeyNavigation.right: asNo; onClicked: win.assistantDecide(true) }
                        Btn { id: asNo; text: "Ignore"; variant: "outline"; e2e: "assistant-ignore"; KeyNavigation.left: asYes; onClicked: win.assistantDecide(false) }
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
