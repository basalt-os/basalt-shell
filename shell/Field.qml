import QtQuick

// Single-line text input in the theme's style.
Rectangle {
    id: f
    property alias text: input.text
    property alias input: input
    property string placeholder: ""
    property string icon: ""
    signal accepted()
    signal edited(string text)
    signal escape()
    signal up()
    signal down()
    function focusInput() { input.forceActiveFocus(); }
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
        onAccepted: f.accepted()
        onTextChanged: f.edited(text)
        Keys.onEscapePressed: f.escape()
        Keys.onUpPressed: f.up()
        Keys.onDownPressed: f.down()
        Txt {
            anchors.fill: parent
            text: f.placeholder
            color: Theme.textMuted
            font.pointSize: Theme.fontLarge
            visible: input.text === "" && !input.preeditText
        }
    }
}
