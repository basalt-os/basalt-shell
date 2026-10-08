import QtQuick
import QtQuick.Layouts
import Quickshell
import Quickshell.Wayland
import Quickshell.Widgets

// Side drawer with two tabs: notifications, and the activity feed (what
// agents asked, what the person decided, what ran; plus the system
// assistant's open proposals). Keyboard: it takes the keyboard while
// open; Left and Right switch the tabs, Tab goes on to the notifications'
// buttons or the feed (Up, Down, Page Up, Page Down scroll it), Escape
// closes it and returns to where it was opened.
PanelWindow {
    id: win
    visible: Ui.drawer || card.opacity > 0
    anchors { top: true; bottom: true; left: true; right: true }
    color: "transparent"
    exclusionMode: ExclusionMode.Normal
    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.namespace: "basalt-drawer"
    WlrLayershell.keyboardFocus: Ui.drawer && !Ui.modal ? WlrKeyboardFocus.Exclusive : WlrKeyboardFocus.None
    Connections {
        target: Ui
        function onDrawerChanged() {
            if (Ui.drawer) Qt.callLater(() => { Nav.initial(drawerCol, Ui.focusKey || ("drawer-tab-" + Ui.drawerTab)); Ui.focusKey = ""; });
        }
    }

    MouseArea { anchors.fill: parent; onClicked: Ui.drawer = false }

    function icon(t) {
        return ({ request: "spark", confirm: "check", apply: "check", done: "check", decline: "close", expire: "info", fail: "warning",
                  refuse: "shield", ask: "search", start: "power", voice: "mic", skill: "search", edit: "list" })[t] || "info";
    }
    function tint(t) {
        return ({ apply: Theme.success, confirm: Theme.success, done: Theme.success, decline: Theme.textMuted, fail: Theme.danger, refuse: Theme.warning,
                  expire: Theme.textMuted, request: Theme.accent, edit: Theme.accent })[t] || Theme.text;
    }
    // What a record means, for the person (the type stays in the log).
    function label(r) {
        return ({ request: qsTr("Waiting for you"), apply: qsTr("You confirmed"), done: qsTr("Done"), decline: qsTr("You declined"),
                  expire: qsTr("Nobody confirmed it"), fail: qsTr("Failed"), refuse: qsTr("Refused"), voice: qsTr("Voice"),
                  skill: qsTr("The assistant read"), edit: qsTr("You edited it"), confirm: qsTr("You confirmed") })[r.type] || r.type;
    }
    // The exact preview of an acting step, one line.
    function detail(r) {
        const pv = r.data && r.data.previews && r.data.previews.length > 0 ? r.data.previews[0] : null;
        if (pv && pv.kind === "mail") return qsTr("To %1 · %2 · %3 characters, no attachment").arg(pv.to).arg(pv.subject).arg(pv.chars);
        if (pv && pv.kind === "files") return qsTr("Files: %1").arg((pv.moves || []).map(m => m.from + " → " + m.to).join("; "));
        if (pv && pv.kind === "dictation") return qsTr("Into %1").arg(pv.app);
        if (r.type === "skill" && r.data && r.data.warnings && r.data.warnings.length > 0) return r.data.warnings[0];
        return "";
    }
    function ago(ts) {
        const d = (new Date() - new Date(ts)) / 1000;
        if (d < 60) return "now";
        if (d < 3600) return Math.floor(d / 60) + " min";
        if (d < 86400) return Math.floor(d / 3600) + " h";
        return Qt.formatDateTime(new Date(ts), "d MMM");
    }

    // 8 px from the panel's edge, the side and the bottom (the layer
    // already starts after the panel's exclusive zone).
    Surface {
        id: card
        width: Math.min(400, parent.width - Theme.popoverGap * 2)
        anchors.top: parent.top
        anchors.bottom: parent.bottom
        anchors.topMargin: Theme.popoverGap
        anchors.bottomMargin: Theme.popoverGap
        x: parent.width - (Ui.drawer ? width + Theme.popoverGap : width * 0.25)
        opacity: Ui.drawer ? 1 : 0
        Behavior on x { NumberAnimation { duration: Theme.slow; easing.type: Theme.easing } }
        Behavior on opacity { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        MouseArea { anchors.fill: parent }
        Keys.onEscapePressed: Ui.dismiss()

        ColumnLayout {
            id: drawerCol
            anchors.fill: parent
            anchors.margins: Theme.s4
            spacing: Theme.s3

            RowLayout {
                Layout.fillWidth: true
                // The tabs: the selected one follows the arrows.
                NavRow {
                    spacing: Theme.s1
                    Accessible.role: Accessible.PageTabList
                    Btn {
                        text: "Notifications"; variant: "tab"; e2e: "drawer-tab-notifications"; accessibleRole: Accessible.PageTab; checkable: true
                        active: Ui.drawerTab === "notifications"
                        onClicked: Ui.drawerTab = "notifications"
                        onActiveFocusChanged: if (activeFocus && Ui.focusVisible) Ui.drawerTab = "notifications"
                    }
                    Btn {
                        text: "Activity"; variant: "tab"; e2e: "drawer-tab-activity"; accessibleRole: Accessible.PageTab; checkable: true
                        active: Ui.drawerTab === "activity"
                        onClicked: { Ui.drawerTab = "activity"; Bus.refreshAssistant(); }
                        onActiveFocusChanged: if (activeFocus && Ui.focusVisible && Ui.drawerTab !== "activity") { Ui.drawerTab = "activity"; Bus.refreshAssistant(); }
                    }
                }
                Item { Layout.fillWidth: true }
                Btn { visible: Ui.drawerTab === "notifications" && Notifs.list.length > 0; text: "Clear"; variant: "outline"; e2e: "drawer-clear"; accessibleName: Tr.t("Clear all notifications"); onClicked: Notifs.clear() }
            }

            // Notifications.
            ListView {
                visible: Ui.drawerTab === "notifications"
                Layout.fillWidth: true
                Layout.fillHeight: true
                clip: true
                spacing: Theme.s2
                model: Notifs.list
                delegate: NotificationCard { width: ListView.view.width; entry: modelData; required property var modelData }
                Txt { anchors.centerIn: parent; visible: Notifs.list.length === 0; text: "No notifications"; color: Theme.textMuted }
                add: Transition { NumberAnimation { property: "opacity"; from: 0; to: 1; duration: Theme.normal } }
            }

            // Activity.
            ColumnLayout {
                visible: Ui.drawerTab === "activity"
                Layout.fillWidth: true
                Layout.fillHeight: true
                spacing: Theme.s2

                Repeater {
                    model: Bus.assistantPending
                    // A proposal waits for a decision (warning colors); a
                    // report with nothing to apply is only read (neutral,
                    // not counted on the panel) and can be dismissed.
                    delegate: Rectangle {
                        id: apCard
                        required property var modelData
                        readonly property bool report: modelData.report_only === true
                        property string note: ""
                        Layout.fillWidth: true
                        implicitHeight: apRow.implicitHeight + Theme.s3 * 2
                        radius: Theme.radiusMd
                        color: report ? Theme.alpha(Theme.textMuted, 0.08) : Theme.alpha(Theme.warning, 0.12)
                        border.width: 1; border.color: report ? Theme.alpha(Theme.textMuted, 0.3) : Theme.alpha(Theme.warning, 0.5)
                        RowLayout {
                            id: apRow
                            anchors.fill: parent
                            anchors.margins: Theme.s3
                            Icon { name: apCard.report ? "info" : "shield"; color: apCard.report ? Theme.textMuted : Theme.warning }
                            Column {
                                Layout.fillWidth: true
                                Txt { width: parent.width; text: modelData.title; font.weight: Font.Medium; wrapMode: Text.Wrap; elide: Text.ElideNone }
                                Txt {
                                    width: parent.width; wrapMode: Text.Wrap; elide: Text.ElideNone
                                    text: apCard.note !== "" ? apCard.note
                                        : (apCard.report ? Tr.t("A report for you to read: nothing to decide.") : Tr.t("The system assistant proposes a change (%1).").arg(modelData.id))
                                    role: "small"; color: Theme.textMuted
                                }
                            }
                            Btn {
                                text: apCard.report ? Tr.t("Read") : Tr.t("Review")
                                variant: "outline"
                                e2e: "assistant-review"
                                onClicked: { Ui.commandText = "show " + modelData.id; Bus.call("assistant.show", { id: modelData.id }, () => {}); Ui.open("commandbar", ""); }
                            }
                            Btn {
                                visible: apCard.report
                                text: Tr.t("Dismiss")
                                variant: "outline"
                                e2e: "assistant-dismiss"
                                accessibleName: Tr.t("Dismiss this report")
                                // Closing it changes the assistant's own records
                                // (as root): an administrator authenticates.
                                onClicked: {
                                    apCard.note = Tr.t("Waiting for authentication.");
                                    Bus.call("assistant.ignore", { id: modelData.id }, (ok, res) => {
                                        apCard.note = ok && res.ok ? "" : Tr.t("Not dismissed.");
                                        Bus.refreshAssistant();
                                    });
                                }
                            }
                        }
                    }
                }

                ListView {
                    id: feed
                    Layout.fillWidth: true
                    Layout.fillHeight: true
                    clip: true
                    spacing: Theme.s1
                    // Read with the keyboard: a stop of its own that scrolls.
                    activeFocusOnTab: count > 0
                    Accessible.role: Accessible.List
                    Accessible.name: Tr.t("Activity")
                    Keys.onPressed: e => Nav.scrollKey(feed, e)
                    FocusRing { parent: feed; target: feed; inside: true }
                    // Bookkeeping records stay in the log, not in the feed.
                    model: Bus.activity.slice().reverse().filter(r => r.type !== "start" && r.text !== "microphone closed" && r.text !== "sender names read for speech recognition")
                    delegate: Item {
                        required property var modelData
                        width: feed.width
                        height: actCol.implicitHeight + Theme.s3
                        Rectangle {
                            anchors.fill: parent
                            radius: Theme.radiusSm
                            color: actMa.containsMouse ? Theme.hover : "transparent"
                        }
                        Icon {
                            id: actIcon
                            x: Theme.s2; y: Theme.s2
                            name: win.icon(modelData.type)
                            color: win.tint(modelData.type)
                            size: Theme.fontSize * 1.5
                        }
                        Column {
                            id: actCol
                            anchors.left: actIcon.right
                            anchors.leftMargin: Theme.s2
                            anchors.right: parent.right
                            anchors.rightMargin: Theme.s2
                            y: Theme.s1 + 2
                            Txt { width: parent.width; text: modelData.text; wrapMode: Text.Wrap; elide: Text.ElideNone; maximumLineCount: 3 }
                            Txt {
                                visible: text !== ""
                                width: parent.width
                                role: "small"
                                color: modelData.type === "skill" ? Theme.warning : Theme.text
                                wrapMode: Text.Wrap; elide: Text.ElideNone; maximumLineCount: 3
                                text: win.detail(modelData)
                            }
                            Txt {
                                width: parent.width
                                role: "small"
                                color: Theme.textMuted
                                text: win.label(modelData) + "  ·  " + modelData.actor + "  ·  " + win.ago(modelData.time) + "  ·  #" + modelData.seq
                            }
                        }
                        MouseArea { id: actMa; anchors.fill: parent; hoverEnabled: true }
                    }
                    Txt { anchors.centerIn: parent; visible: feed.count === 0; text: "Nothing yet"; color: Theme.textMuted }
                }
                Txt {
                    Layout.fillWidth: true
                    role: "small"
                    color: Theme.textMuted
                    wrapMode: Text.Wrap
                    text: "Hash-chained log: basalt-shell audit verify"
                }
            }
        }
    }
}
