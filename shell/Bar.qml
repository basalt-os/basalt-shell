import QtQuick
import QtQuick.Layouts
import Quickshell
import Quickshell.Wayland
import Quickshell.Widgets
import Quickshell.Services.SystemTray
import Quickshell.Services.Pipewire
import Quickshell.Services.UPower

// The panel: one per screen, top or bottom (token panel.position).
PanelWindow {
    id: bar
    required property var modelData
    screen: modelData

    readonly property bool top: Theme.panelPosition !== "bottom"
    anchors.top: top
    anchors.bottom: !top
    anchors.left: true
    anchors.right: true
    implicitHeight: Theme.panelHeight + Theme.s2
    exclusiveZone: Theme.panelHeight + Theme.s2
    color: "transparent"
    WlrLayershell.namespace: "basalt-panel"
    WlrLayershell.layer: WlrLayer.Top

    readonly property var spaces: (Bus.desktop.workspaces || []).filter(w => !w.output || w.output === bar.screen.name)
    readonly property var focusedWindow: (Bus.desktop.windows || []).find(w => w.focused)
    // The window list: windows of this screen's visible workspace, then
    // the minimized ones (any screen), in a stable order.
    readonly property var tasks: {
        const shown = (Bus.desktop.workspaces || []).filter(w => w.visible && (!w.output || w.output === bar.screen.name)).map(w => w.id);
        const all = Bus.desktop.windows || [];
        const byId = (a, b) => Number(a.id) - Number(b.id);
        const here = all.filter(w => w.state !== "minimized" && shown.indexOf(w.workspace) >= 0).sort(byId);
        const hidden = all.filter(w => w.state === "minimized").sort(byId);
        return here.concat(hidden);
    }

    Rectangle {
        id: pill
        anchors.fill: parent
        anchors.leftMargin: Theme.s2
        anchors.rightMargin: Theme.s2
        anchors.topMargin: bar.top ? Theme.s2 : 0
        anchors.bottomMargin: bar.top ? 0 : Theme.s2
        radius: Theme.radiusLg
        color: Theme.alpha(Theme.surface, Theme.panelOpacity)
        border.width: 1
        border.color: Theme.border
        Behavior on color { ColorAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        Behavior on radius { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }

        // Left: launcher, workspaces, focused window.
        RowLayout {
            anchors.left: parent.left
            anchors.leftMargin: Theme.s1
            anchors.verticalCenter: parent.verticalCenter
            spacing: Theme.s2

            Btn {
                icon: "logo"
                iconSize: Theme.panelHeight * 0.58
                implicitHeight: Theme.panelHeight - Theme.s2
                implicitWidth: implicitHeight
                active: Ui.launcher
                onClicked: Ui.toggle("launcher")
            }

            Row {
                spacing: Theme.s1
                Repeater {
                    model: bar.spaces
                    delegate: Rectangle {
                        required property var modelData
                        readonly property bool on: modelData.focused || (modelData.visible && bar.spaces.length > 1 && false)
                        height: Theme.panelHeight - Theme.s3
                        width: on ? height * 1.9 : height
                        radius: Math.min(Theme.radiusSm, height / 2)
                        color: on ? Theme.accent : (wsMa.containsMouse ? Theme.hover : "transparent")
                        border.width: modelData.windows > 0 && !on ? 1 : 0
                        border.color: Theme.border
                        Behavior on width { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }
                        Behavior on color { ColorAnimation { duration: Theme.fast } }
                        Txt {
                            anchors.centerIn: parent
                            text: modelData.index > 0 ? modelData.index : modelData.name
                            role: "small"
                            font.weight: Font.DemiBold
                            color: parent.on ? Theme.accentText : (modelData.windows > 0 ? Theme.text : Theme.textMuted)
                        }
                        MouseArea {
                            id: wsMa
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: Bus.act("workspace.switch", { workspace: modelData.id })
                        }
                    }
                }
            }

            // Window list (taskbar): the windows of the workspace shown on
            // this screen, then the minimized ones. Click: focus, or
            // minimize the focused one, or restore a minimized one; middle
            // click closes; right click opens the window menu.
            Row {
                id: taskbar
                spacing: Theme.s1
                readonly property real maxWidth: bar.width * 0.42
                readonly property real itemWidth: bar.tasks.length > 0
                    ? Math.max(Theme.panelHeight * 1.2, Math.min(Theme.fontSize * 15, (maxWidth - spacing * (bar.tasks.length - 1)) / bar.tasks.length))
                    : 0
                Repeater {
                    model: bar.tasks
                    delegate: Rectangle {
                        id: task
                        required property var modelData
                        readonly property bool min: modelData.state === "minimized"
                        readonly property var entry: DesktopEntries.heuristicLookup(modelData.app_id || "")
                        height: Theme.panelHeight - Theme.s3
                        width: taskbar.itemWidth
                        radius: Theme.radiusSm
                        color: modelData.focused ? Theme.accentSoft : (taskMa.containsMouse ? Theme.hover : "transparent")
                        Behavior on color { ColorAnimation { duration: Theme.fast } }
                        // Focus underline.
                        Rectangle {
                            visible: task.modelData.focused
                            anchors.bottom: parent.bottom
                            anchors.horizontalCenter: parent.horizontalCenter
                            width: parent.width * 0.4
                            height: 2
                            radius: 1
                            color: Theme.accent
                        }
                        Row {
                            anchors.fill: parent
                            anchors.leftMargin: Theme.s2
                            anchors.rightMargin: Theme.s2
                            spacing: Theme.s2
                            IconImage {
                                anchors.verticalCenter: parent.verticalCenter
                                implicitSize: Theme.fontSize * 1.5
                                source: Quickshell.iconPath(task.entry ? task.entry.icon : "", "application-x-executable")
                                opacity: task.min ? 0.55 : 1
                            }
                            Txt {
                                anchors.verticalCenter: parent.verticalCenter
                                width: parent.width - Theme.fontSize * 1.5 - parent.spacing
                                visible: width > Theme.fontSize * 2
                                text: task.modelData.title || (task.entry ? task.entry.name : task.modelData.app_id)
                                role: "small"
                                color: task.min ? Theme.textMuted : Theme.text
                                font.italic: task.min
                            }
                        }
                        MouseArea {
                            id: taskMa
                            anchors.fill: parent
                            hoverEnabled: true
                            acceptedButtons: Qt.LeftButton | Qt.MiddleButton | Qt.RightButton
                            cursorShape: Qt.PointingHandCursor
                            onClicked: mouse => {
                                const w = task.modelData;
                                if (mouse.button === Qt.MiddleButton) {
                                    Bus.act("window.close", { window: w.id });
                                } else if (mouse.button === Qt.RightButton) {
                                    const p = task.mapToItem(bar.contentItem, 0, bar.top ? task.height + Theme.s2 : -Theme.s2);
                                    const o = (Bus.desktop.outputs || []).find(o => o.name === bar.screen.name);
                                    Ui.openWindowMenu(w.id, (o ? o.rect.x : 0) + p.x, (o ? o.rect.y : 0) + (bar.top ? p.y : bar.screen.height - 320));
                                } else if (task.min) {
                                    Bus.act("window.set_state", { window: w.id, state: "normal" });
                                } else if (w.focused) {
                                    Bus.act("window.set_state", { window: w.id, state: "minimized" });
                                } else {
                                    Bus.act("window.focus", { window: w.id });
                                }
                            }
                        }
                    }
                }
            }
        }

        // Center: clock.
        Item {
            anchors.centerIn: parent
            width: clockText.implicitWidth + Theme.s4
            height: parent.height
            SystemClock { id: clock; precision: SystemClock.Minutes }
            Txt {
                id: clockText
                anchors.centerIn: parent
                text: Qt.formatDateTime(clock.date, "ddd d MMM  HH:mm")
                font.weight: Font.Medium
            }
            MouseArea {
                anchors.fill: parent
                cursorShape: Qt.PointingHandCursor
                onClicked: Ui.toggle("notifications")
            }
        }

        // Right: assistant, tray, status, notifications, quick settings.
        RowLayout {
            anchors.right: parent.right
            anchors.rightMargin: Theme.s1
            anchors.verticalCenter: parent.verticalCenter
            spacing: Theme.s1

            Btn {
                id: askBtn
                icon: "spark"
                text: "Ask"
                implicitHeight: Theme.panelHeight - Theme.s2
                active: Ui.commandBar
                onClicked: Ui.toggle("commandbar")
                Rectangle {
                    readonly property int n: Bus.pending.length + Bus.assistantPending.length
                    visible: n > 0
                    anchors.right: parent.right
                    anchors.top: parent.top
                    anchors.rightMargin: -2
                    anchors.topMargin: -2
                    width: Math.max(height, badge.implicitWidth + 6)
                    height: Theme.fontSmall * 1.6
                    radius: height / 2
                    color: Theme.warning
                    Txt { id: badge; anchors.centerIn: parent; text: parent.n; role: "small"; color: "#1b1b1b"; font.weight: Font.Bold }
                }
            }

            // An agent in control: who, and Stop.
            Btn {
                visible: Bus.control !== null
                icon: "spark"
                text: "Agent in control"
                variant: "danger"
                implicitHeight: Theme.panelHeight - Theme.s2
                onClicked: Bus.stopControl()
            }

            // StatusNotifierItem tray.
            Row {
                spacing: Theme.s1
                Repeater {
                    model: SystemTray.items
                    delegate: Item {
                        required property var modelData
                        width: Theme.panelHeight - Theme.s3
                        height: width
                        IconImage {
                            anchors.centerIn: parent
                            implicitSize: parent.width * 0.75
                            source: modelData.icon
                        }
                        MouseArea {
                            anchors.fill: parent
                            acceptedButtons: Qt.LeftButton | Qt.RightButton
                            cursorShape: Qt.PointingHandCursor
                            onClicked: mouse => {
                                if (mouse.button === Qt.RightButton || modelData.onlyMenu) {
                                    const p = mapToItem(bar.contentItem, 0, height);
                                    modelData.display(bar, p.x, p.y);
                                } else {
                                    modelData.activate();
                                }
                            }
                        }
                    }
                }
            }

            StatusIcons { height: Theme.panelHeight - Theme.s2 }

            Btn {
                icon: "bell"
                implicitHeight: Theme.panelHeight - Theme.s2
                implicitWidth: implicitHeight
                active: Ui.drawer
                onClicked: Ui.toggle("notifications")
                Rectangle {
                    visible: Notifs.unread > 0
                    width: 8; height: 8; radius: 4
                    color: Theme.accent
                    anchors.right: parent.right; anchors.top: parent.top; anchors.margins: 5
                }
            }
            Btn {
                icon: "sliders"
                implicitHeight: Theme.panelHeight - Theme.s2
                implicitWidth: implicitHeight
                active: Ui.quickSettings
                onClicked: Ui.toggle("quicksettings")
            }
        }
    }
}
