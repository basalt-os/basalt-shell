import QtQuick
import Quickshell
import Quickshell.Wayland

// Notification popups, top or bottom right (opposite the panel edge is
// avoided: they appear near the panel). Auto-dismiss after a while unless
// critical.
PanelWindow {
    id: win
    visible: Notifs.popups.length > 0
    anchors.top: Theme.panelPosition !== "bottom"
    anchors.bottom: Theme.panelPosition === "bottom"
    anchors.right: true
    implicitWidth: 400
    implicitHeight: col.implicitHeight + Theme.s4
    color: "transparent"
    exclusionMode: ExclusionMode.Normal
    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.namespace: "basalt-notifications"

    Column {
        id: col
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.margins: Theme.s2
        y: Theme.s2
        spacing: Theme.s2
        move: Transition { NumberAnimation { properties: "y"; duration: Theme.normal; easing.type: Theme.easing } }
        add: Transition { NumberAnimation { property: "opacity"; from: 0; to: 1; duration: Theme.normal } }
        Repeater {
            model: Notifs.popups
            delegate: Surface {
                id: pop
                required property var modelData
                width: col.width
                height: card.implicitHeight
                NotificationCard { id: card; anchors.fill: parent; entry: pop.modelData; popup: true }
                Timer {
                    interval: pop.modelData.urgency === "critical" ? 0 : (pop.modelData.urgency === "low" ? 4000 : 7000)
                    running: interval > 0
                    onTriggered: Notifs.dismissPopup(pop.modelData.key)
                }
            }
        }
    }
}
