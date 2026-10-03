import QtQuick

// Horizontal slider in the theme's style.
Item {
    id: s
    property real from: 0
    property real to: 1
    property real value: 0
    property real stepSize: 0
    signal moved(real value)
    implicitHeight: Theme.fontSize * 2.4
    implicitWidth: 200

    readonly property real frac: to > from ? Math.max(0, Math.min(1, (value - from) / (to - from))) : 0

    Rectangle {
        id: track
        anchors.verticalCenter: parent.verticalCenter
        width: parent.width
        height: Theme.unit * 1.5
        radius: height / 2
        color: Theme.hover
        Rectangle {
            width: knob.x + knob.width / 2
            height: parent.height
            radius: parent.radius
            color: Theme.accent
        }
    }
    Rectangle {
        id: knob
        width: Theme.fontSize * 1.6
        height: width
        radius: Math.min(width / 2, Theme.radiusMd)
        color: Theme.surfaceAlt
        border.width: 2
        border.color: Theme.accent
        anchors.verticalCenter: parent.verticalCenter
        x: s.frac * (s.width - width)
    }
    MouseArea {
        anchors.fill: parent
        cursorShape: Qt.PointingHandCursor
        function upd(mx) {
            let v = s.from + Math.max(0, Math.min(1, (mx - knob.width / 2) / (s.width - knob.width))) * (s.to - s.from);
            if (s.stepSize > 0) v = Math.round(v / s.stepSize) * s.stepSize;
            s.moved(v);
        }
        onPressed: mouse => upd(mouse.x)
        onPositionChanged: mouse => { if (pressed) upd(mouse.x); }
    }
}
