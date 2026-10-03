import QtQuick
import Quickshell
import Quickshell.Wayland

// Wallpaper on the background layer: follows light and dark mode, with a
// short cross-fade when motion is on.
PanelWindow {
    id: wp
    required property var modelData
    screen: modelData
    anchors { top: true; bottom: true; left: true; right: true }
    exclusionMode: ExclusionMode.Ignore
    WlrLayershell.layer: WlrLayer.Background
    WlrLayershell.namespace: "basalt-wallpaper"
    color: Theme.bg

    readonly property string size: wp.width * (screen ? screen.devicePixelRatio : 1) > 2000 ? "3840x2160" : "1920x1080"
    function src(m) { return Qt.resolvedUrl("wallpapers/basalt-" + m + "-" + wp.size + ".png"); }

    Image {
        anchors.fill: parent
        fillMode: Image.PreserveAspectCrop
        source: wp.src("dark")
        opacity: Theme.dark ? 1 : 0
        asynchronous: true
        Behavior on opacity { NumberAnimation { duration: Theme.slow; easing.type: Theme.easing } }
    }
    Image {
        anchors.fill: parent
        fillMode: Image.PreserveAspectCrop
        source: wp.src("light")
        opacity: Theme.dark ? 0 : 1
        asynchronous: true
        Behavior on opacity { NumberAnimation { duration: Theme.slow; easing.type: Theme.easing } }
    }
}
