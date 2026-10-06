import QtQuick

// The keyboard focus ring: 2 px in Theme.focusRing (3:1 against every
// surface), 2 px outside the control's edge, following its corners. It
// shows only while the control has the keyboard focus and the last input
// was the keyboard (Ui.focusVisible), or always (`always`) for text
// fields, where the caret alone is easy to lose. Put it as the last child
// of the control. inside: drawn on the inner edge instead, for areas that
// clip what is outside them (a scrolling list).
Rectangle {
    id: ring
    property Item target: parent
    property bool always: false
    property bool inside: false
    property bool shown: target !== null && target.activeFocus && (always || Ui.focusVisible)
    readonly property real gap: Theme.focusOffset + Theme.focusWidth
    anchors.fill: target
    anchors.margins: inside ? 0 : -gap
    radius: inside ? (target && target.radius !== undefined ? target.radius : 0)
                   : (target && target.radius !== undefined && target.radius > 0 ? target.radius + gap : gap)
    color: "transparent"
    border.width: Theme.focusWidth
    border.color: Theme.focusRing
    visible: shown
    z: 1000
    enabled: false
}
