import QtQuick
import Quickshell
import Quickshell.Wayland
import Quickshell.Widgets

// Application launcher: desktop entries from the daemon (same list the
// app.launch action and the MCP apps_list tool use), fuzzy search,
// keyboard first.
PanelWindow {
    id: win
    visible: Ui.launcher || card.opacity > 0
    anchors { top: true; bottom: true; left: true; right: true }
    color: "transparent"
    exclusionMode: ExclusionMode.Ignore
    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.namespace: "basalt-launcher"
    WlrLayershell.keyboardFocus: Ui.launcher ? WlrKeyboardFocus.Exclusive : WlrKeyboardFocus.None

    property var apps: []
    property var results: []
    property int sel: 0

    // Fuzzy score: consecutive and word-start matches score higher; null
    // when not every character of the query appears in order.
    function score(q, s) {
        if (!q) return 1;
        s = s.toLowerCase();
        let qi = 0, sc = 0, prev = -2;
        for (let i = 0; i < s.length && qi < q.length; i++) {
            if (s[i] === q[qi]) {
                sc += 1;
                if (i === prev + 1) sc += 3;
                if (i === 0 || " -._".indexOf(s[i - 1]) >= 0) sc += 5;
                prev = i; qi++;
            }
        }
        return qi === q.length ? sc - s.length * 0.01 : null;
    }
    function update() {
        const q = search.text.trim().toLowerCase();
        let r = [];
        for (const a of win.apps) {
            const fields = [a.name, a.generic || "", (a.keywords || []).join(" "), a.id];
            let best = null;
            for (let i = 0; i < fields.length; i++) {
                const s = score(q, fields[i]);
                if (s !== null) { const w = s * (i === 0 ? 1.5 : 1); if (best === null || w > best) best = w; }
            }
            if (best !== null) r.push({ app: a, s: best });
        }
        r.sort((x, y) => y.s - x.s || x.app.name.localeCompare(y.app.name));
        win.results = r.slice(0, 40).map(x => x.app);
        win.sel = 0;
    }
    function launch(a) {
        if (!a) return;
        Bus.act("app.launch", { app: a.id });
        Ui.launcher = false;
    }

    onVisibleChanged: if (Ui.launcher) {
        search.text = "";
        Bus.call("apps", {}, (ok, list) => { if (ok) { win.apps = list || []; win.update(); } });
        search.focusInput();
    }

    MouseArea { anchors.fill: parent; onClicked: Ui.launcher = false }

    Surface {
        id: card
        width: Math.min(640, parent.width - Theme.s6 * 2)
        height: Math.min(560, parent.height * 0.7, search.height + Theme.s4 * 2 + Theme.s3 + Math.max(1, win.results.length) * (Theme.fontSize * 4.2 + 2))
        anchors.horizontalCenter: parent.horizontalCenter
        y: parent.height * 0.14 + (Ui.launcher ? 0 : Theme.s6)
        opacity: Ui.launcher ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        Behavior on y { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        MouseArea { anchors.fill: parent }

        Column {
            anchors.fill: parent
            anchors.margins: Theme.s4
            spacing: Theme.s3
            Field {
                id: search
                width: parent.width
                icon: "search"
                placeholder: "Search applications"
                onEdited: win.update()
                onEscapePressed: Ui.launcher = false
                onDownPressed: win.sel = Math.min(win.results.length - 1, win.sel + 1)
                onUpPressed: win.sel = Math.max(0, win.sel - 1)
                onAccepted: win.launch(win.results[win.sel])
            }
            ListView {
                id: list
                width: parent.width
                height: parent.height - search.height - Theme.s3
                clip: true
                model: win.results
                currentIndex: win.sel
                spacing: 2
                highlightMoveDuration: Theme.fast
                highlight: Rectangle { radius: Theme.radiusMd; color: Theme.accentSoft }
                delegate: Item {
                    id: row
                    required property var modelData
                    required property int index
                    width: list.width
                    height: Theme.fontSize * 4.2
                    Rectangle { anchors.fill: parent; radius: Theme.radiusMd; color: rowMa.containsMouse && win.sel !== index ? Theme.hover : "transparent" }
                    IconImage {
                        id: appIcon
                        anchors.left: parent.left
                        anchors.leftMargin: Theme.s3
                        anchors.verticalCenter: parent.verticalCenter
                        implicitSize: parent.height * 0.62
                        source: Quickshell.iconPath(modelData.icon || "", "application-x-executable")
                    }
                    Column {
                        anchors.left: appIcon.right
                        anchors.leftMargin: Theme.s3
                        anchors.right: parent.right
                        anchors.rightMargin: Theme.s3
                        anchors.verticalCenter: parent.verticalCenter
                        Txt { width: parent.width; text: modelData.name; font.weight: Font.Medium }
                        Txt { width: parent.width; text: modelData.comment || modelData.generic || modelData.id; role: "small"; color: Theme.textMuted; visible: text !== "" }
                    }
                    MouseArea {
                        id: rowMa
                        anchors.fill: parent
                        hoverEnabled: true
                        onClicked: win.launch(modelData)
                        onEntered: win.sel = index
                    }
                }
            }
        }
    }
}
