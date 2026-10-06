import QtQuick

// Button: icon, text or both. Variants: ghost (default), primary, chip
// (a quiet pill on the wallpaper). e2e names the button for the tests
// (objectName "e2e:<name>", the data-e2e of the web apps); label is what
// a screen reader says when the button shows only an icon. Tab reaches
// it; Return, Enter and Space press it; the focus ring is GFocusRing.
Rectangle {
    id: b
    property string icon: ""
    property string text: ""
    property string label: text
    property string variant: "ghost"
    property bool active: false
    property bool round: false
    property real iconSize: G.fontSize * 1.6
    property string e2e: ""
    property alias hovered: ma.containsMouse
    signal clicked()

    objectName: e2e !== "" ? "e2e:" + e2e : ""
    activeFocusOnTab: visible && enabled
    implicitHeight: Math.max(iconSize, lbl.implicitHeight) + G.s2 * 1.6
    implicitWidth: round ? implicitHeight : row.implicitWidth + (b.text !== "" ? G.s4 + G.s1 : G.s2 * 1.6)
    radius: round ? height / 2 : (variant === "chip" ? height / 2 : G.radiusSm)
    opacity: enabled ? 1 : 0.5
    color: {
        if (variant === "primary") return ma.pressed ? Qt.darker(G.accent, 1.15) : (ma.containsMouse ? Qt.lighter(G.accent, 1.08) : G.accent);
        if (active) return G.accentSoft;
        if (variant === "chip") return ma.pressed ? G.pressed : (ma.containsMouse ? G.hover : G.alpha(G.surface, G.highContrast ? 1 : 0.55));
        return ma.pressed ? G.pressed : (ma.containsMouse ? G.hover : "transparent");
    }
    border.width: variant === "chip" && G.highContrast ? 1 : 0
    border.color: G.border
    readonly property color fg: variant === "primary" ? G.accentText : (active ? G.accent : G.text)
    Behavior on color { ColorAnimation { duration: G.fast; easing.type: G.easing } }

    Accessible.role: Accessible.Button
    Accessible.name: b.label
    Accessible.onPressAction: b.clicked()
    Accessible.focusable: true
    Accessible.focused: activeFocus

    Row {
        id: row
        anchors.centerIn: parent
        spacing: G.s2
        GIcon {
            visible: b.icon !== ""
            name: b.icon
            size: b.iconSize
            color: b.fg
            anchors.verticalCenter: parent.verticalCenter
        }
        GTxt {
            id: lbl
            visible: b.text !== ""
            text: b.text
            color: b.fg
            font.weight: b.variant === "primary" ? Font.DemiBold : Font.Medium
            anchors.verticalCenter: parent.verticalCenter
        }
    }
    MouseArea {
        id: ma
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onClicked: b.clicked()
    }
    Keys.onReturnPressed: b.clicked()
    Keys.onEnterPressed: b.clicked()
    Keys.onSpacePressed: b.clicked()
    GFocusRing { target: b }
}
