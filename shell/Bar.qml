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
            // clock.
            ListView {
                id: taskbar
                orientation: ListView.Horizontal
                spacing: Theme.s1
                clip: true
                boundsBehavior: Flickable.StopAtBounds
                interactive: contentWidth > width
                readonly property int count0: bar.tasks.length
                // Room up to the clock (x: where the list starts in the pill).
                readonly property real maxWidth: Math.max(0, pill.width / 2 - clockBox.width / 2 - Theme.s3 - Theme.s1 - x - Theme.s1)
                // An icon-only entry.
                readonly property real minItem: Theme.panelHeight - Theme.s3
                readonly property real itemWidth: count0 > 0
                    ? Math.max(minItem, Math.min(Theme.fontSize * 15, (maxWidth - spacing * (count0 - 1)) / count0))
                    : 0
                readonly property real wanted: count0 > 0 ? count0 * itemWidth + spacing * (count0 - 1) : 0
                Layout.preferredWidth: Math.min(maxWidth, wanted)
                Layout.preferredHeight: Theme.panelHeight - Theme.s3
                // The focused window's entry stays in view, after the
                // delegates are laid out (a new window changes the list and
                // the focus at once). Not ListView's currentIndex: the view
                // resets it when the model changes.
                readonly property int focusedIndex: bar.tasks.findIndex(w => w.focused)
                function showFocused() {
                    if (focusedIndex >= 0) positionViewAtIndex(focusedIndex, ListView.Contain);
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
                model: bar.tasks
                delegate: Rectangle {
                    id: task
                    required property var modelData
                    property string e2e: "task"
                    readonly property bool min: modelData.state === "minimized"
                    readonly property var entry: DesktopEntries.heuristicLookup(modelData.app_id || "")
                    height: Theme.panelHeight - Theme.s3
                    width: taskbar.itemWidth
                    radius: Theme.radiusSm
                    color: modelData.focused ? Theme.accentSoft : (task.hot ? Theme.hover : "transparent")
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
                    readonly property bool hot: taskMa.containsMouse || closeMa.containsMouse
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

        // Center: clock.
        Item {
            id: clockBox
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

            // Push to talk: hold the button (or Super+V) and speak; release
            // to send. The microphone is open only while it is held.
            Rectangle {
                id: ptt
                visible: Bus.voice.enabled
                readonly property string st: Bus.voice.state || "idle"
                implicitHeight: Theme.panelHeight - Theme.s2
                implicitWidth: implicitHeight
                radius: Theme.radiusSm
                color: st === "listening" ? Theme.danger : (st === "transcribing" || st === "thinking" || st === "speaking" ? Theme.accentSoft
                       : (pttMa.containsMouse ? Theme.hover : "transparent"))
                Behavior on color { ColorAnimation { duration: Theme.fast } }
                Icon {
                    anchors.centerIn: parent
                    name: "mic"
                    size: Theme.panelHeight * 0.5
                    color: ptt.st === "listening" ? "#ffffff" : (ptt.st === "idle" || ptt.st === "error" ? Theme.text : Theme.accent)
                }
                MouseArea {
                    id: pttMa
                    anchors.fill: parent
                    hoverEnabled: true
                    cursorShape: Qt.PointingHandCursor
                    property bool held: false
                    // Down and up, like the key: in "press to start and
                    // stop" the daemon ignores the up and the next click stops.
                    onPressed: { held = true; Bus.voicePress(false); }
                    onReleased: if (held) { held = false; Bus.voiceRelease(); }
                    onCanceled: if (held) { held = false; Bus.voiceRelease(); }
                }
            }

            // Scopes the person gave the read-only skills (folders, mail,
            // sites), while they last. Click: the list, with End buttons.
            Btn {
                visible: Bus.grants.length > 0
                icon: "shield"
                text: Bus.grants.length === 1 ? Bus.grants[0].label : qsTr("%1 permissions").arg(Bus.grants.length)
                implicitHeight: Theme.panelHeight - Theme.s2
                onClicked: Ui.toggle("commandbar")
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
                        id: trayItem
                        required property var modelData
                        width: Theme.panelHeight - Theme.s3
                        height: width
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
            // Lock, log out, suspend, restart, power off (Super+Shift+E).
            Btn {
                icon: "power"
                e2e: "panel-power"
                implicitHeight: Theme.panelHeight - Theme.s2
                implicitWidth: implicitHeight
                active: Ui.powerMenu
                onClicked: Ui.toggle("power")
            }
        }
    }
}
