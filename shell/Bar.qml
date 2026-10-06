import QtQuick
import QtQuick.Layouts
import Quickshell
import Quickshell.Wayland
import Quickshell.Widgets
import Quickshell.Services.SystemTray
import Quickshell.Services.Pipewire
import Quickshell.Services.UPower

// The panel: one per screen, top or bottom (token panel.position).
// Keyboard: Ctrl+Alt+Tab or Super+B give the keyboard to the panel of the
// screen with the focused workspace; Left and Right (Tab, Shift+Tab, Home,
// End) move along it, Return or Space press, Menu or Shift+F10 open a
// window's menu, Escape gives the keyboard back. A surface opened from
// here returns to its button when it is closed with Escape.
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

    // Is this the panel that takes the keyboard?
    readonly property bool keyboardScreen: {
        const ws = (Bus.desktop.workspaces || []).find(w => w.focused);
        if (ws && ws.output) return ws.output === bar.screen.name;
        const list = Quickshell.screens;
        return list.length > 0 && list[0].name === bar.screen.name;
    }
    readonly property bool kbd: Ui.panelFocus && !Ui.modal && keyboardScreen
    WlrLayershell.keyboardFocus: kbd ? WlrKeyboardFocus.Exclusive : WlrKeyboardFocus.None
    onKbdChanged: if (kbd) Qt.callLater(() => { Nav.initial(pill, Ui.focusKey); Ui.focusKey = ""; })

    // Open a surface from a panel button: Escape comes back to the button
    // when the panel had the keyboard.
    function openSurface(surface, key) {
        if (Ui.panelFocus) Ui.toggleFrom(surface, "panel", key);
        else Ui.toggle(surface);
    }

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

    // Holds the panel's keyboard focus even when the focused entry goes
    // away (a window closed from its menu), so Escape always works.
    FocusScope {
        id: panelScope
        anchors.fill: parent
        Keys.onPressed: e => {
            if (e.key === Qt.Key_Escape) { Ui.panelFocus = false; e.accepted = true; return; }
            Nav.groupKey(pill, e, "horizontal", 1, true, false);
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
            Accessible.role: Accessible.ToolBar
            Accessible.name: Tr.t("Panel")

            // Left: launcher, workspaces, focused window.
            RowLayout {
                anchors.left: parent.left
                anchors.leftMargin: Theme.s1
                anchors.verticalCenter: parent.verticalCenter
                spacing: Theme.s2

                Btn {
                    icon: "logo"
                    e2e: "panel-launcher"
                    accessibleName: Tr.t("Applications")
                    iconSize: Theme.panelHeight * 0.58
                    implicitHeight: Theme.panelHeight - Theme.s2
                    implicitWidth: implicitHeight
                    active: Ui.launcher
                    onClicked: bar.openSurface("launcher", e2e)
                }

                Row {
                    spacing: Theme.s1
                    Repeater {
                        model: bar.spaces
                        delegate: Pressable {
                            required property var modelData
                            readonly property bool on: modelData.focused || (modelData.visible && bar.spaces.length > 1 && false)
                            navKey: "panel-ws-" + modelData.id
                            accessibleName: Tr.t("Workspace %1").arg(modelData.index > 0 ? modelData.index : modelData.name)
                            checkable: true
                            checked: on
                            onClicked: Bus.act("workspace.switch", { workspace: modelData.id })
                            height: Theme.panelHeight - Theme.s3
                            width: on ? height * 1.9 : height
                            radius: Math.min(Theme.radiusSm, height / 2)
                            color: on ? Theme.accent : (hovered ? Theme.hover : "transparent")
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
                        }
                    }
                }

                // Window list (taskbar): the windows of the workspace shown on
                // this screen, then the minimized ones. Click: focus, or
                // minimize the focused one, or restore a minimized one; middle
                // click closes; right click opens the window menu. Hovering an
                // entry shows a close button, so any window can be closed with
                // the mouse, also one without a title bar of its own (X11 apps
                // on niri) or with a compositor title bar (sway cannot draw
                // buttons on it).
                Row {
                    id: taskbar
                    spacing: Theme.s1
                    // Up to the clock in the middle (many windows used to run
                    // under it).
                    readonly property real maxWidth: Math.max(0, pill.width / 2 - clockBox.width / 2 - Theme.s3 - Theme.s1 - x)
                    readonly property real itemWidth: bar.tasks.length > 0
                        ? Math.max(Theme.panelHeight * 1.2, Math.min(Theme.fontSize * 15, (maxWidth - spacing * (bar.tasks.length - 1)) / bar.tasks.length))
                        : 0
                    Repeater {
                        model: bar.tasks
                        delegate: Pressable {
                            id: task
                            required property var modelData
                            e2e: "task"
                            navKey: "task-" + modelData.id
                            readonly property bool min: modelData.state === "minimized"
                            readonly property var entry: DesktopEntries.heuristicLookup(modelData.app_id || "")
                            accessibleName: (modelData.title || (entry ? entry.name : modelData.app_id)) + (min ? ", " + Tr.t("minimized") : "")
                            checkable: true
                            checked: modelData.focused
                            contextMenu: true
                            acceptedButtons: Qt.LeftButton | Qt.MiddleButton | Qt.RightButton
                            height: Theme.panelHeight - Theme.s3
                            width: taskbar.itemWidth
                            radius: Theme.radiusSm
                            color: modelData.focused ? Theme.accentSoft : (task.hot ? Theme.hover : "transparent")
                            onMiddleClicked: Bus.act("window.close", { window: modelData.id })
                            onMenuRequested: {
                                const w = task.modelData;
                                const p = task.mapToItem(bar.contentItem, 0, bar.top ? task.height + Theme.s2 : -Theme.s2);
                                const o = (Bus.desktop.outputs || []).find(o => o.name === bar.screen.name);
                                Ui.openWindowMenu(w.id, (o ? o.rect.x : 0) + p.x, (o ? o.rect.y : 0) + (bar.top ? p.y : bar.screen.height - 320),
                                                  Ui.panelFocus ? "panel" : "", task.navKey);
                            }
                            onClicked: {
                                const w = task.modelData;
                                if (task.min) Bus.act("window.set_state", { window: w.id, state: "normal" });
                                else if (w.focused) Bus.act("window.set_state", { window: w.id, state: "minimized" });
                                else Bus.act("window.focus", { window: w.id });
                            }
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
                            readonly property bool hot: task.hovered || closeMa.containsMouse
                            // Room for the close button, shown on hover when the
                            // entry is wide enough for a title.
                            readonly property bool showClose: hot && width > Theme.fontSize * 6
                            Row {
                                anchors.fill: parent
                                anchors.leftMargin: Theme.s2
                                anchors.rightMargin: task.showClose ? closeBtn.width + Theme.s1 : Theme.s2
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
                            // Close button (hover). Declared after the entry's
                            // MouseArea so it takes the click.
                            Rectangle {
                                id: closeBtn
                                property string e2e: "task-close"
                                visible: task.showClose
                                anchors.right: parent.right
                                anchors.rightMargin: Theme.s1
                                anchors.verticalCenter: parent.verticalCenter
                                width: Theme.fontSize * 1.7
                                height: width
                                radius: width / 2
                                color: closeMa.pressed ? Theme.pressed : (closeMa.containsMouse ? Theme.hover : "transparent")
                                Icon {
                                    anchors.centerIn: parent
                                    name: "close"
                                    size: Theme.fontSize * 1.05
                                    color: closeMa.containsMouse ? Theme.danger : Theme.textMuted
                                }
                                MouseArea {
                                    id: closeMa
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: Bus.act("window.close", { window: task.modelData.id })
                                }
                            }
                        }
                    }
                }
            }

            // Center: clock (opens the notifications).
            Pressable {
                id: clockBox
                anchors.centerIn: parent
                width: clockText.implicitWidth + Theme.s4
                height: Theme.panelHeight - Theme.s2
                radius: Theme.radiusSm
                e2e: "panel-clock"
                accessibleName: Tr.t("%1, notifications").arg(Qt.formatDateTime(clock.date, "dddd d MMMM HH:mm"))
                color: hovered ? Theme.hover : "transparent"
                onClicked: bar.openSurface("notifications", e2e)
                SystemClock { id: clock; precision: SystemClock.Minutes }
                Txt {
                    id: clockText
                    anchors.centerIn: parent
                    text: Qt.formatDateTime(clock.date, "ddd d MMM  HH:mm")
                    font.weight: Font.Medium
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
                    e2e: "panel-ask"
                    readonly property int waiting: Bus.pending.length + Bus.assistantPending.length
                    accessibleName: waiting > 0 ? Tr.n("Ask the system, %1 request waiting", "Ask the system, %1 requests waiting", waiting) : Tr.t("Ask the system")
                    implicitHeight: Theme.panelHeight - Theme.s2
                    active: Ui.commandBar
                    onClicked: bar.openSurface("commandbar", e2e)
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

                // Push to talk: hold the button (or Super+V) and speak; release
                // to send. The microphone is open only while it is held.
                // From the keyboard (Return or Space) it works as press to
                // start and stop: press again to send.
                Pressable {
                    id: ptt
                    visible: Bus.voice.enabled
                    readonly property string st: Bus.voice.state || "idle"
                    e2e: "panel-mic"
                    accessibleName: Tr.t("Push to talk")
                    accessibleDescription: Tr.t("Hold to speak to the assistant, or press Return to start and again to stop")
                    checkable: true
                    checked: st === "listening"
                    implicitHeight: Theme.panelHeight - Theme.s2
                    implicitWidth: implicitHeight
                    radius: Theme.radiusSm
                    color: st === "listening" ? Theme.danger : (st === "transcribing" || st === "thinking" || st === "speaking" ? Theme.accentSoft
                           : (hovered ? Theme.hover : "transparent"))
                    // Down and up, like the key: in "press to start and
                    // stop" the daemon ignores the up and the next click stops.
                    property bool held: false
                    onPressedChanged: {
                        if (pressed) { held = true; Bus.voicePress(false); }
                        else if (held) { held = false; Bus.voiceRelease(); }
                    }
                    onClicked: if (Ui.focusVisible) Bus.voicePress(true)
                    Behavior on color { ColorAnimation { duration: Theme.fast } }
                    Icon {
                        anchors.centerIn: parent
                        name: "mic"
                        size: Theme.panelHeight * 0.5
                        color: ptt.st === "listening" ? "#ffffff" : (ptt.st === "idle" || ptt.st === "error" ? Theme.text : Theme.accent)
                    }
                }

                // Scopes the person gave the read-only skills (folders, mail,
                // sites), while they last. Click: the list, with End buttons.
                Btn {
                    visible: Bus.grants.length > 0
                    icon: "shield"
                    e2e: "panel-grants"
                    text: Bus.grants.length === 1 ? Bus.grants[0].label : qsTr("%1 permissions").arg(Bus.grants.length)
                    implicitHeight: Theme.panelHeight - Theme.s2
                    onClicked: bar.openSurface("commandbar", e2e)
                }

                // An agent in control: who, and Stop.
                Btn {
                    visible: Bus.control !== null
                    icon: "spark"
                    text: "Agent in control"
                    variant: "danger"
                    e2e: "panel-agent-stop"
                    accessibleName: Tr.t("Agent in control: stop it")
                    implicitHeight: Theme.panelHeight - Theme.s2
                    onClicked: Bus.stopControl()
                }

                // StatusNotifierItem tray.
                Row {
                    spacing: Theme.s1
                    Repeater {
                        model: SystemTray.items
                        delegate: Pressable {
                            id: trayItem
                            required property var modelData
                            width: Theme.panelHeight - Theme.s3
                            height: width
                            radius: Theme.radiusSm
                            navKey: "tray-" + modelData.id
                            accessibleName: modelData.tooltipTitle || modelData.title || modelData.id
                            contextMenu: modelData.hasMenu
                            color: hovered ? Theme.hover : "transparent"
                            function showMenu() {
                                const p = trayItem.mapToItem(bar.contentItem, 0, trayItem.height);
                                modelData.display(bar, p.x, p.y);
                            }
                            onClicked: { if (modelData.onlyMenu) trayItem.showMenu(); else modelData.activate(); }
                            onMenuRequested: trayItem.showMenu()
                            IconImage {
                                anchors.centerIn: parent
                                implicitSize: parent.width * 0.75
                                source: modelData.icon
                            }
                        }
                    }
                }

                StatusIcons { height: Theme.panelHeight - Theme.s2; onOpen: bar.openSurface("quicksettings", "panel-status") }

                Btn {
                    icon: "bell"
                    e2e: "panel-notifications"
                    accessibleName: Notifs.unread > 0 ? Tr.n("Notifications, %1 new", "Notifications, %1 new", Notifs.unread) : Tr.t("Notifications")
                    implicitHeight: Theme.panelHeight - Theme.s2
                    implicitWidth: implicitHeight
                    active: Ui.drawer
                    onClicked: bar.openSurface("notifications", e2e)
                    Rectangle {
                        visible: Notifs.unread > 0
                        width: 8; height: 8; radius: 4
                        color: Theme.accent
                        anchors.right: parent.right; anchors.top: parent.top; anchors.margins: 5
                    }
                }
                Btn {
                    icon: "sliders"
                    e2e: "panel-quicksettings"
                    accessibleName: Tr.t("Quick settings")
                    implicitHeight: Theme.panelHeight - Theme.s2
                    implicitWidth: implicitHeight
                    active: Ui.quickSettings
                    onClicked: bar.openSurface("quicksettings", e2e)
                }
                // Lock, log out, suspend, restart, power off (Super+Shift+E).
                Btn {
                    icon: "power"
                    e2e: "panel-power"
                    accessibleName: Tr.t("Power")
                    implicitHeight: Theme.panelHeight - Theme.s2
                    implicitWidth: implicitHeight
                    active: Ui.powerMenu
                    onClicked: bar.openSurface("power", e2e)
                }
            }
        }
    }
}
