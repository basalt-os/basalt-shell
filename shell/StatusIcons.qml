import QtQuick
import Quickshell
import Quickshell.Services.Pipewire
import Quickshell.Services.UPower

// Network, audio and battery state from D-Bus (NetworkManager, PipeWire,
// UPower), through Quickshell's service modules. Clicking opens quick
// settings.
Row {
    id: st
    spacing: Theme.s2
    readonly property var sink: Pipewire.defaultAudioSink
    readonly property bool online: Net.online
    readonly property bool wifi: Net.kind === "wifi"
    readonly property var battery: UPower.displayDevice

    PwObjectTracker { objects: st.sink ? [st.sink] : [] }

    // Privacy indicators: an application is capturing the screen or a
    // camera (a video input stream), or recording audio.
    readonly property var streams: Pipewire.nodes ? Pipewire.nodes.values.filter(n => !n.isSink && !(n.audio && !n.isStream)) : []
    PwObjectTracker { objects: st.streams }
    function streamClass(n) { return n && n.properties ? (n.properties["media.class"] || "") : ""; }
    readonly property bool capturingVideo: streams.some(n => streamClass(n) === "Stream/Input/Video")
    readonly property bool capturingAudio: streams.some(n => streamClass(n) === "Stream/Input/Audio")

    Rectangle {
        visible: st.capturingVideo || st.capturingAudio
        anchors.verticalCenter: parent.verticalCenter
        height: parent.height - Theme.s2
        width: privRow.implicitWidth + Theme.s3
        radius: height / 2
        color: Theme.danger
        Row {
            id: privRow
            anchors.centerIn: parent
            spacing: Theme.s1
            Icon { visible: st.capturingVideo; name: "screen"; color: "#ffffff"; size: Theme.fontSize * 1.3; anchors.verticalCenter: parent.verticalCenter }
            Icon { visible: st.capturingAudio; name: "mic"; color: "#ffffff"; size: Theme.fontSize * 1.3; anchors.verticalCenter: parent.verticalCenter }
            Txt { text: st.capturingVideo ? "Sharing" : "Mic on"; color: "#ffffff"; role: "small"; font.weight: Font.DemiBold; anchors.verticalCenter: parent.verticalCenter }
        }
    }

    Item {
        width: row.implicitWidth + Theme.s3
        height: parent.height
        Rectangle { anchors.fill: parent; radius: Theme.radiusSm; color: ma.containsMouse ? Theme.hover : "transparent" }
        Row {
            id: row
            anchors.centerIn: parent
            spacing: Theme.s2
            Icon {
                name: !st.online ? "offline" : (st.wifi ? "wifi" : "network")
                size: Theme.fontSize * 1.5
                color: !st.online ? Theme.textMuted : Theme.text
                anchors.verticalCenter: parent.verticalCenter
            }
            Icon {
                visible: st.sink !== null
                name: (st.sink && st.sink.audio && st.sink.audio.muted) ? "mute" : "volume"
                size: Theme.fontSize * 1.5
                anchors.verticalCenter: parent.verticalCenter
            }
            Txt {
                visible: st.sink !== null && st.sink.audio !== null
                text: st.sink && st.sink.audio ? Math.round(st.sink.audio.volume * 100) + "%" : ""
                role: "small"
                anchors.verticalCenter: parent.verticalCenter
            }
            Icon {
                visible: st.battery && st.battery.isLaptopBattery
                name: st.battery && st.battery.state === UPowerDeviceState.Charging ? "charging" : "battery"
                size: Theme.fontSize * 1.5
                anchors.verticalCenter: parent.verticalCenter
            }
            Txt {
                visible: st.battery && st.battery.isLaptopBattery
                text: st.battery ? Math.round(st.battery.percentage * 100) + "%" : ""
                role: "small"
                anchors.verticalCenter: parent.verticalCenter
            }
        }
        MouseArea {
            id: ma
            anchors.fill: parent
            hoverEnabled: true
            cursorShape: Qt.PointingHandCursor
            onClicked: Ui.toggle("quicksettings")
            onWheel: wheel => {
                if (st.sink && st.sink.audio) st.sink.audio.volume = Math.max(0, Math.min(1.5, st.sink.audio.volume + (wheel.angleDelta.y > 0 ? 0.05 : -0.05)));
            }
        }
    }
}
