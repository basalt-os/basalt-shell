import QtQuick
import Quickshell
import Quickshell.Services.Pipewire
import Quickshell.Services.UPower
import Quickshell.Networking

// Network, audio and battery state from D-Bus (NetworkManager, PipeWire,
// UPower), through Quickshell's service modules. Clicking opens quick
// settings.
Row {
    id: st
    spacing: Theme.s2
    readonly property var sink: Pipewire.defaultAudioSink
    readonly property var net: {
        const devs = Networking.devices ? Networking.devices.values : [];
        for (let i = 0; i < devs.length; i++) if (devs[i].connected) return devs[i];
        return null;
    }
    readonly property bool wifi: net !== null && net.type === DeviceType.Wifi
    readonly property var battery: UPower.displayDevice

    PwObjectTracker { objects: st.sink ? [st.sink] : [] }

    Item {
        width: row.implicitWidth + Theme.s3
        height: parent.height
        Rectangle { anchors.fill: parent; radius: Theme.radiusSm; color: ma.containsMouse ? Theme.hover : "transparent" }
        Row {
            id: row
            anchors.centerIn: parent
            spacing: Theme.s2
            Icon {
                name: st.net === null ? "offline" : (st.wifi ? "wifi" : "network")
                size: Theme.fontSize * 1.5
                color: st.net === null ? Theme.textMuted : Theme.text
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
