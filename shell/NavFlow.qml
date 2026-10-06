import QtQuick

// A wrapping set of choices with one Tab stop (NavRow that wraps lines):
// Left and Right move between its controls, Home and End go to the ends.
Flow {
    id: g
    property bool navRoving: true
    property Item tabStop: null
    property bool wrap: false
    Accessible.role: Accessible.Grouping
    Keys.onPressed: e => Nav.groupKey(g, e, "horizontal", 1, g.wrap, false)
}
