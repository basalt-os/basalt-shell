import QtQuick
import Quickshell
import Quickshell.Wayland
import Quickshell.Services.Polkit

// The session's polkit authentication agent, in the shell's theme. Used
// for administrator prompts, including the system assistant's Apply.
// Keyboard: the password field has the focus (the person types there
// first); Return in it submits only when something was typed, so a stray
// Return never authenticates. Tab goes on to Cancel, then Authenticate;
// Escape cancels.
Scope {
    id: root
    PolkitAgent { id: agent }
    Binding { target: Ui; property: "polkitActive"; value: agent.isActive && agent.flow !== null }

    PanelWindow {
        visible: agent.isActive && agent.flow !== null
        anchors { top: true; bottom: true; left: true; right: true }
        color: Theme.scrim
        exclusionMode: ExclusionMode.Ignore
        WlrLayershell.layer: WlrLayer.Overlay
        WlrLayershell.namespace: "basalt-polkit"
        WlrLayershell.keyboardFocus: visible ? WlrKeyboardFocus.Exclusive : WlrKeyboardFocus.None
        onVisibleChanged: if (visible) pw.focusInput()

        Surface {
            width: Math.min(520, parent.width - 48)
            height: col.implicitHeight + Theme.s6 * 2
            anchors.centerIn: parent
            Accessible.role: Accessible.Dialog
            Accessible.name: authTitle.text
            // Escape cancels, from the field or the buttons.
            Keys.onEscapePressed: if (agent.flow) agent.flow.cancelAuthenticationRequest()
            Column {
                id: col
                anchors.fill: parent
                anchors.margins: Theme.s6
                spacing: Theme.s3
                Row {
                    spacing: Theme.s3
                    Icon { name: "shield"; size: Theme.fontTitle * 1.8; color: Theme.accent }
                    Txt { id: authTitle; text: "Authentication required"; role: "title"; anchors.verticalCenter: parent.verticalCenter }
                }
                Txt { width: parent.width; wrapMode: Text.Wrap; elide: Text.ElideNone; text: agent.flow ? agent.flow.message : "" }
                Txt { width: parent.width; role: "mono"; color: Theme.textMuted; text: agent.flow ? agent.flow.actionId : "" }
                Txt {
                    width: parent.width; role: "small"; color: Theme.textMuted
                    text: agent.flow && agent.flow.selectedIdentity ? "As " + agent.flow.selectedIdentity.displayName : ""
                }
                Field {
                    id: pw
                    width: parent.width
                    placeholder: agent.flow ? (agent.flow.inputPrompt || "Password") : "Password"
                    e2e: "polkit-password"
                    input.echoMode: agent.flow && agent.flow.responseVisible ? TextInput.Normal : TextInput.Password
                    onAccepted: if (agent.flow && text !== "") { agent.flow.submit(text); text = ""; }
                }
                Txt {
                    visible: text !== ""
                    width: parent.width; wrapMode: Text.Wrap; elide: Text.ElideNone
                    color: agent.flow && agent.flow.supplementaryIsError ? Theme.danger : Theme.textMuted
                    text: agent.flow ? agent.flow.supplementaryMessage : ""
                }
                Row {
                    anchors.right: parent.right
                    spacing: Theme.s2
                    Btn { text: "Cancel"; variant: "outline"; e2e: "polkit-cancel"; onClicked: if (agent.flow) agent.flow.cancelAuthenticationRequest() }
                    Btn { text: "Authenticate"; variant: "primary"; e2e: "polkit-authenticate"; onClicked: if (agent.flow) { agent.flow.submit(pw.text); pw.text = ""; } }
                }
            }
        }
    }
}
