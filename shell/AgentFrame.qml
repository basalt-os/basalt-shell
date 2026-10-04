import QtQuick
import Quickshell
import Quickshell.Wayland

// While an agent holds a control session (it may see the screen and type
// or click), every screen gets a frame in the warning color and a banner
// with who is in control, what for, the time left and a Stop button. The
// frame lets clicks through; only the banner takes input. A screenshot
// taken by an agent outside a session flashes a short banner too.
Variants {
    model: Quickshell.screens
    delegate: PanelWindow {
        id: win
        required property var modelData
        screen: modelData
        readonly property var ctl: Bus.control
        property bool flash: false
        property string flashText: ""
        visible: ctl !== null || flash
        anchors { top: true; bottom: true; left: true; right: true }
        color: "transparent"
        exclusionMode: ExclusionMode.Ignore
        WlrLayershell.layer: WlrLayer.Overlay
        WlrLayershell.namespace: "basalt-agent-frame"
        WlrLayershell.keyboardFocus: WlrKeyboardFocus.None
        mask: Region { item: banner }

        property int left: 0
        Timer {
            interval: 1000; repeat: true; triggeredOnStart: true; running: win.ctl !== null
            onTriggered: win.left = win.ctl ? Math.max(0, Math.round((new Date(win.ctl.expires) - new Date()) / 1000)) : 0
        }
        Timer { id: flashTimer; interval: 3500; onTriggered: win.flash = false }
        Connections {
            target: Bus
            function onAgentActivity(d) {
                if (d.kind === "capture" && Bus.control === null) {
                    win.flashText = "Screenshot shown to " + d.actor;
                    win.flash = true;
                    flashTimer.restart();
                }
            }
        }

        Rectangle {
            anchors.fill: parent
            color: "transparent"
            border.width: win.ctl !== null ? 4 : 0
            border.color: Theme.warning
        }

        Rectangle {
            id: banner
            anchors.horizontalCenter: parent.horizontalCenter
            anchors.bottom: parent.bottom
            anchors.bottomMargin: Theme.s4
            width: row.implicitWidth + Theme.s4 * 2
            height: Theme.panelHeight + Theme.s2
            radius: height / 2
            color: Theme.warning
            Row {
                id: row
                anchors.centerIn: parent
                spacing: Theme.s3
                Icon { name: "spark"; color: "#1b1b1b"; size: Theme.fontSize * 1.6; anchors.verticalCenter: parent.verticalCenter }
                Txt {
                    anchors.verticalCenter: parent.verticalCenter
                    color: "#1b1b1b"
                    font.weight: Font.DemiBold
                    text: win.ctl !== null
                        ? (win.ctl.actor + " is controlling the desktop" + (win.ctl.input ? " (keyboard and pointer)" : " (screen)") +
                           "  " + Math.floor(win.left / 60) + ":" + ("0" + win.left % 60).slice(-2))
                        : win.flashText
                }
                Rectangle {
                    visible: win.ctl !== null
                    anchors.verticalCenter: parent.verticalCenter
                    width: stopTxt.implicitWidth + Theme.s4; height: banner.height - Theme.s3; radius: height / 2
                    color: stopMa.containsMouse ? "#000000" : "#1b1b1b"
                    Txt { id: stopTxt; anchors.centerIn: parent; text: "Stop  (Super+Shift+Esc)"; color: "#ffffff"; font.weight: Font.DemiBold; role: "small" }
                    MouseArea { id: stopMa; anchors.fill: parent; hoverEnabled: true; cursorShape: Qt.PointingHandCursor; onClicked: Bus.stopControl() }
                }
            }
        }
    }
}
