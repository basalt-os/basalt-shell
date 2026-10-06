import QtQuick

// A row of choices or actions with one Tab stop: Left and Right move
// between its controls (Pressable, Btn), Home and End go to the ends.
// At an edge the key goes on to the enclosing group (Settings: Left
// returns to the sidebar).
Row {
    id: g
    property bool navRoving: true
    property Item tabStop: null
    property bool wrap: false
    Accessible.role: Accessible.Grouping
    Keys.onPressed: e => Nav.groupKey(g, e, "horizontal", 1, g.wrap, false)
}
