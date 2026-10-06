import QtQuick
import QtQuick.Layouts
import Quickshell
import Quickshell.Widgets

// One notification: app icon, summary, body, actions, dismiss.
Rectangle {
    id: c
    property var entry
    property bool popup: false
    implicitHeight: col.implicitHeight + Theme.s3 * 2
    radius: Theme.radiusMd
    color: popup ? Theme.surfaceAlt : Theme.alpha(Theme.text, 0.04)
    border.width: 1
    border.color: entry && entry.urgency === "critical" ? Theme.danger : Theme.border
    Accessible.role: Accessible.Notification
    Accessible.name: entry ? (entry.appName ? entry.appName + ": " : "") + entry.summary : ""
    Accessible.description: entry ? entry.body : ""

    RowLayout {
        id: col
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.margins: Theme.s3
        spacing: Theme.s3

        Item {
            Layout.alignment: Qt.AlignTop
            implicitWidth: Theme.fontLarge * 2.4
            implicitHeight: implicitWidth
            Rectangle {
                anchors.fill: parent
                radius: Theme.radiusSm
                color: Theme.accentSoft
                visible: !appImg.visible
                Icon { anchors.centerIn: parent; name: c.entry && c.entry.agent ? "spark" : "bell"; color: Theme.accent; size: parent.width * 0.55 }
            }
            IconImage {
                id: appImg
                anchors.fill: parent
                visible: status === Image.Ready
                source: c.entry ? (c.entry.image || (c.entry.appIcon ? Quickshell.iconPath(c.entry.appIcon, true) : "")) : ""
            }
        }
        ColumnLayout {
            Layout.fillWidth: true
            spacing: 2
            RowLayout {
                Layout.fillWidth: true
                Txt { text: c.entry ? c.entry.appName : ""; role: "small"; color: Theme.textMuted; Layout.fillWidth: true }
                Txt { text: c.entry ? Qt.formatDateTime(c.entry.time, "HH:mm") : ""; role: "small"; color: Theme.textMuted }
                Btn { icon: "close"; iconSize: Theme.fontSize * 1.1; e2e: "notification-dismiss"; accessibleName: Tr.t("Dismiss notification"); onClicked: c.popup ? Notifs.dismissPopup(c.entry.key) : Notifs.remove(c.entry.key) }
            }
            Txt { Layout.fillWidth: true; text: c.entry ? c.entry.summary : ""; font.weight: Font.DemiBold; wrapMode: Text.Wrap; elide: Text.ElideNone; maximumLineCount: 2 }
            Txt { Layout.fillWidth: true; visible: text !== ""; text: c.entry ? c.entry.body : ""; color: Theme.textMuted; wrapMode: Text.Wrap; elide: Text.ElideRight; maximumLineCount: c.popup ? 3 : 6; textFormat: Text.PlainText }
            Row {
                visible: c.entry && c.entry.actions && c.entry.actions.length > 0
                spacing: Theme.s2
                topPadding: Theme.s1
                Repeater {
                    model: c.entry ? c.entry.actions : []
                    delegate: Btn { required property var modelData; text: modelData.text; variant: "outline"; onClicked: { modelData.ref.invoke(); Notifs.remove(c.entry.key); } }
                }
            }
        }
    }
}
