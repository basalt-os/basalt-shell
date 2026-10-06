import QtQuick
import QtQuick.Effects
import Quickshell
import Quickshell.Wayland
import "logic.js" as L

// One output of the login screen: the wallpaper, softly blurred, the
// clock, and on the main output the login card, the people and the
// status and power buttons.
PanelWindow {
    id: win
    required property var modelData
    readonly property bool main: Quickshell.screens.length === 0 || modelData === Quickshell.screens[0]
    screen: modelData
    anchors { top: true; bottom: true; left: true; right: true }
    exclusionMode: ExclusionMode.Ignore
    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.namespace: "basalt-greeter"
    WlrLayershell.keyboardFocus: main ? WlrKeyboardFocus.Exclusive : WlrKeyboardFocus.None
    color: G.bg

    // Appear: the blur and the card come in; leave: everything but the
    // sharp wallpaper fades, so the session (same wallpaper) follows.
    property real reveal: 0
    property bool leaving: false
    Component.onCompleted: revealAnim.start()
    NumberAnimation { id: revealAnim; target: win; property: "reveal"; from: 0; to: 1; duration: Math.max(G.slow * 2, 1); easing.type: Easing.OutCubic }
    Connections {
        target: Login
        function onLaunching() { win.leaving = true; leaveAnim.start(); }
    }
    NumberAnimation { id: leaveAnim; target: win; property: "reveal"; to: 0; duration: Math.max(G.slow * 1.5, 1); easing.type: Easing.InOutQuad }

    readonly property string wallSize: win.width > 2000 ? "3840x2160" : "1920x1080"
    readonly property string wallMode: G.mode === "light" ? "light" : "dark"

    Image {
        id: wall
        anchors.fill: parent
        visible: !G.highContrast
        fillMode: Image.PreserveAspectCrop
        asynchronous: false
        source: "file://" + G.shellData + "/qml/wallpapers/basalt-" + win.wallMode + "-" + win.wallSize + ".png"
    }
    // The blur sits over the plain picture: with Qt Quick's software
    // renderer (no shader effects) the picture still shows.
    MultiEffect {
        anchors.fill: parent
        visible: !G.highContrast
        source: wall
        autoPaddingEnabled: false
        blurEnabled: true
        blurMax: 64
        blur: 0.62 * win.reveal
        brightness: win.leaving ? 0 : -0.08 * win.reveal
    }
    // A gentle shade where the text sits.
    Rectangle {
        anchors.fill: parent
        visible: !G.highContrast
        opacity: win.reveal
        gradient: Gradient {
            GradientStop { position: 0.0; color: G.alpha("#000000", G.dark ? 0.30 : 0.08) }
            GradientStop { position: 0.45; color: G.alpha("#000000", G.dark ? 0.10 : 0.0) }
            GradientStop { position: 1.0; color: G.alpha("#000000", G.dark ? 0.40 : 0.12) }
        }
    }

    // Escape anywhere on the screen (not taken by a menu): back to the
    // remembered person, the cursor in the field.
    function backToField() {
        if (Login.other && Sys.users.length > 0) Login.choose(L.pickUser(Sys.users, Sys.state.lastUser) || Sys.users[0]);
        card.focusField();
    }

    Item {
        id: ui
        anchors.fill: parent
        opacity: win.reveal
        Keys.onEscapePressed: win.backToField()
        // Content moves up a little as it appears.
        transform: Translate { y: (1 - win.reveal) * 18 }

        // ------------------------------------------------------------ top bar
        Row {
            id: brand
            anchors.left: parent.left
            anchors.top: parent.top
            anchors.margins: G.s6
            spacing: G.s2
            GIcon { name: "logo"; size: G.fontLarge * 1.9; color: G.text; anchors.verticalCenter: parent.verticalCenter }
            GTxt { text: Sys.osName; role: "large"; font.weight: Font.DemiBold; anchors.verticalCenter: parent.verticalCenter }
        }
        TopBar {
            id: top
            visible: win.main
            popoverHost: ui
            anchors.right: parent.right
            anchors.top: parent.top
            anchors.margins: G.s6 - G.s1
        }

        // ------------------------------------------------------------ clock
        Column {
            id: clock
            anchors.horizontalCenter: parent.horizontalCenter
            y: win.main ? Math.max(G.s8 * 2, center.y - implicitHeight - G.s8 * 1.5) : parent.height * 0.36
            spacing: 0
            property date now: new Date()
            Timer { interval: 1000; running: true; repeat: true; onTriggered: clock.now = new Date() }
            GTxt {
                anchors.horizontalCenter: parent.horizontalCenter
                role: "clock"
                text: clock.now.toLocaleTimeString(I18n.locale, Locale.ShortFormat)
                Accessible.ignored: true
            }
            GTxt {
                anchors.horizontalCenter: parent.horizontalCenter
                role: "large"
                color: G.alpha(G.text, 0.86)
                // The date's format is part of the translation (its order
                // and words differ by language).
                text: clock.now.toLocaleDateString(I18n.locale, I18n.t("dddd, MMMM d"))
            }
        }

        // ------------------------------------------------------------ people and card
        Item {
            id: center
            visible: win.main
            anchors.horizontalCenter: parent.horizontalCenter
            y: Math.round(parent.height * 0.5 - height * 0.32)
            width: chooseList.visible ? chooseList.implicitWidth : card.implicitWidth
            height: chooseList.visible ? chooseList.implicitHeight : card.implicitHeight

            LoginCard {
                id: card
                visible: Login.user !== null || Login.other
                anchors.horizontalCenter: parent.horizontalCenter
                width: implicitWidth
                height: implicitHeight
                popoverHost: ui
                maxWidth: win.width - G.s8
            }
            // Several people and nobody remembered: choose first.
            UserPicker {
                id: chooseList
                large: true
                visible: !card.visible
                anchors.horizontalCenter: parent.horizontalCenter
            }
        }
        // Switch to someone else.
        UserPicker {
            id: picker
            visible: win.main && card.visible && (Sys.users.length > 1 || Login.other || Sys.users.length === 0)
            anchors.horizontalCenter: parent.horizontalCenter
            anchors.bottom: parent.bottom
            anchors.bottomMargin: G.s8
        }
        GBtn {
            // One person on this computer: "other user" stays reachable.
            visible: win.main && card.visible && Sys.users.length === 1 && !Login.other
            anchors.horizontalCenter: parent.horizontalCenter
            anchors.bottom: parent.bottom
            anchors.bottomMargin: G.s8
            variant: "chip"
            icon: "user-plus"
            text: I18n.t("Other user")
            e2e: "other-user"
            onClicked: { Login.chooseOther(); card.focusField(); }
        }
    }

    // Keys that work everywhere on the screen.
    Item {
        anchors.fill: parent
        focus: win.main
        Keys.onEscapePressed: win.backToField()
    }

    // Start: pick the remembered person and put the cursor in the field.
    Timer {
        interval: 50
        running: win.main
        onTriggered: {
            if (Sys.users.length === 0) Login.chooseOther();
            else {
                const u = L.pickUser(Sys.users, Sys.state.lastUser);
                if (u) Login.choose(u);
            }
            card.focusField();
            Sys.markReady();
        }
    }
    Connections {
        target: Login
        function onUserChanged() { Qt.callLater(card.focusField); }
        function onOtherChanged() { Qt.callLater(card.focusField); }
        function onFocusRequested() { Qt.callLater(card.focusField); }
    }
}
