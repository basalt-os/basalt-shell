import QtQuick
import QtQuick.Effects

// A raised card: surface color, border, rounded corners and a soft shadow
// (the shadow is skipped when motion is reduced: weak hardware).
Item {
    id: s
    default property alias content: inner.data
    property color color: Theme.surfaceAlt
    property real radius: Theme.radiusLg
    property bool shadow: true

    RectangularShadow {
        anchors.fill: bg
        visible: s.shadow && Theme.animate && Theme.shadowStrength > 0
        radius: bg.radius
        blur: Theme.shadowBlur
        offset.y: Theme.s1
        color: Qt.rgba(0, 0, 0, Theme.shadowStrength)
    }
    Rectangle {
        id: bg
        anchors.fill: parent
        radius: s.radius
        color: s.color
        border.width: 1
        border.color: Theme.border
        Behavior on color { ColorAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        Behavior on radius { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }
    }
    Item {
        id: inner
        anchors.fill: parent
    }
}
