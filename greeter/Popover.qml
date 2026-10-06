import QtQuick

// A small menu that opens next to the button that asked for it: a list
// of items { id, icon, text, checked, e2e }, keyboard friendly (Up, Down,
// Home, End, Enter, Space, Escape; Tab closes it) and announced as a menu
// to screen readers. Closing it puts the focus back on its button. A click
// anywhere else closes it.
Item {
    id: pop
    property var items: []
    property Item anchorItem: null
    property string title: ""
    property string e2e: ""
    signal picked(string id)
    signal closed()

    objectName: e2e !== "" ? "e2e:" + e2e : ""
    visible: false
    z: 100
    anchors.fill: parent

    MouseArea {
        anchors.fill: parent
        onClicked: pop.close()
    }

    function openAt(item) {
        anchorItem = item;
        const p = item.mapToItem(pop, 0, 0);
        // Below the button, kept inside the screen; above it when there is
        // no room below.
        let x = p.x + item.width - menu.width;
        if (x < G.s4) x = Math.min(p.x, pop.width - menu.width - G.s4);
        let y = p.y + item.height + G.s2;
        if (y + menu.height > pop.height - G.s4) y = p.y - menu.height - G.s2;
        menu.x = Math.max(G.s4, x);
        menu.y = Math.max(G.s4, y);
        visible = true;
        list.currentIndex = Math.max(0, items.findIndex(i => i.checked));
        list.forceActiveFocus();
    }
    function close() {
        if (!visible) return;
        visible = false;
        closed();
        if (anchorItem) anchorItem.forceActiveFocus();
    }

    Rectangle {
    id: menu
    width: Math.max(220 * G.textScale, col.implicitWidth + G.s4)
    height: col.implicitHeight + G.s2 * 2
    radius: G.radiusMd
    color: G.highContrast ? "#000000" : G.surface
    border.width: G.borderWidth
    border.color: G.border
    Accessible.role: Accessible.PopupMenu
    Accessible.name: pop.title
    MouseArea { anchors.fill: parent }
    Column {
        id: col
        anchors.fill: parent
        anchors.margins: G.s2
        spacing: G.s1
        GTxt {
            visible: pop.title !== ""
            text: pop.title
            role: "small"
            color: G.textMuted
            leftPadding: G.s2
            height: implicitHeight + G.s1
        }
        ListView {
            id: list
            width: parent.width
            height: contentHeight
            interactive: false
            // Not bound to interactive (its default): a list that does not
            // flick with the mouse still moves with the arrows.
            keyNavigationEnabled: true
            model: pop.items
            keyNavigationWraps: true
            Keys.onEscapePressed: pop.close()
            Keys.onReturnPressed: pop.picked(pop.items[currentIndex].id)
            Keys.onEnterPressed: pop.picked(pop.items[currentIndex].id)
            Keys.onSpacePressed: pop.picked(pop.items[currentIndex].id)
            Keys.onTabPressed: pop.close()
            Keys.onBacktabPressed: pop.close()
            Keys.onPressed: e => {
                if (e.key === Qt.Key_Home) { currentIndex = 0; e.accepted = true; }
                else if (e.key === Qt.Key_End) { currentIndex = count - 1; e.accepted = true; }
                else e.accepted = false;
            }
            delegate: Rectangle {
                id: row
                required property var modelData
                required property int index
                width: ListView.view.width
                height: rowText.implicitHeight + G.s3 * 1.6
                radius: G.radiusSm
                objectName: modelData.e2e ? "e2e:" + modelData.e2e : ""
                color: ma.pressed ? G.pressed : ((ma.containsMouse || ListView.isCurrentItem) ? G.hover : "transparent")
                border.width: ListView.isCurrentItem && list.activeFocus ? 2 : 0
                border.color: G.focusRing
                Accessible.role: Accessible.MenuItem
                Accessible.name: modelData.text
                Accessible.checkable: modelData.checked !== undefined
                Accessible.checked: modelData.checked === true
                Accessible.onPressAction: pop.picked(modelData.id)
                Row {
                    anchors.left: parent.left
                    anchors.leftMargin: G.s3
                    anchors.verticalCenter: parent.verticalCenter
                    spacing: G.s3
                    GIcon {
                        name: row.modelData.icon || "info"
                        visible: !!row.modelData.icon
                        size: G.fontSize * 1.5
                        anchors.verticalCenter: parent.verticalCenter
                    }
                    GTxt {
                        id: rowText
                        text: row.modelData.text
                        anchors.verticalCenter: parent.verticalCenter
                    }
                }
                GIcon {
                    visible: row.modelData.checked === true
                    name: "check"
                    size: G.fontSize * 1.4
                    color: G.accent
                    anchors.right: parent.right
                    anchors.rightMargin: G.s3
                    anchors.verticalCenter: parent.verticalCenter
                }
                MouseArea {
                    id: ma
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    onClicked: pop.picked(row.modelData.id)
                }
            }
        }
    }
    }
}
