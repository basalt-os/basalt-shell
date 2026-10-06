import QtQuick

// A menu or a list of entries with one Tab stop: Up and Down move (and
// wrap, as menus do), Home and End go to the ends, and with typeAhead a
// letter goes to the next entry that starts with it.
Column {
    id: g
    property bool navRoving: true
    property Item tabStop: null
    property bool wrap: true
    property bool typeAhead: false
    property int accessibleRole: Accessible.List
    Accessible.role: accessibleRole
    Keys.onPressed: e => Nav.groupKey(g, e, "vertical", 1, g.wrap, g.typeAhead)
}
