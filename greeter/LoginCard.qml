import QtQuick
import QtQuick.Effects
import "logic.js" as L

// The card in the middle: who is logging in, the password, what went
// wrong, and the session to start.
Item {
    id: card
    property Item popoverHost: null
    readonly property var user: Login.user
    readonly property bool askName: Login.other && Login.otherName === ""
    property real maxWidth: 400 * G.textScale
    readonly property real cardWidth: Math.min(400 * G.textScale, maxWidth)
    implicitWidth: cardWidth
    implicitHeight: body.implicitHeight + G.s6 * 2

    function focusField() {
        if (askName) nameField.focusInput(); else pass.focusInput();
    }

    // Shake on a wrong password.
    property real shake: 0
    SequentialAnimation {
        id: shakeAnim
        NumberAnimation { target: card; property: "shake"; to: -12; duration: 50 }
        NumberAnimation { target: card; property: "shake"; to: 10; duration: 70 }
        NumberAnimation { target: card; property: "shake"; to: -6; duration: 70 }
        NumberAnimation { target: card; property: "shake"; to: 3; duration: 60 }
        NumberAnimation { target: card; property: "shake"; to: 0; duration: 50 }
    }
    Connections {
        target: Login
        function onFailed() { pass.clear(); if (G.slow > 0) shakeAnim.restart(); pass.focusInput(); }
    }
    transform: Translate { x: card.shake }

    RectangularShadow {
        anchors.fill: bg
        radius: bg.radius
        blur: 48
        spread: 0
        offset.y: 12
        color: G.alpha("#000000", G.dark ? 0.45 : 0.18)
        visible: !G.highContrast
    }
    Rectangle {
        id: bg
        anchors.fill: parent
        radius: G.radiusLg + 4
        color: G.card
        border.width: G.borderWidth
        border.color: G.alpha(G.border, G.highContrast ? 1 : 0.9)
    }

    Column {
        id: body
        anchors.left: parent.left
        anchors.right: parent.right
        anchors.top: parent.top
        anchors.margins: G.s6
        spacing: G.s3

        Avatar {
            anchors.horizontalCenter: parent.horizontalCenter
            size: 88 * G.textScale
            user: card.user
            other: Login.other
            selected: true
        }
        GTxt {
            width: parent.width
            horizontalAlignment: Text.AlignHCenter
            role: "title"
            text: Login.other ? (Login.otherName !== "" ? Login.otherName : I18n.t("Other user")) : L.displayName(card.user)
            Accessible.role: Accessible.Heading
            Accessible.name: text
        }
        GTxt {
            width: parent.width
            horizontalAlignment: Text.AlignHCenter
            role: "small"
            color: G.textMuted
            visible: !Login.other && card.user && card.user.realName !== "" && card.user.realName !== card.user.name
            text: card.user ? card.user.name : ""
        }
        Item { width: 1; height: G.s1 }

        // "Other user": the user name first.
        PasswordField {
            id: nameField
            visible: card.askName
            width: parent.width
            secret: false
            placeholder: I18n.t("User name")
            onSubmitted: t => { if (t.trim() !== "") { Login.setOtherName(t); pass.focusInput(); } }
        }
        PasswordField {
            id: pass
            visible: !card.askName
            width: parent.width
            busy: Login.busy
            secret: Login.promptKind !== "visible"
            placeholder: Login.phase === "ready" ? I18n.t("Press Enter to log in")
                         : (Login.promptKind === "password" ? I18n.t("Password") : Login.promptText.replace(/:\s*$/, ""))
            onSubmitted: t => Login.submit(t)
        }

        // One line for what the person should know: an error, Caps Lock,
        // a message from PAM. Its height is kept so the card does not jump.
        Item {
            width: parent.width
            height: Math.max(statusText.implicitHeight, G.fontSize * 1.6)
            Row {
                anchors.horizontalCenter: parent.horizontalCenter
                spacing: G.s2
                visible: statusText.text !== ""
                GIcon {
                    name: Login.error !== "" ? "warning" : (Login.capsOn ? "keyboard" : "info")
                    color: statusText.color
                    size: G.fontSize * 1.35
                    anchors.verticalCenter: parent.verticalCenter
                }
                GTxt {
                    id: statusText
                    objectName: "e2e:status"
                    width: Math.min(implicitWidth, body.width - G.s8)
                    wrapMode: Text.Wrap
                    elide: Text.ElideNone
                    role: "small"
                    color: Login.error !== "" ? G.danger : (Login.capsOn ? G.warning : G.textMuted)
                    text: Login.error !== "" ? Login.error
                          : (Login.capsOn && !card.askName ? I18n.t("Caps Lock is on")
                          : (Login.phase === "launching" ? I18n.t("Starting your session")
                          : (!Login.available ? I18n.t("The login service is not available.") : Login.info)))
                    Accessible.role: Accessible.StaticText
                    Accessible.name: text
                    // Screen readers announce changes of an alert.
                    Accessible.description: Login.error !== "" ? "alert" : ""
                }
            }
        }

        // The session to start, when there is a choice.
        Item {
            width: parent.width
            height: sessionBtn.implicitHeight
            visible: Sys.sessions.length > 1 && !card.askName
            GBtn {
                id: sessionBtn
                anchors.horizontalCenter: parent.horizontalCenter
                icon: "session"
                variant: "ghost"
                e2e: "session"
                iconSize: G.fontSize * 1.3
                text: Login.session ? (Login.session.known !== "" ? I18n.t(Login.session.known) : Login.session.label) + "  ▾" : ""
                label: I18n.t("Session: %1").arg(Login.session ? (Login.session.known !== "" ? I18n.t(Login.session.known) : Login.session.label) : "")
                onClicked: sessionMenu.openAt(sessionBtn)
            }
        }
    }

    // The menu lives on the screen (popoverHost) so it is not clipped by
    // the card.
    Popover {
        id: sessionMenu
        parent: card.popoverHost
        title: I18n.t("Session")
        e2e: "session-menu"
        items: Sys.sessions.map(s => ({ id: s.file, icon: "session", e2e: "session:" + s.file,
                                        text: s.known !== "" ? I18n.t(s.known) : s.label,
                                        checked: Login.session !== null && Login.session.file === s.file }))
        onPicked: id => {
            Login.session = Sys.sessions.find(s => s.file === id) || Login.session;
            close();
            pass.focusInput();
        }
    }
}
