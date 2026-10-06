import QtQuick

// Toggle (design system: 40 by 24, a stronger boundary when off, accent
// fill when on, the state readable by screen readers). Built on
// Pressable, so the keyboard and accessibility contract is the shell's
// own. It does not flip itself: the page asks for the change and the
// state comes back from the system, so the toggle never shows something
// that did not happen.
Pressable {
    id: tg
    property bool busy: false
    property string label: ""
    signal toggled()

    implicitWidth: 40
    implicitHeight: 24
    radius: height / 2
    opacity: enabled ? 1 : 0.45
    color: checked ? Theme.accent : "transparent"
    border.width: checked ? 0 : 1.5
    border.color: Theme.textMuted
    checkable: true
    accessibleRole: Accessible.CheckBox
    accessibleName: label
    accessibleDescription: checked ? Tr.t("On") : Tr.t("Off")
    onClicked: if (enabled && !busy) toggled()

    Behavior on color { ColorAnimation { duration: Theme.fast } }

    Rectangle {
        width: 16; height: 16; radius: 8
        anchors.verticalCenter: parent.verticalCenter
        x: tg.checked ? parent.width - width - 4 : 4
        color: tg.checked ? Theme.accentText : Theme.textMuted
        opacity: tg.busy ? 0.5 : 1
        Behavior on x { NumberAnimation { duration: Theme.fast; easing.type: Theme.easing } }
    }
}
