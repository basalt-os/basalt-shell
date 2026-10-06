import QtQuick

// The base of every control that does something when pressed: buttons,
// tiles, chips, swatches, menu rows, panel entries. It brings the whole
// keyboard and accessibility contract, so a new control gets it by
// using Pressable (or Btn) instead of a Rectangle with a MouseArea:
//
// - Tab reaches it (focusable), or arrows inside a NavRow, NavColumn or
//   NavFlow, where the group keeps one Tab stop (Nav.isStop);
// - Return, Enter and Space press it; Menu and Shift+F10 open its
//   context menu (contextMenu), when it has one;
// - the focus ring shows when it has the keyboard focus and the last
//   input was the keyboard (FocusRing, Ui.focusVisible);
// - a click focuses it without the ring;
// - screen readers get its role, name, description, and checked state for
//   toggles and choices (checkable, checked).
//
// Children go on top of the pointer area; a child MouseArea (a close
// button inside an entry) takes its own clicks.
Rectangle {
    id: p
    property bool focusable: true
    property string e2e: ""
    // Names the control for Nav.find when e2e is shared (list entries).
    property string navKey: ""
    property string text: ""
    property string accessibleName: ""
    property string accessibleDescription: ""
    property int accessibleRole: Accessible.Button
    // A toggle or one choice of a group: screen readers say on or off.
    property bool checkable: false
    property bool checked: false
    // Selected in its group (the group's Tab stop when none was focused).
    property bool active: false
    property bool contextMenu: false
    property alias hovered: ma.containsMouse
    property alias pressed: ma.pressed
    property alias acceptedButtons: ma.acceptedButtons
    property alias cursorShape: ma.cursorShape
    // Nav reads it: can this control take the keyboard now?
    readonly property bool navigable: focusable && visible && enabled
    readonly property bool focusVisible: activeFocus && Ui.focusVisible
    signal clicked()
    signal rightClicked()
    signal middleClicked()
    signal menuRequested()

    objectName: e2e !== "" ? "e2e:" + e2e : ""
    color: "transparent"
    activeFocusOnTab: navigable && Nav.isStop(parent, p)

    Accessible.role: accessibleRole
    Accessible.name: accessibleName !== "" ? accessibleName : text
    Accessible.description: accessibleDescription
    Accessible.focusable: focusable
    Accessible.focused: activeFocus
    Accessible.checkable: checkable
    Accessible.checked: checked
    Accessible.onPressAction: p.clicked()
    Accessible.onToggleAction: p.clicked()

    onActiveFocusChanged: {
        Ui.noteFocused(p.navKey || p.e2e || p.Accessible.name, activeFocus);
        if (activeFocus) { Nav.noteFocus(p); Nav.reveal(p); }
    }

    MouseArea {
        id: ma
        anchors.fill: parent
        hoverEnabled: true
        acceptedButtons: Qt.LeftButton | Qt.RightButton
        cursorShape: Qt.PointingHandCursor
        onPressed: mouse => {
            Ui.focusVisible = false;
            if (p.focusable && mouse.button === Qt.LeftButton) p.forceActiveFocus(Qt.MouseFocusReason);
        }
        onClicked: mouse => {
            if (mouse.button === Qt.RightButton) { p.rightClicked(); if (p.contextMenu) p.menuRequested(); }
            else if (mouse.button === Qt.MiddleButton) p.middleClicked();
            else p.clicked();
        }
    }

    Keys.onPressed: e => {
        Ui.focusVisible = true;
        const plain = !(e.modifiers & (Qt.ControlModifier | Qt.AltModifier | Qt.MetaModifier));
        if (plain && !e.isAutoRepeat && (e.key === Qt.Key_Return || e.key === Qt.Key_Enter || e.key === Qt.Key_Space)) {
            p.clicked();
            e.accepted = true;
            return;
        }
        if (p.contextMenu && (e.key === Qt.Key_Menu || (e.key === Qt.Key_F10 && (e.modifiers & Qt.ShiftModifier)))) {
            p.menuRequested();
            e.accepted = true;
            return;
        }
        e.accepted = false;
    }

    FocusRing { target: p }
}
