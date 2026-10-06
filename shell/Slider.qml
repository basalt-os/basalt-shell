import QtQuick

// Horizontal slider in the theme's style. Keyboard: Left and Down lower
// it by one step, Right and Up raise it, Page Up and Page Down by a tenth
// of the range, Home and End to the ends; every change is reported as
// moved(value), as a drag is.
Item {
    id: s
    property real from: 0
    property real to: 1
    property real value: 0
    property real stepSize: 0
    property string e2e: ""
    property string accessibleName: ""
    property bool focusable: true
    readonly property bool navigable: focusable && visible && enabled
    signal moved(real value)
    implicitHeight: Theme.fontSize * 2.4
    implicitWidth: 200
    objectName: e2e !== "" ? "e2e:" + e2e : ""
    activeFocusOnTab: navigable

    readonly property real frac: to > from ? Math.max(0, Math.min(1, (value - from) / (to - from))) : 0
    // One key press: the step, or a fiftieth of the range without one.
    readonly property real keyStep: stepSize > 0 ? stepSize : (to - from) / 50

    function nudge(v) {
        let n = Math.max(s.from, Math.min(s.to, v));
        if (s.stepSize > 0) n = Math.round(n / s.stepSize) * s.stepSize;
        n = Math.round(n * 1000) / 1000;
        if (n !== s.value) s.moved(n);
    }

    Accessible.role: Accessible.Slider
    Accessible.name: accessibleName
    Accessible.focusable: true
    Accessible.focused: activeFocus
    Accessible.onIncreaseAction: s.nudge(s.value + s.keyStep)
    Accessible.onDecreaseAction: s.nudge(s.value - s.keyStep)

    onActiveFocusChanged: {
        Ui.noteFocused(s.e2e || s.accessibleName, activeFocus);
        if (activeFocus) Nav.reveal(s);
    }
    Keys.onPressed: e => {
        Ui.focusVisible = true;
        const rtl = Qt.application.layoutDirection === Qt.RightToLeft;
        const big = Math.max(s.keyStep, (s.to - s.from) / 10);
        switch (e.key) {
        case Qt.Key_Left: s.nudge(s.value + (rtl ? s.keyStep : -s.keyStep)); break;
        case Qt.Key_Right: s.nudge(s.value + (rtl ? -s.keyStep : s.keyStep)); break;
        case Qt.Key_Down: s.nudge(s.value - s.keyStep); break;
        case Qt.Key_Up: s.nudge(s.value + s.keyStep); break;
        case Qt.Key_PageDown: s.nudge(s.value - big); break;
        case Qt.Key_PageUp: s.nudge(s.value + big); break;
        case Qt.Key_Home: s.nudge(s.from); break;
        case Qt.Key_End: s.nudge(s.to); break;
        default: e.accepted = false; return;
        }
        e.accepted = true;
    }

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
        // The focus ring goes around the knob, the part that moves.
        FocusRing { target: knob; shown: s.activeFocus && Ui.focusVisible }
    }
    MouseArea {
        anchors.fill: parent
        cursorShape: Qt.PointingHandCursor
        function upd(mx) {
            let v = s.from + Math.max(0, Math.min(1, (mx - knob.width / 2) / (s.width - knob.width))) * (s.to - s.from);
            if (s.stepSize > 0) v = Math.round(v / s.stepSize) * s.stepSize;
            s.moved(v);
        }
        onPressed: mouse => {
            Ui.focusVisible = false;
            if (s.focusable) s.forceActiveFocus(Qt.MouseFocusReason);
            upd(mouse.x);
        }
        onPositionChanged: mouse => { if (pressed) upd(mouse.x); }
    }
}
