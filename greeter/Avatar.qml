import QtQuick
import "logic.js" as L

// A person's picture (AccountsService icon) or their initials on the
// accent color, in a circle.
Item {
    id: a
    property var user: null
    property bool other: false
    property real size: 88
    property bool selected: false
    implicitWidth: size
    implicitHeight: size
    Accessible.ignored: true

    Rectangle {
        id: disc
        anchors.fill: parent
        radius: width / 2
        gradient: Gradient {
            GradientStop { position: 0; color: a.other ? G.surfaceAlt : Qt.lighter(G.accent, 1.18) }
            GradientStop { position: 1; color: a.other ? G.surface : Qt.darker(G.accent, 1.25) }
        }
        border.width: a.selected ? Math.max(2, a.size / 30) : 1
        border.color: a.selected ? G.accent : G.alpha(G.text, 0.18)
    }
    GTxt {
        anchors.centerIn: parent
        visible: !a.other && !pic.ready
        text: L.initials(L.displayName(a.user))
        color: G.accentText
        font.pointSize: Math.max(6, a.size * 0.30)
        font.weight: Font.DemiBold
    }
    GIcon {
        anchors.centerIn: parent
        visible: a.other
        name: "user-plus"
        size: a.size * 0.46
        color: G.text
    }
    // The picture, cropped to the circle with a Canvas (no shader
    // effects, so it also draws with the software renderer).
    Canvas {
        id: pic
        anchors.fill: parent
        anchors.margins: disc.border.width
        property bool ready: false
        readonly property string url: a.user && !a.other ? Sys.avatar(a.user.name) : ""
        onUrlChanged: { ready = false; if (url !== "") loadImage(url); requestPaint(); }
        Component.onCompleted: if (url !== "") loadImage(url)
        onImageLoaded: { ready = isImageLoaded(url); requestPaint(); }
        onPaint: {
            const ctx = getContext("2d");
            ctx.reset();
            if (!ready) return;
            ctx.save();
            ctx.beginPath();
            ctx.arc(width / 2, height / 2, Math.min(width, height) / 2, 0, Math.PI * 2);
            ctx.closePath();
            ctx.clip();
            ctx.drawImage(url, 0, 0, width, height);
            ctx.restore();
        }
    }
}
