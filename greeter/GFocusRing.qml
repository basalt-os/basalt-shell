import QtQuick

// The keyboard focus ring of the login screen: 2 px in G.focusRing (3:1
// against the card and the wallpaper's shade; yellow in high contrast),
// 2 px outside the control's edge. The login screen is used with the
// keyboard first and a click does not move the focus, so it shows
// whenever the control has the focus. Put it as the last child.
Rectangle {
    property Item target: parent
    property bool shown: target !== null && target.activeFocus
    readonly property real gap: 4
    anchors.fill: target
    anchors.margins: -gap
    radius: target && target.radius !== undefined && target.radius > 0 ? target.radius + gap : gap
    color: "transparent"
    border.width: 2
    border.color: G.focusRing
    visible: shown
    z: 1000
    enabled: false
}
