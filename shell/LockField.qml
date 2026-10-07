import QtQuick

// The lock screen's password field: the answer to PAM's question, with a
// button to show what was typed and the button that sends it. The focus
// ring shows whenever it has the keyboard (the lock screen is used with
// the keyboard first). Return sends (only once something was typed),
// Escape clears. The text is cleared as soon as it is sent.
Rectangle {
    id: f
    property var ctl
    property real fontSize: Theme.fontLarge
    property bool reveal: false
    readonly property bool secret: ctl.promptKind !== "visible"
    readonly property string placeholder: ctl.busy ? (ctl.phase === "unlocking" ? Tr.t("Unlocking") : Tr.t("Checking"))
        : (ctl.promptKind === "password" ? Tr.t("Type your password to unlock") : ctl.promptText.replace(/:\s*$/, ""))
    property alias input: input
    objectName: "e2e:lock-field"

    implicitHeight: Math.max(fontSize * 2.9, 44)
    radius: Theme.radiusMd
    color: Theme.alpha(Theme.bg, 0.72)
    border.width: 1
    border.color: Theme.border

    function focusInput() { input.forceActiveFocus(Qt.TabFocusReason); }
    function clear() { f.ctl.clearTyped(); reveal = false; }
    function send() { reveal = false; f.ctl.submitTyped(); }

    Icon {
        id: lockIcon
        name: "lock"
        size: f.fontSize * 1.35
        color: Theme.textMuted
        anchors.left: parent.left
        anchors.leftMargin: Theme.s3
        anchors.verticalCenter: parent.verticalCenter
    }
    TextInput {
        id: input
        objectName: "e2e:lock-password"
        anchors.left: lockIcon.right
        anchors.leftMargin: Theme.s3
        anchors.right: eye.visible ? eye.left : go.left
        anchors.rightMargin: Theme.s2
        anchors.verticalCenter: parent.verticalCenter
        color: Theme.text
        selectionColor: Theme.accent
        selectedTextColor: Theme.accentText
        font.family: Theme.fontFamily
        font.pointSize: f.fontSize
        font.letterSpacing: echoMode === TextInput.Password ? 2 : 0
        echoMode: f.secret && !f.reveal ? TextInput.Password : TextInput.Normal
        passwordCharacter: "●"
        passwordMaskDelay: 0
        inputMethodHints: Qt.ImhSensitiveData | Qt.ImhNoPredictiveText | Qt.ImhHiddenText | Qt.ImhNoAutoUppercase
        clip: true
        // Shared with the other outputs' surfaces (Lock.typed).
        text: f.ctl.typed
        onTextEdited: f.ctl.typed = text
        readOnly: f.ctl.busy
        activeFocusOnTab: true
        Accessible.role: Accessible.EditableText
        Accessible.passwordEdit: f.secret
        Accessible.name: f.placeholder
        onActiveFocusChanged: Ui.noteFocused("lock-password", activeFocus, f)
        Keys.onPressed: event => {
            if (event.key === Qt.Key_Return || event.key === Qt.Key_Enter) {
                // A stray Return on an empty field does nothing.
                f.send();
                event.accepted = true;
                return;
            }
            if (event.key === Qt.Key_Escape) {
                f.clear();
                event.accepted = true;
                return;
            }
            f.ctl.noteKey(event);
            event.accepted = false;
        }
        // The hint: always whole (a smaller size before it would be cut).
        Txt {
            anchors.fill: parent
            text: f.placeholder
            color: Theme.textMuted
            font.pointSize: f.fontSize
            fontSizeMode: Text.HorizontalFit
            minimumPointSize: Theme.fontSmall
            visible: input.text === "" && !input.preeditText
        }
    }
    Btn {
        id: eye
        visible: f.secret && f.ctl.typed !== ""
        icon: f.reveal ? "eye-off" : "eye"
        accessibleName: f.reveal ? Tr.t("Hide password") : Tr.t("Show password")
        e2e: "lock-reveal"
        radius: height / 2
        width: height
        iconSize: f.fontSize * 1.25
        anchors.right: go.left
        anchors.rightMargin: Theme.s1
        anchors.verticalCenter: parent.verticalCenter
        onClicked: { f.reveal = !f.reveal; f.focusInput(); }
    }
    Btn {
        id: go
        variant: "primary"
        icon: f.ctl.busy ? "" : "arrow"
        accessibleName: Tr.t("Unlock")
        e2e: "lock-unlock"
        iconSize: f.fontSize * 1.25
        anchors.right: parent.right
        anchors.rightMargin: Theme.s1 * 1.5
        anchors.verticalCenter: parent.verticalCenter
        height: f.height - Theme.s1 * 3
        width: height
        radius: height / 2
        onClicked: { f.send(); f.focusInput(); }
        Icon {
            anchors.centerIn: parent
            visible: f.ctl.busy
            name: "spinner"
            size: f.fontSize * 1.25
            color: Theme.accentText
            RotationAnimator on rotation {
                running: f.ctl.busy
                from: 0; to: 360; duration: 900; loops: Animation.Infinite
            }
        }
    }
    FocusRing { target: f; always: true; shown: input.activeFocus }
}
