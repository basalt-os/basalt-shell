import QtQuick
import Quickshell
import Quickshell.Wayland

// Confirmation sheet for requests from agents (MCP or IPC clients): the
// person sees who asks, every step and the token diff, and confirms or
// declines. The daemon refuses confirmations from anyone but this UI.
PanelWindow {
    id: win
    // The command bar and the voice card show the person's own requests.
    readonly property var queue: Bus.pending.filter(p => p.origin !== "commandbar" && p.origin !== "voice")
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

    onCurrentChanged: if (current !== null) { shown = current; armed = false; armTimer.restart(); confirmBtn.forceActiveFocus(); }

    // The buttons take no input for a moment after a request appears, so
    // a click or a key meant for something else does not answer it.
    property bool armed: false
    Timer { id: armTimer; interval: 700; onTriggered: win.armed = true }
    readonly property var actionNames: shown ? (shown.calls || []).map(c => c.action) : []
    readonly property bool asksControl: actionNames.indexOf("agent.control") >= 0
    readonly property bool asksScreen: actionNames.indexOf("screen.capture") >= 0
    onHasKeyboardChanged: if (hasKeyboard) confirmBtn.forceActiveFocus()

    property int remaining: 0
    Timer {
        interval: 1000; repeat: true; running: win.current !== null
        onTriggered: win.remaining = win.current ? Math.max(0, Math.round((new Date(win.current.expires) - new Date()) / 1000)) : 0
    }

    function decide(approve) {
        if (!current || !armed) return;
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
                    Txt {
                        text: win.asksControl ? "An assistant wants to control your desktop"
                            : (win.asksScreen ? "An assistant wants to see your screen" : "An assistant wants to change your desktop")
                        role: "title"
                    }
                    Txt {
                        text: win.shown ? ("Requested by " + win.shown.actor + " through " + (win.shown.origin === "mcp" ? "MCP" : "local IPC") +
                              (win.queue.length > 1 ? "  (" + (win.queue.length - 1) + " more waiting)" : "")) : ""
                        color: Theme.textMuted; role: "small"
                    }
                }
            }

            ProposalView { width: parent.width; proposal: win.shown }

            Rectangle {
                visible: win.asksControl || win.asksScreen
                width: parent.width
                height: warnTxt.implicitHeight + Theme.s3 * 2
                radius: Theme.radiusMd
                color: Qt.rgba(Theme.warning.r, Theme.warning.g, Theme.warning.b, 0.16)
                border.width: 1
                border.color: Theme.warning
                Txt {
                    id: warnTxt
                    anchors.fill: parent
                    anchors.margins: Theme.s3
                    wrapMode: Text.Wrap
                    elide: Text.ElideNone
                    text: win.asksControl
                        ? "It will see everything on your screens and can type and click in any window until the time runs out. A frame shows while it is in control; stop it any time with the Stop button or Super+Shift+Escape. It cannot answer this kind of request for itself."
                        : "It will receive an image of what is on the screen now, including any private content shown there."
                }
            }

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
                Btn { id: confirmBtn; text: win.asksControl ? "Allow control" : (win.asksScreen ? "Show screenshot" : "Confirm"); icon: "check"; variant: "primary"; focusable: true; opacity: win.armed ? 1 : 0.5; onClicked: win.decide(true); Keys.onEscapePressed: win.decide(false) }
            }
        }
    }
}
