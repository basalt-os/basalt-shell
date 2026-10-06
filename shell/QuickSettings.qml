import QtQuick
import QtQuick.Layouts
import Quickshell
import Quickshell.Wayland
import Quickshell.Services.Pipewire
import Quickshell.Services.UPower

// Quick settings: light/dark, theme, motion, spoken answers, volume,
// network, battery and a door to the full settings page. Changes made
// here are the person's own, so they apply at once (audited as "ui").
PanelWindow {
    id: win
    visible: Ui.quickSettings || card.opacity > 0
    anchors { top: true; bottom: true; left: true; right: true }
    color: "transparent"
    exclusionMode: ExclusionMode.Normal
    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.namespace: "basalt-quicksettings"
    WlrLayershell.keyboardFocus: Ui.quickSettings && !Ui.modal ? WlrKeyboardFocus.OnDemand : WlrKeyboardFocus.None

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
        function onQuickSettingsChanged() { if (Ui.quickSettings) win.loadVoice(); }
    }
    Connections {
        target: Bus
        function onVoiceSettings(data) { win.voiceInfo = data; }
    }

    MouseArea { anchors.fill: parent; onClicked: Ui.quickSettings = false }

    Surface {
        id: card
        width: 380
        height: col.implicitHeight + Theme.s4 * 2
        anchors.right: parent.right
        anchors.rightMargin: Theme.s2
        y: (Theme.panelPosition === "bottom" ? parent.height - height - Theme.s2 : Theme.s2) + (Ui.quickSettings ? 0 : (Theme.panelPosition === "bottom" ? Theme.s4 : -Theme.s4))
        opacity: Ui.quickSettings ? 1 : 0
        Behavior on opacity { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        Behavior on y { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        MouseArea { anchors.fill: parent }

        ColumnLayout {
            id: col
            anchors.fill: parent
            anchors.margins: Theme.s4
            spacing: Theme.s3

            // Tiles.
            GridLayout {
                columns: 2
                Layout.fillWidth: true
                rowSpacing: Theme.s2
                columnSpacing: Theme.s2
                Tile {
                    icon: Theme.dark ? "moon" : "sun"
                    label: Theme.dark ? "Dark" : "Light"
                    sub: "Appearance"
                    on: Theme.dark
                    onClicked: Bus.act("theme.switch", { mode: "toggle" })
                }
                Tile {
                    icon: "motion"
                    label: Theme.animate ? "Animations on" : "Reduced motion"
                    sub: win.st && win.st.settings.motion === "auto" ? "Auto" + (win.st.hardware.weak ? " (weak GPU)" : "") : "Manual"
                    on: Theme.animate
                    onClicked: Bus.act("motion.set", { motion: Theme.animate ? "reduced" : "full" })
                }
                Tile {
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
                    icon: "spark"
                    label: "Ask the system"
                    sub: Bus.translatorAvailable ? "Local model" : "Commands"
                    on: false
                    onClicked: Ui.open("commandbar", "")
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
            Row {
                spacing: Theme.s2
                Repeater {
                    model: win.st ? win.st.themes : []
                    delegate: Rectangle {
                        required property var modelData
                        readonly property var sw: Theme.dark ? modelData.dark : modelData.light
                        width: 104; height: 58
                        radius: Theme.radiusMd
                        color: sw[0]
                        border.width: Theme.themeId === modelData.id ? 2 : 1
                        border.color: Theme.themeId === modelData.id ? Theme.accent : Theme.border
                        Rectangle { x: 8; y: 8; width: 40; height: 10; radius: 3; color: sw[1] }
                        Rectangle { x: 8; y: 22; width: 24; height: 10; radius: 3; color: sw[2] }
                        Txt { anchors.left: parent.left; anchors.bottom: parent.bottom; anchors.margins: 6; text: modelData.name; role: "small"; color: sw[3] }
                        MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: Bus.act("theme.switch", { theme: modelData.id }) }
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
                    onClicked: if (win.sink && win.sink.audio) win.sink.audio.muted = !win.sink.audio.muted
                }
                Slider {
                    Layout.fillWidth: true
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
                Btn { text: "Settings"; icon: "sliders"; variant: "outline"; onClicked: { Ui.quickSettings = false; Ui.open("settings", "appearance"); } }
                Item { Layout.fillWidth: true }
                Btn { icon: "list"; text: "Activity"; onClicked: Ui.open("activity", "") }
            }
        }
    }

    component Tile: Rectangle {
        id: tile
        property string icon
        property string label
        property string sub
        property bool on
        property string e2e: ""
        signal clicked()
        Layout.fillWidth: true
        implicitHeight: Theme.fontSize * 5
        radius: Theme.radiusMd
        color: on ? Theme.accent : (tma.containsMouse ? Theme.pressed : Theme.hover)
        Behavior on color { ColorAnimation { duration: Theme.normal; easing.type: Theme.easing } }
        Row {
            anchors.left: parent.left
            anchors.leftMargin: Theme.s3
            anchors.verticalCenter: parent.verticalCenter
            spacing: Theme.s3
            Icon { name: tile.icon; size: Theme.fontLarge * 1.5; color: tile.on ? Theme.accentText : Theme.text; anchors.verticalCenter: parent.verticalCenter }
            Column {
                anchors.verticalCenter: parent.verticalCenter
                Txt { text: tile.label; font.weight: Font.DemiBold; color: tile.on ? Theme.accentText : Theme.text; width: 120 }
                Txt { text: tile.sub; role: "small"; color: tile.on ? Theme.alpha(Theme.accentText, 0.8) : Theme.textMuted; width: 120 }
            }
        }
        MouseArea { id: tma; anchors.fill: parent; hoverEnabled: true; cursorShape: Qt.PointingHandCursor; onClicked: tile.clicked() }
    }
}
