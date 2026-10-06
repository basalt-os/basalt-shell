import QtQuick
import "logic.js" as L

// The people who can log in, and "Other user". large: the first screen
// when several people use the computer and nobody is remembered.
Column {
    id: picker
    property bool large: false
    spacing: G.s4

    GTxt {
        visible: picker.large
        anchors.horizontalCenter: parent.horizontalCenter
        role: "title"
        text: I18n.t("Who is logging in?")
        Accessible.role: Accessible.Heading
        Accessible.name: text
    }
    Row {
        id: row
        anchors.horizontalCenter: parent.horizontalCenter
        spacing: picker.large ? G.s4 : G.s2
        Repeater {
            model: Sys.users
            delegate: Tile {
                required property var modelData
                user: modelData
                name: L.displayName(modelData)
                selected: !Login.other && Login.user !== null && Login.user.name === modelData.name
                e2e: "user:" + modelData.name
                onClicked: Login.choose(modelData)
            }
        }
        Tile {
            other: true
            name: I18n.t("Other user")
            selected: Login.other
            e2e: "other-user"
            onClicked: Login.chooseOther()
        }
    }

    component Tile: Rectangle {
        id: tile
        property var user: null
        property bool other: false
        property string name: ""
        property bool selected: false
        property string e2e: ""
        signal clicked()
        objectName: "e2e:" + e2e
        readonly property real avatarSize: (picker.large ? 88 : 44) * G.textScale
        width: Math.max(avatarSize + G.s6, Math.min(nameText.implicitWidth + G.s4, 160 * G.textScale))
        height: col.implicitHeight + G.s3 * 2
        radius: G.radiusLg
        color: ma.pressed ? G.pressed : (ma.containsMouse ? G.hover : (picker.large ? G.alpha(G.surface, G.highContrast ? 1 : 0.6) : "transparent"))
        border.width: activeFocus ? 2 : (picker.large && G.highContrast ? 1 : 0)
        border.color: activeFocus ? G.accent : G.border
        activeFocusOnTab: true
        Accessible.role: Accessible.Button
        Accessible.name: tile.name
        Accessible.onPressAction: tile.clicked()
        Keys.onReturnPressed: tile.clicked()
        Keys.onEnterPressed: tile.clicked()
        Keys.onSpacePressed: tile.clicked()
        Column {
            id: col
            anchors.centerIn: parent
            spacing: G.s2
            Avatar {
                anchors.horizontalCenter: parent.horizontalCenter
                size: tile.avatarSize
                user: tile.user
                other: tile.other
                selected: tile.selected
            }
            GTxt {
                id: nameText
                anchors.horizontalCenter: parent.horizontalCenter
                width: Math.min(implicitWidth, tile.width - G.s2)
                horizontalAlignment: Text.AlignHCenter
                text: tile.name
                role: picker.large ? "body" : "small"
                color: tile.selected ? G.text : G.alpha(G.text, 0.82)
                font.weight: tile.selected ? Font.DemiBold : Font.Normal
            }
        }
        MouseArea {
            id: ma
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: tile.clicked()
        }
    }
}
