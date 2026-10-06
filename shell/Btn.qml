import QtQuick

// Button: icon, text or both. Variants: ghost (default), primary, danger,
// outline. Built on Pressable: Tab reaches it, Return and Space press it,
// the focus ring shows for the keyboard, screen readers read its name
// (an icon-only button must set accessibleName). In a row of choices
// (NavRow, NavFlow) set checkable and active, so the selected one is the
// group's Tab stop and screen readers say it is selected.
Pressable {
    id: b
    property string icon: ""
    property string variant: "ghost"
    property real iconSize: Theme.fontSize * 1.55
    property bool alignLeft: false

    checked: active
    implicitHeight: Math.max(iconSize, label.implicitHeight) + Theme.s2 * 1.5
    implicitWidth: row.implicitWidth + (b.text !== "" ? Theme.s4 : Theme.s2 * 1.5)
    radius: Theme.radiusSm
    opacity: enabled ? 1 : 0.5
    color: {
        if (variant === "primary") return pressed ? Qt.darker(Theme.accent, 1.15) : (hovered ? Qt.lighter(Theme.accent, 1.08) : Theme.accent);
        if (variant === "danger") return pressed ? Qt.darker(Theme.danger, 1.15) : Theme.danger;
        if (active) return Theme.accentSoft;
        return pressed ? Theme.pressed : (hovered ? Theme.hover : "transparent");
    }
    border.width: variant === "outline" ? 1 : 0
    border.color: Theme.border
    readonly property color fg: (variant === "primary") ? Theme.accentText : (variant === "danger" ? "#ffffff" : (active ? Theme.accent : Theme.text))

    Behavior on color { ColorAnimation { duration: Theme.fast; easing.type: Theme.easing } }

    Row {
        id: row
        anchors.centerIn: b.alignLeft ? undefined : parent
        anchors.left: b.alignLeft ? parent.left : undefined
        anchors.leftMargin: Theme.s3
        anchors.verticalCenter: parent.verticalCenter
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
}
