import QtQuick
import "logic.js" as L

// The secret (or visible) answer to PAM's question, with a button to show
// what was typed and the button that sends it. The text is cleared as
// soon as it is sent.
Rectangle {
    id: f
    property string placeholder: I18n.t("Password")
    property bool secret: true
    property bool busy: false
    property bool reveal: false
    property alias input: input
    signal submitted(string text)

    implicitHeight: Math.max(G.fontLarge * 2.9, 44)
    radius: G.radiusMd
    color: G.highContrast ? "#000000" : G.alpha(G.bg, 0.72)
    border.width: input.activeFocus ? 2 : G.borderWidth
    border.color: input.activeFocus ? G.accent : G.border
    Behavior on border.color { ColorAnimation { duration: G.fast } }

    function focusInput() { input.forceActiveFocus(); }
    function clear() { input.text = ""; reveal = false; }
    function send() {
        if (f.busy) return;
        const t = input.text;
        input.text = "";
        f.submitted(t);
    }

    GIcon {
        id: lockIcon
        name: f.secret ? "lock" : "user"
        size: G.fontLarge * 1.35
        color: G.textMuted
        anchors.left: parent.left
        anchors.leftMargin: G.s3
        anchors.verticalCenter: parent.verticalCenter
    }
    TextInput {
        id: input
        objectName: "e2e:password"
        anchors.left: lockIcon.right
        anchors.leftMargin: G.s3
        anchors.right: eye.left
        anchors.rightMargin: G.s2
        anchors.verticalCenter: parent.verticalCenter
        color: G.text
        selectionColor: G.accent
        selectedTextColor: G.accentText
        font.family: G.fontFamily
        font.pointSize: G.fontLarge
        font.letterSpacing: echoMode === TextInput.Password ? 2 : 0
        echoMode: f.secret && !f.reveal ? TextInput.Password : TextInput.Normal
        passwordCharacter: "●"
        passwordMaskDelay: 0
        inputMethodHints: f.secret ? (Qt.ImhSensitiveData | Qt.ImhNoPredictiveText | Qt.ImhHiddenText | Qt.ImhNoAutoUppercase)
                                   : (Qt.ImhNoPredictiveText | Qt.ImhNoAutoUppercase)
        clip: true
        readOnly: f.busy
        activeFocusOnTab: true
        Accessible.role: Accessible.EditableText
        Accessible.passwordEdit: f.secret
        Accessible.name: f.placeholder
        onAccepted: f.send()
        Keys.onPressed: event => {
            if (event.key === Qt.Key_CapsLock) Login.capsGuess = !Login.capsGuess;
            const g = L.capsFromKey(event.text, (event.modifiers & Qt.ShiftModifier) !== 0);
            if (g !== null) Login.capsGuess = g;
            Sys.checkCaps();
        }
        GTxt {
            anchors.fill: parent
            text: f.placeholder
            color: G.textMuted
            font.pointSize: G.fontLarge
            visible: input.text === "" && !input.preeditText
        }
    }
    GBtn {
        id: eye
        visible: f.secret
        icon: f.reveal ? "eye-off" : "eye"
        label: f.reveal ? I18n.t("Hide password") : I18n.t("Show password")
        e2e: "reveal"
        round: true
        iconSize: G.fontLarge * 1.25
        anchors.right: go.left
        anchors.rightMargin: G.s1
        anchors.verticalCenter: parent.verticalCenter
        onClicked: { f.reveal = !f.reveal; input.forceActiveFocus(); }
    }
    GBtn {
        id: go
        variant: "primary"
        round: true
        icon: f.busy ? "" : "arrow"
        label: I18n.t("Log in")
        e2e: "login"
        iconSize: G.fontLarge * 1.25
        anchors.right: parent.right
        anchors.rightMargin: G.s1 * 1.5
        anchors.verticalCenter: parent.verticalCenter
        height: f.height - G.s1 * 3
        width: height
        onClicked: f.send()
        GIcon {
            anchors.centerIn: parent
            visible: f.busy
            name: "spinner"
            size: G.fontLarge * 1.25
            color: G.accentText
            RotationAnimator on rotation {
                running: f.busy
                from: 0; to: 360; duration: 900; loops: Animation.Infinite
            }
        }
    }
}
