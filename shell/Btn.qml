import QtQuick

// Button: icon, text or both. Variants: ghost (default), primary, danger, outline.
Rectangle {
    id: b
    property string icon: ""
    property string text: ""
    property string variant: "ghost"
    property bool active: false
    property real iconSize: Theme.fontSize * 1.55
    property string e2e: ""
    property alias hovered: ma.containsMouse
    property bool focusable: false
    signal clicked()
    signal rightClicked()

    activeFocusOnTab: focusable
    implicitHeight: Math.max(iconSize, label.implicitHeight) + Theme.s2 * 1.5
    implicitWidth: row.implicitWidth + (b.text !== "" ? Theme.s4 : Theme.s2 * 1.5)
    radius: Theme.radiusSm
    color: {
        if (variant === "primary") return ma.pressed ? Qt.darker(Theme.accent, 1.15) : (ma.containsMouse ? Qt.lighter(Theme.accent, 1.08) : Theme.accent);
        if (variant === "danger") return ma.pressed ? Qt.darker(Theme.danger, 1.15) : Theme.danger;
        if (active) return Theme.accentSoft;
        return ma.pressed ? Theme.pressed : (ma.containsMouse ? Theme.hover : "transparent");
    }
    border.width: variant === "outline" || activeFocus ? 1 : 0
    border.color: activeFocus ? Theme.accent : Theme.border
    readonly property color fg: (variant === "primary") ? Theme.accentText : (variant === "danger" ? "#ffffff" : (active ? Theme.accent : Theme.text))

    Behavior on color { ColorAnimation { duration: Theme.fast; easing.type: Theme.easing } }

    Row {
        id: row
        anchors.centerIn: parent
        spacing: Theme.s2
        Icon {
            visible: b.icon !== ""
            name: b.icon
            size: b.iconSize
            color: b.fg
            anchors.verticalCenter: parent.verticalCenter
        }
        Txt {
            id: label
            visible: b.text !== ""
            text: b.text
            color: b.fg
            font.weight: b.variant === "primary" || b.variant === "danger" ? Font.DemiBold : Font.Medium
            anchors.verticalCenter: parent.verticalCenter
        }
    }
    MouseArea {
        id: ma
        anchors.fill: parent
        hoverEnabled: true
        acceptedButtons: Qt.LeftButton | Qt.RightButton
        cursorShape: Qt.PointingHandCursor
        onClicked: mouse => { if (mouse.button === Qt.RightButton) b.rightClicked(); else b.clicked(); }
    }
    Keys.onReturnPressed: b.clicked()
    Keys.onEnterPressed: b.clicked()
    Keys.onSpacePressed: b.clicked()
}
