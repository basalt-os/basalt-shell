import QtQuick

// Single-line text input in the theme's style. Tab reaches it, and it
// shows the focus ring whenever it has the keyboard (a caret alone is easy
// to lose). Escape is reported (escapePressed) and still goes on to the
// surface, which closes or steps back.
Rectangle {
    id: f
    property alias text: input.text
    property alias input: input
    property string placeholder: ""
    property string icon: ""
    property string e2e: ""
    property string accessibleName: ""
    readonly property bool navigable: visible && enabled
    signal accepted()
    signal edited(string text)
    signal escapePressed()
    signal upPressed()
    signal downPressed()
    // Any other key, before the text input: a handler may take it by
    // setting event.accepted (the launcher's Page Up and Page Down).
    signal keyPressed(var event)
    function focusInput() { input.forceActiveFocus(Qt.TabFocusReason); }
    objectName: e2e !== "" ? "e2e:" + e2e : ""
    implicitHeight: Theme.fontLarge * 2.6
    radius: Theme.radiusMd
    color: Theme.bg
    border.width: 1
    border.color: input.activeFocus ? Theme.accent : Theme.border
    Behavior on border.color { ColorAnimation { duration: Theme.fast } }

    Icon {
        id: ic
        visible: f.icon !== ""
        name: f.icon
        size: Theme.fontLarge * 1.5
        color: Theme.textMuted
        anchors.left: parent.left
        anchors.leftMargin: Theme.s3
        anchors.verticalCenter: parent.verticalCenter
    }
    TextInput {
        id: input
        anchors.left: ic.visible ? ic.right : parent.left
        anchors.leftMargin: Theme.s3
        anchors.right: parent.right
        anchors.rightMargin: Theme.s3
        anchors.verticalCenter: parent.verticalCenter
        color: Theme.text
        selectionColor: Theme.accent
        selectedTextColor: Theme.accentText
        font.family: Theme.fontFamily
        font.pointSize: Theme.fontLarge
        clip: true
        activeFocusOnTab: f.navigable
        selectByMouse: true
        Accessible.role: Accessible.EditableText
        Accessible.name: f.accessibleName !== "" ? f.accessibleName : f.placeholder
        Accessible.passwordEdit: echoMode === TextInput.Password
        onAccepted: f.accepted()
        onTextChanged: f.edited(text)
        onActiveFocusChanged: {
            Ui.noteFocused(f.e2e || Accessible.name, activeFocus);
            if (activeFocus) Nav.reveal(f);
        }
        Keys.onPressed: e => {
            Ui.focusVisible = true;
            if (e.key === Qt.Key_Escape) { f.escapePressed(); e.accepted = false; return; }
            if (e.key === Qt.Key_Up) { f.upPressed(); e.accepted = true; return; }
            if (e.key === Qt.Key_Down) { f.downPressed(); e.accepted = true; return; }
            e.accepted = false;
            f.keyPressed(e);
        }
        MouseArea {
            anchors.fill: parent
            acceptedButtons: Qt.NoButton
            cursorShape: Qt.IBeamCursor
        }
        Txt {
            anchors.fill: parent
            text: f.placeholder
            color: Theme.textMuted
            font.pointSize: Theme.fontLarge
            visible: input.text === "" && !input.preeditText
        }
    }
    FocusRing { target: f; always: true; shown: input.activeFocus }
}
