import QtQuick
import QtQuick.Effects
import Quickshell
import Quickshell.Wayland
import Quickshell.Services.UPower
import "lock.js" as L

// One output of the lock screen, in the login screen's look: the
// wallpaper, softly blurred, the time and the date, and on the output with
// the keyboard the card with the person's picture and name, the password
// field (focused, with its hint), Caps Lock, what went wrong and the
// keyboard layout. Everything is drawn in the first frame: no fade from
// blank, so locking never looks like a frozen, empty screen.
//
// Nothing from the desktop shows here: no notification text, no windows,
// no previews (the compositor draws only these surfaces while locked).
WlSessionLockSurface {
    id: surf
    property var ctl
    color: Theme.bg

    readonly property string outName: surf.screen ? surf.screen.name : ""
    readonly property bool cardHere: ctl.cardOutput === "" || ctl.cardOutput === outName
    readonly property bool leaving: ctl.phase === "unlocking"

    // Sizes as on the login screen: a little larger than the desktop's.
    readonly property real base: Theme.fontSize * 1.15
    readonly property real large: Math.round(base * Theme.scale * 10) / 10
    readonly property real title: Math.round(base * Theme.scale * Theme.scale * 10) / 10
    readonly property real clockSize: Math.round(base * 5.2 * 10) / 10
    readonly property real small: Math.round(base / Theme.scale * 10) / 10

    // The session's language for the clock and the date.
    readonly property var loc: Bus.ready && Bus.uiLang !== "" ? Qt.locale(Bus.uiLang.replace("-", "_")) : Qt.locale()

    readonly property string wallSize: surf.width > 2000 ? "3840x2160" : "1920x1080"

    function focusKeys() {
        if (surf.cardHere) card.focusField(); else relay.forceActiveFocus();
    }

    Image {
        id: wall
        anchors.fill: parent
        fillMode: Image.PreserveAspectCrop
        // Loaded before the first frame: the screen is never blank.
        asynchronous: false
        source: Qt.resolvedUrl("wallpapers/basalt-" + (Theme.dark ? "dark" : "light") + "-" + surf.wallSize + ".png")
    }
    // The blur sits over the plain picture: with Qt Quick's software
    // renderer (no shader effects) the picture still shows.
    MultiEffect {
        anchors.fill: parent
        source: wall
        autoPaddingEnabled: false
        blurEnabled: true
        blurMax: 64
        blur: surf.leaving ? 0 : 0.62
        brightness: surf.leaving ? 0 : -0.08
        Behavior on blur { NumberAnimation { duration: Theme.normal; easing.type: Theme.easing } }
    }
    // A gentle shade where the text sits.
    Rectangle {
        anchors.fill: parent
        opacity: surf.leaving ? 0 : 1
        Behavior on opacity { NumberAnimation { duration: Theme.normal } }
        gradient: Gradient {
            GradientStop { position: 0.0; color: Theme.alpha("#000000", Theme.dark ? 0.30 : 0.08) }
            GradientStop { position: 0.45; color: Theme.alpha("#000000", Theme.dark ? 0.10 : 0.0) }
            GradientStop { position: 1.0; color: Theme.alpha("#000000", Theme.dark ? 0.40 : 0.12) }
        }
    }

    Item {
        id: ui
        anchors.fill: parent
        opacity: surf.leaving ? 0 : 1
        Behavior on opacity { NumberAnimation { duration: Theme.normal } }

        // The surface with the keyboard: its card's field takes the keys,
        // or, on an output without the card, a hidden field that types
        // into the card's (Lock.typed).
        readonly property bool hasKeyboard: Window.active
        onHasKeyboardChanged: if (hasKeyboard) Qt.callLater(surf.focusKeys)

        TextInput {
            id: relay
            visible: !surf.cardHere
            opacity: 0
            width: 1
            height: 1
            echoMode: TextInput.Password
            inputMethodHints: Qt.ImhSensitiveData | Qt.ImhNoPredictiveText | Qt.ImhHiddenText | Qt.ImhNoAutoUppercase
            readOnly: surf.ctl.busy
            text: surf.ctl.typed
            onTextEdited: surf.ctl.typed = text
            onActiveFocusChanged: Ui.noteFocused("lock-relay", activeFocus, relay)
            Accessible.ignored: true
            Keys.onPressed: event => {
                if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter) { surf.ctl.submitTyped(); event.accepted = true; return; }
                if (event.key === Qt.Key_Escape) { surf.ctl.clearTyped(); event.accepted = true; return; }
                surf.ctl.noteKey(event);
                event.accepted = false;
            }
        }

        // ------------------------------------------------------------ top bar
        Row {
            anchors.left: parent.left
            anchors.top: parent.top
            anchors.margins: Theme.s6
            spacing: Theme.s2
            Icon { name: "logo"; size: surf.large * 1.9; color: Theme.text; anchors.verticalCenter: parent.verticalCenter }
            Txt { text: surf.ctl.osName; font.pointSize: surf.large; font.weight: Font.DemiBold; anchors.verticalCenter: parent.verticalCenter }
        }
        Row {
            id: status
            visible: surf.cardHere
            anchors.right: parent.right
            anchors.top: parent.top
            anchors.margins: Theme.s6 - Theme.s1
            spacing: Theme.s2
            readonly property var battery: UPower.displayDevice
            readonly property bool hasBattery: battery !== null && battery.isLaptopBattery
            readonly property int percent: hasBattery ? Math.round(battery.percentage * (battery.percentage <= 1 ? 100 : 1)) : 0
            readonly property bool charging: hasBattery && (battery.state === UPowerDeviceState.Charging || battery.state === UPowerDeviceState.FullyCharged)

            // The keyboard layout: press it to switch when there are several.
            Btn {
                id: layoutBtn
                visible: surf.ctl.layoutLabel !== ""
                icon: "keyboard"
                text: surf.ctl.layoutLabel
                e2e: "lock-layout"
                focusable: surf.ctl.canSwitchLayout
                color: hovered && surf.ctl.canSwitchLayout ? Theme.hover : Theme.alpha(Theme.bg, 0.45)
                radius: height / 2
                accessibleName: surf.ctl.canSwitchLayout ? Tr.t("Keyboard layout: %1. Press to switch.").arg(surf.ctl.layoutName)
                                                         : Tr.t("Keyboard layout: %1").arg(surf.ctl.layoutName)
                onClicked: { surf.ctl.nextLayout(); surf.focusKeys(); }
            }
            Rectangle {
                height: layoutBtn.implicitHeight
                width: netRow.implicitWidth + Theme.s4
                radius: height / 2
                color: Theme.alpha(Theme.bg, 0.45)
                Accessible.role: Accessible.StaticText
                Accessible.name: !Net.online ? Tr.t("Offline") : (Net.kind === "wifi" ? Tr.t("Wi-Fi") : Tr.t("Wired network"))
                Row {
                    id: netRow
                    anchors.centerIn: parent
                    spacing: Theme.s2
                    Icon {
                        name: !Net.online ? "offline" : (Net.kind === "wifi" ? "wifi" : "network")
                        size: Theme.fontSize * 1.5
                        color: Net.online ? Theme.text : Theme.textMuted
                        anchors.verticalCenter: parent.verticalCenter
                    }
                    Icon {
                        visible: status.hasBattery
                        name: status.charging ? "charging" : "battery"
                        size: Theme.fontSize * 1.5
                        anchors.verticalCenter: parent.verticalCenter
                    }
                    Txt {
                        visible: status.hasBattery
                        text: status.percent + "%"
                        role: "small"
                        anchors.verticalCenter: parent.verticalCenter
                        Accessible.name: Tr.t("Battery %1%").arg(status.percent)
                    }
                }
            }
        }

        // ------------------------------------------------------------ clock
        Column {
            id: clock
            anchors.horizontalCenter: parent.horizontalCenter
            y: surf.cardHere ? Math.max(Theme.s6 * 3, card.y - implicitHeight - Theme.s6 * 2) : parent.height * 0.36
            spacing: 0
            SystemClock { id: sysClock; precision: SystemClock.Minutes }
            Txt {
                anchors.horizontalCenter: parent.horizontalCenter
                font.pointSize: surf.clockSize
                font.weight: Font.Light
                text: sysClock.date.toLocaleTimeString(surf.loc, Locale.ShortFormat)
                Accessible.ignored: true
            }
            Txt {
                anchors.horizontalCenter: parent.horizontalCenter
                font.pointSize: surf.large
                color: Theme.alpha(Theme.text, 0.86)
                // The date's format is part of the translation (its order
                // and words differ by language).
                text: sysClock.date.toLocaleDateString(surf.loc, Tr.t("dddd, MMMM d"))
            }
        }

        // ------------------------------------------------------------ card
        FocusScope {
            id: card
            visible: surf.cardHere
            focus: surf.cardHere
            width: Math.min(420 * (surf.base / (Theme.fontSize * 1.15)), parent.width - Theme.s6 * 2)
            height: body.implicitHeight + Theme.s6 * 2
            anchors.horizontalCenter: parent.horizontalCenter
            y: Math.round(parent.height * 0.5 - height * 0.32)
            Accessible.role: Accessible.Dialog
            Accessible.name: Tr.t("Type your password to unlock")

            function focusField() { if (surf.cardHere) field.focusInput(); }
            onVisibleChanged: if (visible) Qt.callLater(focusField)

            // Shake on a refused password, then a fresh empty field.
            property real shake: 0
            transform: Translate { x: card.shake }
            SequentialAnimation {
                id: shakeAnim
                NumberAnimation { target: card; property: "shake"; to: -12; duration: 50 }
                NumberAnimation { target: card; property: "shake"; to: 10; duration: 70 }
                NumberAnimation { target: card; property: "shake"; to: -6; duration: 70 }
                NumberAnimation { target: card; property: "shake"; to: 3; duration: 60 }
                NumberAnimation { target: card; property: "shake"; to: 0; duration: 50 }
            }
            Connections {
                target: surf.ctl
                function onRefusalsChanged() {
                    if (Theme.animate) shakeAnim.restart();
                    surf.focusKeys();
                }
            }

            RectangularShadow {
                anchors.fill: cardBg
                radius: cardBg.radius
                blur: 48
                spread: 0
                offset.y: 12
                color: Theme.alpha("#000000", Theme.dark ? 0.45 : 0.18)
                visible: Theme.animate
            }
            Rectangle {
                id: cardBg
                anchors.fill: parent
                radius: Theme.radiusLg + 4
                color: Qt.rgba(Theme.surface.r, Theme.surface.g, Theme.surface.b, Theme.dark ? 0.82 : 0.88)
                border.width: 1
                border.color: Theme.alpha(Theme.border, 0.9)
            }

            Column {
                id: body
                anchors.left: parent.left
                anchors.right: parent.right
                anchors.top: parent.top
                anchors.margins: Theme.s6
                spacing: Theme.s3

                // The person's picture (AccountsService, or ~/.face) or
                // their initials, in a circle.
                Item {
                    id: avatar
                    anchors.horizontalCenter: parent.horizontalCenter
                    width: 88 * (surf.base / (Theme.fontSize * 1.15))
                    height: width
                    Accessible.ignored: true
                    readonly property string picture: acct.status === Image.Ready ? acct.source.toString()
                                                   : (face.status === Image.Ready ? face.source.toString() : "")
                    Image { id: acct; visible: false; asynchronous: true; source: surf.ctl.userName !== "" ? "file:///var/lib/AccountsService/icons/" + surf.ctl.userName : "" }
                    Image { id: face; visible: false; asynchronous: true; source: "file://" + (Quickshell.env("HOME") || "/nonexistent") + "/.face" }
                    Rectangle {
                        id: disc
                        anchors.fill: parent
                        radius: width / 2
                        gradient: Gradient {
                            GradientStop { position: 0; color: Qt.lighter(Theme.accent, 1.18) }
                            GradientStop { position: 1; color: Qt.darker(Theme.accent, 1.25) }
                        }
                        border.width: Math.max(2, avatar.width / 30)
                        border.color: Theme.accent
                    }
                    Txt {
                        anchors.centerIn: parent
                        visible: !pic.ready
                        text: L.initials(surf.ctl.displayName)
                        color: Theme.accentText
                        font.pointSize: Math.max(6, avatar.width * 0.30)
                        font.weight: Font.DemiBold
                    }
                    // Cropped to the circle with a Canvas (no shader
                    // effects, so it also draws with the software renderer).
                    Canvas {
                        id: pic
                        anchors.fill: parent
                        anchors.margins: disc.border.width
                        property bool ready: false
                        readonly property string url: avatar.picture
                        onUrlChanged: { ready = false; if (url !== "") loadImage(url); requestPaint(); }
                        onImageLoaded: { ready = isImageLoaded(url); requestPaint(); }
                        onPaint: {
                            const c = getContext("2d");
                            c.reset();
                            if (!ready) return;
                            c.save();
                            c.beginPath();
                            c.arc(width / 2, height / 2, Math.min(width, height) / 2, 0, Math.PI * 2);
                            c.closePath();
                            c.clip();
                            c.drawImage(url, 0, 0, width, height);
                            c.restore();
                        }
                    }
                }
                Txt {
                    width: parent.width
                    horizontalAlignment: Text.AlignHCenter
                    font.pointSize: surf.title
                    font.weight: Font.DemiBold
                    text: surf.ctl.displayName
                    Accessible.role: Accessible.Heading
                    Accessible.name: text
                }
                Txt {
                    width: parent.width
                    horizontalAlignment: Text.AlignHCenter
                    font.pointSize: surf.small
                    color: Theme.textMuted
                    visible: surf.ctl.realName !== "" && surf.ctl.realName !== surf.ctl.userName
                    text: surf.ctl.userName
                }
                Item { width: 1; height: Theme.s1 }

                LockField {
                    id: field
                    ctl: surf.ctl
                    width: parent.width
                    fontSize: surf.large
                    focus: true
                }

                // One line for what the person should know: what went
                // wrong, Caps Lock, a message from PAM. Its height is kept
                // so the card does not jump.
                Item {
                    width: parent.width
                    height: Math.max(statusText.implicitHeight, surf.base * 1.6)
                    Row {
                        anchors.horizontalCenter: parent.horizontalCenter
                        spacing: Theme.s2
                        visible: statusText.text !== ""
                        Icon {
                            name: statusText.showCaps ? "keyboard" : (statusText.isError ? "warning" : "info")
                            color: statusText.color
                            size: surf.base * 1.35
                            anchors.verticalCenter: parent.verticalCenter
                        }
                        Txt {
                            id: statusText
                            objectName: "e2e:lock-status"
                            readonly property string errorText: {
                                if (surf.ctl.errorRaw !== "") return surf.ctl.errorRaw;
                                switch (surf.ctl.errorId) {
                                case "wrong": return Tr.t("That password did not work. Try again.");
                                case "wrong-hint": return surf.ctl.layoutLabel !== ""
                                    ? Tr.t("That password did not work. Check Caps Lock and the keyboard layout (%1).").arg(surf.ctl.layoutLabel)
                                    : Tr.t("That password did not work. Try again.");
                                case "maxtries": return Tr.t("Too many attempts. Wait a few minutes, then try again.");
                                case "error": return Tr.t("The password could not be checked. Try again.");
                                }
                                return "";
                            }
                            readonly property bool isError: errorText !== ""
                            // Caps Lock first: it is what to fix before the next try.
                            readonly property bool showCaps: surf.ctl.capsOn && !surf.ctl.busy
                            width: Math.min(implicitWidth, body.width - Theme.s6 * 2)
                            wrapMode: Text.Wrap
                            elide: Text.ElideNone
                            font.pointSize: surf.small
                            color: showCaps ? Theme.warning : (isError ? Theme.danger : Theme.textMuted)
                            text: showCaps ? Tr.t("Caps Lock is on") : (isError ? errorText : surf.ctl.info)
                            Accessible.role: Accessible.StaticText
                            Accessible.name: text
                            Accessible.description: isError && !showCaps ? "alert" : ""
                        }
                    }
                }
            }
        }
    }
}
