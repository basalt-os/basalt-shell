import QtQuick
import QtQuick.Layouts
import QtQuick.Effects
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
                //
                // It never reaches the clock in the middle: entries shrink
                // down to their icon, and when even icons do not fit the list
                // is clipped before the clock and scrolls (mouse wheel, drag),
                // keeping the focused window in view. Before, entries stopped
                // shrinking at a minimum width and many windows ran under the
                // clock. Every entry stays a keyboard stop (a Row of
                // Pressables, all created, in order, unlike a ListView's
                // delegates): Left and Right reach the ones scrolled out, and
                // Nav.reveal scrolls the list to the entry with the focus.
                Flickable {
                    id: taskbar
                    clip: true
                    flickableDirection: Flickable.HorizontalFlick
                    boundsBehavior: Flickable.StopAtBounds
                    interactive: contentWidth > width
                    // Room around the entries for the focus ring, which the
                    // clip would cut otherwise.
                    readonly property real pad: Theme.focusWidth + Theme.focusOffset
                    readonly property int count0: bar.tasks.length
                    readonly property real spacing: Theme.s1
                    // Room up to the clock (x: where the list starts in the
                    // left group, which starts Theme.s1 into the pill).
                    readonly property real maxWidth: Math.max(0, pill.width / 2 - clockBox.width / 2 - Theme.s3 - Theme.s1 - x - Theme.s1)
                    // An icon-only entry.
                    readonly property real minItem: Theme.panelHeight - Theme.s3
                    readonly property real itemWidth: count0 > 0
                        ? Math.max(minItem, Math.min(Theme.fontSize * 15, (maxWidth - 2 * pad - spacing * (count0 - 1)) / count0))
                        : 0
                    readonly property real wanted: count0 > 0 ? count0 * itemWidth + spacing * (count0 - 1) + 2 * pad : 0
                    Layout.preferredWidth: Math.min(maxWidth, wanted)
                    Layout.preferredHeight: Theme.panelHeight - Theme.s3 + 2 * pad
                    contentWidth: taskRow.width + 2 * pad
                    contentHeight: height
                    // Scroll so item (an entry) is in view.
                    function ensureVisible(item) {
                        if (!item) return;
                        const max = Math.max(0, contentWidth - width);
                        const left = item.x, right = item.x + item.width + 2 * pad;
                        if (left < contentX) contentX = Math.max(0, left);
                        else if (right > contentX + width) contentX = Math.min(max, right - width);
                    }
                    // The focused window's entry stays in view, after the
                    // entries are laid out (a new window changes the list and
                    // the focus at once).
                    readonly property int focusedIndex: bar.tasks.findIndex(w => w.focused)
                    function showFocused() {
                        if (focusedIndex >= 0) ensureVisible(taskRepeater.itemAt(focusedIndex));
                        else if (contentX > Math.max(0, contentWidth - width)) contentX = Math.max(0, contentWidth - width);
                    }
                    onFocusedIndexChanged: Qt.callLater(showFocused)
                    onContentWidthChanged: Qt.callLater(showFocused)
                    onWidthChanged: Qt.callLater(showFocused)
                    WheelHandler {
                        acceptedDevices: PointerDevice.Mouse | PointerDevice.TouchPad
                        onWheel: event => {
                            const d = event.angleDelta.y !== 0 ? event.angleDelta.y : event.angleDelta.x;
                            const max = Math.max(0, taskbar.contentWidth - taskbar.width);
                            taskbar.contentX = Math.max(0, Math.min(max, taskbar.contentX - d));
                        }
                    }
                    Row {
                        id: taskRow
                        x: taskbar.pad
                        y: taskbar.pad
                        spacing: taskbar.spacing
                        Repeater {
                            id: taskRepeater
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
                                // Too narrow for a title: the icon alone,
                                // centered, no larger than the entry.
                                readonly property real iconSize: Math.min(Theme.fontSize * 1.5, height - Theme.s1)
                                readonly property bool iconOnly: width < Theme.s2 * 3 + iconSize + Theme.fontSize * 2
                                Row {
                                    anchors.fill: parent
                                    anchors.leftMargin: task.iconOnly ? Math.max(0, (task.width - task.iconSize) / 2) : Theme.s2
                                    anchors.rightMargin: task.showClose ? closeBtn.width + Theme.s1 : (task.iconOnly ? 0 : Theme.s2)
                                    spacing: Theme.s2
                                    IconImage {
                                        anchors.verticalCenter: parent.verticalCenter
                                        implicitSize: task.iconSize
                                        source: Quickshell.iconPath(task.entry ? task.entry.icon : "", "application-x-executable")
                                        opacity: task.min ? 0.55 : 1
                                    }
                                    Txt {
                                        anchors.verticalCenter: parent.verticalCenter
                                        width: parent.width - task.iconSize - parent.spacing
                                        visible: !task.iconOnly
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
                    // Reports with nothing to apply are read, not decided: not counted.
                    readonly property int waiting: Bus.pending.length + Bus.assistantPending.filter(p => !p.report_only).length
                    accessibleName: waiting > 0 ? Tr.n("Ask the system, %1 request waiting", "Ask the system, %1 requests waiting", waiting) : Tr.t("Ask the system")
                    implicitHeight: Theme.panelHeight - Theme.s2
                    active: Ui.commandBar
                    onClicked: bar.openSurface("commandbar", e2e)
                    Rectangle {
                        readonly property int n: askBtn.waiting
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
                            // The theme icon the item names, if any. Apps in a
                            // Flatpak name icons their sandbox has but the
                            // host theme lacks (VLC names "vlc"; Flatpak
                            // exports only "org.videolan.VLC"), which drew
                            // the missing-icon checkerboard: then the icon of
                            // the app's launcher is used. Symbolic icons
                            // (Telegram) are drawn in the panel's text color,
                            // not their dark default.
                            readonly property string themeName: {
                                const u = String(modelData.icon || "");
                                return u.startsWith("image://icon/") ? decodeURIComponent(u.substring(13).split("?")[0]) : "";
                            }
                            // Items that ship their own icon directory
                            // (IconThemePath, "?path=": JetBrains Toolbox) are
                            // found there.
                            readonly property bool missing: themeName !== "" && !themeName.startsWith("/")
                                && String(modelData.icon).indexOf("path=") < 0 && Quickshell.iconPath(themeName, true) === ""
                            readonly property var entry: missing ? DesktopEntries.heuristicLookup(modelData.id || themeName) : null
                            readonly property bool symbolic: !missing && themeName.endsWith("-symbolic")
                            IconImage {
                                anchors.centerIn: parent
                                implicitSize: parent.width * 0.75
                                source: trayItem.missing
                                    ? Quickshell.iconPath(trayItem.entry ? trayItem.entry.icon : "", "application-x-executable")
                                    : trayItem.modelData.icon
                                layer.enabled: trayItem.symbolic
                                layer.effect: MultiEffect {
                                    // White first, then the text color
                                    // (colorization keeps the dark luminance).
                                    brightness: 1.0
                                    colorization: 1.0
                                    colorizationColor: Theme.text
                                }
                            }
                        }
                    }
                }

                // The keyboard layout in use, when there is more than one:
                // click (or Return) switches to the next, as Super+Shift+Space.
                Pressable {
                    id: kbdInd
                    readonly property var kb: Bus.keyboard || ({ labels: [], current: 0 })
                    readonly property var labels: kb.labels || []
                    readonly property string label: labels.length > 0 ? labels[Math.max(0, Math.min(kb.current || 0, labels.length - 1))] : ""
                    visible: labels.length > 1
                    e2e: "panel-keyboard"
                    accessibleName: Tr.t("Keyboard layout: %1").arg(label)
                    accessibleDescription: Tr.t("Press to switch to the next layout")
                    implicitHeight: Theme.panelHeight - Theme.s2
                    implicitWidth: kbdLabel.implicitWidth + Theme.s3
                    radius: Theme.radiusSm
                    color: hovered ? Theme.hover : "transparent"
                    onClicked: Bus.switchLayout()
                    Txt { id: kbdLabel; anchors.centerIn: parent; text: kbdInd.label; role: "small"; font.weight: Font.DemiBold }
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
