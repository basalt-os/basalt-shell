import QtQuick
import QtQuick.Layouts
import Quickshell
import Quickshell.Wayland
import Quickshell.Services.Pipewire
import Quickshell.Services.UPower

// Quick settings: light/dark, theme, motion, spoken answers, volume,
// network, battery and a door to the full settings page. Changes made
// here are the person's own, so they apply at once (audited as "ui").
// Keyboard: it takes the keyboard while open; Tab moves between the
// tiles, the themes, the volume and the buttons at the bottom; arrows
// move inside each; Escape closes it and returns to where it was opened.
PanelWindow {
    id: win
    visible: Ui.quickSettings || card.opacity > 0
    anchors { top: true; bottom: true; left: true; right: true }
    color: "transparent"
    exclusionMode: ExclusionMode.Normal
    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.namespace: "basalt-quicksettings"
    WlrLayershell.keyboardFocus: Ui.quickSettings && !Ui.modal ? WlrKeyboardFocus.Exclusive : WlrKeyboardFocus.None

    readonly property var st: Bus.themeState
    readonly property var sink: Pipewire.defaultAudioSink
    PwObjectTracker { objects: win.sink ? [win.sink] : [] }

    // The person's voice and assistant settings, read when the panel
    // opens and kept current by the daemon's "voice-settings" event; the
    // spoken answers tile saves through the same path as the Settings
    // page (Bus.saveVoiceSettings, voice.settings.set).
    property var voiceInfo: null
    property string voiceError: ""
    readonly property bool spoken: voiceInfo !== null && voiceInfo.prefs.spoken !== "no"
    function loadVoice() {
        Bus.call("voice.settings", {}, (ok, res) => { if (ok) { win.voiceInfo = res; win.voiceError = ""; } });
    }
    function setSpoken(on) {
        if (!win.voiceInfo) return;
        Bus.saveVoiceSettings(win.voiceInfo.prefs, { spoken: on ? "yes" : "no" }, (ok, res) => {
            if (ok) { win.voiceInfo = res; win.voiceError = ""; }
            else win.voiceError = res;
        });
    }
    Connections {
        target: Ui
        function onQuickSettingsChanged() {
            if (!Ui.quickSettings) return;
            win.loadVoice();
            Qt.callLater(win.focusFirst);
        }
    }
    function focusFirst() {
        Nav.initial(col, Ui.focusKey);
        Ui.focusKey = "";
    }
    Connections {
        target: Bus
        function onVoiceSettings(data) { win.voiceInfo = data; }
    }

    MouseArea { anchors.fill: parent; onClicked: Ui.quickSettings = false }

    // 8 px from the panel's edge and the side (the layer starts after the
    // panel's exclusive zone).
    Surface {
        id: card
        width: Math.min(360, parent.width - Theme.popoverGap * 2)
        height: col.implicitHeight + Theme.s4 * 2
        anchors.right: parent.right
        anchors.rightMargin: Theme.popoverGap
        y: (Theme.panelPosition === "bottom" ? parent.height - height - Theme.popoverGap : Theme.popoverGap) + (Ui.quickSettings ? 0 : (Theme.panelPosition === "bottom" ? Theme.s4 : -Theme.s4))
        opacity: Ui.quickSettings ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        Behavior on y { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        MouseArea { anchors.fill: parent }
        Keys.onEscapePressed: Ui.dismiss()

        ColumnLayout {
            id: col
            anchors.fill: parent
            anchors.margins: Theme.s4
            spacing: Theme.s3
            Accessible.role: Accessible.Pane
            Accessible.name: Tr.t("Quick settings")

            // Tiles: one Tab stop, arrows move across the grid.
            GridLayout {
                id: tiles
                columns: 2
                Layout.fillWidth: true
                rowSpacing: Theme.s2
                columnSpacing: Theme.s2
                property bool navRoving: true
                property Item tabStop: null
                Keys.onPressed: e => Nav.groupKey(tiles, e, "grid", tiles.columns, false, false)
                Tile {
                    e2e: "quick-mode"
                    icon: Theme.dark ? "moon" : "sun"
                    label: Theme.dark ? "Dark" : "Light"
                    sub: "Appearance"
                    on: Theme.dark
                    onClicked: Bus.act("theme.switch", { mode: "toggle" })
                }
                Tile {
                    e2e: "quick-motion"
                    icon: "motion"
                    label: Theme.animate ? "Animations on" : "Reduced motion"
                    sub: win.st && win.st.settings.motion === "auto" ? "Auto" + (win.st.hardware.weak ? " (weak GPU)" : "") : "Manual"
                    on: Theme.animate
                    onClicked: Bus.act("motion.set", { motion: Theme.animate ? "reduced" : "full" })
                }
                Tile {
                    e2e: "quick-network"
                    toggle: false
                    icon: "network"
                    label: Net.online ? (Net.name || Net.kind) : "Offline"
                    sub: Net.online ? (Net.kind === "wifi" ? "Wi-Fi" : "Wired") : "Network"
                    on: Net.online
                    onClicked: Bus.act("app.launch", { app: "nm-connection-editor" })
                }
                Tile {
                    // Spoken answers: off means answers are only shown and
                    // nothing is synthesized; push to talk keeps working.
                    visible: win.voiceInfo !== null
                    icon: win.spoken ? "volume" : "mute"
                    label: Tr.t("Spoken answers")
                    sub: win.spoken ? Tr.t("On") : Tr.t("Off")
                    on: win.spoken
                    e2e: "quick-spoken-answers"
                    onClicked: win.setSpoken(!win.spoken)
                }
                Tile {
                    e2e: "quick-ask"
                    icon: "spark"
                    label: "Ask the system"
                    sub: Bus.translatorAvailable ? "Local model" : "Commands"
                    on: false
                    toggle: false
                    onClicked: Ui.openFrom("commandbar", "", "quicksettings", "quick-ask")
                }
            }

            Txt {
                visible: win.voiceError !== ""
                text: win.voiceError
                color: Theme.danger
                role: "small"
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
            }

            // Theme swatches.
            Txt { text: "Theme"; role: "small"; color: Theme.textMuted }
            NavRow {
                spacing: Theme.s2
                Accessible.name: Tr.t("Theme")
                Repeater {
                    model: win.st ? win.st.themes : []
                    delegate: Pressable {
                        required property var modelData
                        readonly property var sw: Theme.dark ? modelData.dark : modelData.light
                        width: 104; height: 58
                        radius: Theme.radiusMd
                        color: sw[0]
                        border.width: Theme.themeId === modelData.id ? 2 : 1
                        border.color: Theme.themeId === modelData.id ? Theme.accent : Theme.border
                        accessibleName: modelData.name
                        e2e: "quick-theme-" + modelData.id
                        checkable: true
                        active: Theme.themeId === modelData.id
                        checked: active
                        onClicked: Bus.act("theme.switch", { theme: modelData.id })
                        Rectangle { x: 8; y: 8; width: 40; height: 10; radius: 3; color: sw[1] }
                        Rectangle { x: 8; y: 22; width: 24; height: 10; radius: 3; color: sw[2] }
                        Txt { anchors.left: parent.left; anchors.bottom: parent.bottom; anchors.margins: 6; text: modelData.name; role: "small"; color: sw[3] }
                    }
                }
            }

            // Volume.
            RowLayout {
                visible: win.sink !== null && win.sink.audio !== null
                Layout.fillWidth: true
                spacing: Theme.s2
                Btn {
                    icon: win.sink && win.sink.audio && win.sink.audio.muted ? "mute" : "volume"
                    e2e: "quick-mute"
                    accessibleName: Tr.t("Mute")
                    checkable: true
                    checked: !!(win.sink && win.sink.audio && win.sink.audio.muted)
                    onClicked: if (win.sink && win.sink.audio) win.sink.audio.muted = !win.sink.audio.muted
                }
                Slider {
                    Layout.fillWidth: true
                    e2e: "quick-volume"
                    accessibleName: Tr.t("Volume")
                    from: 0; to: 1
                    value: win.sink && win.sink.audio ? win.sink.audio.volume : 0
                    onMoved: v => { if (win.sink && win.sink.audio) win.sink.audio.volume = v; }
                }
            }

            RowLayout {
                visible: UPower.displayDevice && UPower.displayDevice.isLaptopBattery
                Icon { name: "battery" }
                Txt { text: UPower.displayDevice ? Math.round(UPower.displayDevice.percentage * 100) + "% battery" : "" }
            }

            RowLayout {
                Layout.fillWidth: true
                Btn { text: "Settings"; icon: "sliders"; variant: "outline"; e2e: "qs-settings"; onClicked: { Ui.quickSettings = false; Ui.open("settings", "appearance"); } }
                Item { Layout.fillWidth: true }
                Btn { icon: "list"; text: "Activity"; e2e: "qs-activity"; onClicked: Ui.openFrom("activity", "", "quicksettings", "qs-activity") }
                // Lock, log out, suspend, restart, power off.
                Btn { icon: "power"; e2e: "qs-power"; accessibleName: Tr.t("Power"); onClicked: Ui.openFrom("power", "", "quicksettings", "qs-power") }
            }
        }
    }

    // A tile: a toggle (on, off) or a door to another surface.
    component Tile: Pressable {
        id: tile
        property string icon
        property string label
        property string sub
        property bool on
        property bool toggle: true
        // Two equal columns that always fit the card: the same preferred
        // width for every tile (the grid shares the width evenly), and the
        // text elides inside the tile's own padding.
        Layout.fillWidth: true
        Layout.preferredWidth: 1
        Layout.minimumWidth: 0
        implicitWidth: 1
        implicitHeight: Theme.fontSize * 5
        radius: Theme.radiusMd
        accessibleName: tile.label
        accessibleDescription: tile.sub
        checkable: toggle
        checked: toggle && on
        color: on ? Theme.accent : (hovered ? Theme.pressed : Theme.hover)
        clip: true
        Behavior on color { ColorAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        Icon {
            id: tileIcon
            name: tile.icon
            size: Theme.fontLarge * 1.5
            color: tile.on ? Theme.accentText : Theme.text
            anchors.left: parent.left
            anchors.leftMargin: Theme.s3
            anchors.verticalCenter: parent.verticalCenter
        }
        Column {
            anchors.left: tileIcon.right
            anchors.leftMargin: Theme.s3
            anchors.right: parent.right
            anchors.rightMargin: Theme.s3
            anchors.verticalCenter: parent.verticalCenter
            Txt { text: tile.label; font.weight: Font.DemiBold; color: tile.on ? Theme.accentText : Theme.text; width: parent.width }
            Txt { text: tile.sub; role: "small"; color: tile.on ? Theme.alpha(Theme.accentText, 0.8) : Theme.textMuted; width: parent.width }
        }
    }
}
