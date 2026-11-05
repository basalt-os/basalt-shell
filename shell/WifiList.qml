import QtQuick
import QtQuick.Layouts

// Quick settings, network: the Wi-Fi networks in range and joining one,
// opened from the network tile. The daemon asks NetworkManager (nmcli);
// a network's password is typed here and goes to the daemon, then to
// nmcli's standard input, nowhere else. Anything that fails says so on
// the line under the list: nothing fails silently. "Network settings"
// opens nm-connection-editor, or nmtui in a terminal.
ColumnLayout {
    id: wl
    spacing: Theme.s2

    property var st: null          // the daemon's WifiState
    property bool loading: false
    property string error: ""
    property string info: ""
    // The network asking for its password, and the one being joined.
    property string asking: ""
    property string joining: ""

    function load(rescan) {
        wl.loading = true;
        Bus.call("network.wifi", { rescan: !!rescan }, (ok, res) => {
            wl.loading = false;
            if (ok) { wl.st = res; if (res.error) wl.error = res.error; }
            else wl.error = res;
        });
    }
    function reset() { wl.error = ""; wl.info = ""; wl.asking = ""; pw.text = ""; }
    function pick(n) {
        wl.error = ""; wl.info = "";
        if (n.active) return;
        if (n.enterprise) { wl.error = Tr.t("%1 uses a company login (802.1X): join it in the network settings.").arg(n.ssid); return; }
        if (n.secure && !n.known) {
            wl.asking = n.ssid;
            pw.text = "";
            Qt.callLater(pw.focusInput);
            return;
        }
        wl.join(n.ssid, "");
    }
    function join(ssid, password) {
        wl.joining = ssid;
        wl.error = ""; wl.info = "";
        Bus.call("network.wifi.connect", { ssid: ssid, password: password }, (ok, res) => {
            wl.joining = "";
            if (ok) {
                wl.st = res;
                wl.asking = "";
                pw.text = "";
                wl.info = Tr.t("Connected to %1").arg(ssid);
                Net.refresh();
            } else {
                wl.error = res;
                // A refused password: ask again for that network.
                if (password !== "") { wl.asking = ssid; pw.text = ""; Qt.callLater(pw.focusInput); }
            }
        });
    }
    function setRadio(on) {
        wl.loading = true;
        Bus.call("network.wifi.radio", { on: on }, (ok, res) => {
            wl.loading = false;
            if (ok) wl.st = res; else wl.error = res;
            Net.refresh();
        });
    }
    function openSettings() {
        wl.error = "";
        Bus.call("network.settings", {}, (ok, res) => {
            if (ok) Ui.quickSettings = false;
            else wl.error = res;
        });
    }

    readonly property bool hasDevice: st !== null && st.device !== ""
    readonly property var networks: st && st.networks ? st.networks.slice(0, 8) : []

    RowLayout {
        Layout.fillWidth: true
        spacing: Theme.s2
        Txt { text: Tr.t("Wi-Fi"); font.weight: Font.DemiBold; Layout.fillWidth: true }
        Btn {
            visible: wl.hasDevice && wl.st.radio
            icon: "restart"
            enabled: !wl.loading
            e2e: "wifi-rescan"
            accessibleName: Tr.t("Search again")
            onClicked: wl.load(true)
        }
        Toggle {
            visible: wl.hasDevice
            e2e: "wifi-radio"
            label: Tr.t("Wi-Fi")
            checked: wl.hasDevice && wl.st.radio
            busy: wl.loading
            onToggled: wl.setRadio(!checked)
        }
    }

    Txt {
        visible: wl.st !== null && !wl.hasDevice
        text: Tr.t("This computer has no Wi-Fi adapter that works. A cable or the network settings can connect it.")
        role: "small"; color: Theme.textMuted
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
    }
    Txt {
        visible: wl.hasDevice && !wl.st.radio
        text: Tr.t("Wi-Fi is off.")
        role: "small"; color: Theme.textMuted
        Layout.fillWidth: true
    }
    Txt {
        visible: wl.hasDevice && wl.st.radio && wl.networks.length === 0
        text: wl.loading ? Tr.t("Searching for networks") : Tr.t("No networks in range.")
        role: "small"; color: Theme.textMuted
        Layout.fillWidth: true
    }

    NavColumn {
        id: list
        visible: wl.networks.length > 0
        Layout.fillWidth: true
        spacing: 2
        Accessible.name: Tr.t("Wi-Fi networks")
        Repeater {
            model: wl.networks
            delegate: Pressable {
                id: row
                required property var modelData
                width: list.width
                implicitHeight: Theme.fontSize * 3.6
                radius: Theme.radiusMd
                color: modelData.active ? Theme.alpha(Theme.accent, 0.18) : (hovered ? Theme.hover : "transparent")
                e2e: "wifi-net:" + modelData.ssid
                accessibleName: modelData.ssid
                accessibleDescription: netSub.text
                onClicked: wl.pick(modelData)
                Icon {
                    id: netIcon
                    name: "wifi"
                    size: Theme.fontLarge * 1.3
                    color: Theme.text
                    opacity: row.modelData.signal >= 50 ? 1 : 0.6
                    anchors.left: parent.left; anchors.leftMargin: Theme.s2
                    anchors.verticalCenter: parent.verticalCenter
                }
                Column {
                    anchors.left: netIcon.right; anchors.leftMargin: Theme.s2
                    anchors.right: lockIcon.left; anchors.rightMargin: Theme.s2
                    anchors.verticalCenter: parent.verticalCenter
                    Txt { text: row.modelData.ssid; width: parent.width; font.weight: row.modelData.active ? Font.DemiBold : Font.Normal }
                    Txt {
                        id: netSub
                        width: parent.width
                        role: "small"; color: Theme.textMuted
                        text: wl.joining === row.modelData.ssid ? Tr.t("Connecting")
                              : (row.modelData.active ? Tr.t("Connected")
                              : (row.modelData.known ? Tr.t("Saved") : (row.modelData.secure ? Tr.t("Needs a password") : Tr.t("Open network"))))
                    }
                }
                Icon {
                    id: lockIcon
                    name: row.modelData.active ? "check" : "lock"
                    visible: row.modelData.active || row.modelData.secure
                    size: Theme.fontSize * 1.3
                    color: Theme.textMuted
                    anchors.right: parent.right; anchors.rightMargin: Theme.s2
                    anchors.verticalCenter: parent.verticalCenter
                }
            }
        }
    }

    // The password of the network the person picked.
    ColumnLayout {
        visible: wl.asking !== ""
        Layout.fillWidth: true
        spacing: Theme.s2
        Txt { text: Tr.t("Password for %1").arg(wl.asking); role: "small"; Layout.fillWidth: true }
        Field {
            id: pw
            Layout.fillWidth: true
            e2e: "wifi-password"
            icon: "key"
            placeholder: Tr.t("Password")
            accessibleName: Tr.t("Password for %1").arg(wl.asking)
            input.echoMode: TextInput.Password
            input.inputMethodHints: Qt.ImhSensitiveData | Qt.ImhNoPredictiveText | Qt.ImhHiddenText
            onAccepted: if (pw.text !== "" && wl.joining === "") wl.join(wl.asking, pw.text)
            onEscapePressed: { wl.asking = ""; pw.text = ""; }
        }
        RowLayout {
            Layout.fillWidth: true
            Item { Layout.fillWidth: true }
            Btn { text: Tr.t("Cancel"); e2e: "wifi-cancel"; onClicked: { wl.asking = ""; pw.text = ""; } }
            Btn {
                text: Tr.t("Connect"); variant: "primary"; e2e: "wifi-connect"
                enabled: pw.text !== "" && wl.joining === ""
                onClicked: wl.join(wl.asking, pw.text)
            }
        }
    }

    Txt {
        visible: wl.error !== "" || wl.info !== ""
        text: wl.error !== "" ? wl.error : wl.info
        color: wl.error !== "" ? Theme.danger : Theme.textMuted
        role: "small"
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
        Accessible.role: Accessible.StaticText
        Accessible.name: text
    }

    Btn {
        text: Tr.t("Network settings"); icon: "sliders"; variant: "outline"; e2e: "wifi-settings"
        visible: wl.st !== null
        onClicked: wl.openSettings()
    }
}
