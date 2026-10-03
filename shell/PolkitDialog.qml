import QtQuick
import Quickshell
import Quickshell.Wayland
import Quickshell.Services.Polkit

// The session's polkit authentication agent, in the shell's theme. Used
// for administrator prompts, including the system assistant's Apply.
Scope {
    id: root
    PolkitAgent { id: agent }

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
            Column {
                id: col
                anchors.fill: parent
                anchors.margins: Theme.s6
                spacing: Theme.s3
                Row {
                    spacing: Theme.s3
                    Icon { name: "shield"; size: Theme.fontTitle * 1.8; color: Theme.accent }
                    Txt { text: "Authentication required"; role: "title"; anchors.verticalCenter: parent.verticalCenter }
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
                    input.echoMode: agent.flow && agent.flow.responseVisible ? TextInput.Normal : TextInput.Password
                    onAccepted: if (agent.flow) { agent.flow.submit(text); text = ""; }
                    onEscapePressed: if (agent.flow) agent.flow.cancelAuthenticationRequest()
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
                    Btn { text: "Cancel"; variant: "outline"; onClicked: if (agent.flow) agent.flow.cancelAuthenticationRequest() }
                    Btn { text: "Authenticate"; variant: "primary"; onClicked: if (agent.flow) { agent.flow.submit(pw.text); pw.text = ""; } }
                }
            }
        }
    }
}
