import QtQuick
import Quickshell
import Quickshell.Wayland

// The assistant's local model, on demand: when a skill needed it (a
// summary of a mailbox or a page) and none is on the computer, this card
// under the panel offers it (what, how big, from where; Download or Not
// now), then shows the download until it is ready (a notification says
// so) or why it stopped. Nothing is downloaded before Download; Not now
// holds for the session (Settings, Voice and assistant, still offers it).
PanelWindow {
    id: mc
    readonly property var st: Bus.models || ({ offers: [], jobs: [] })
    readonly property var offer: (st.offers || []).find(o => o.kind === "llm") || null
    readonly property var job: offer ? null : ((st.jobs || []).find(j => j.kind === "llm" && j.purpose === "skill" &&
        j.state !== "done" && j.state !== "cancelled") || null)
    // Below the voice card when both show.
    readonly property bool voiceCard: Bus.voice && (Bus.voice.state === "offer" || Bus.voice.state === "download")
    visible: offer !== null || job !== null
    anchors { top: true; left: true; right: true }
    implicitHeight: card.y + card.height + Theme.s4
    color: "transparent"
    exclusionMode: ExclusionMode.Ignore
    WlrLayershell.layer: WlrLayer.Overlay
    WlrLayershell.namespace: "basalt-models"
    WlrLayershell.keyboardFocus: WlrKeyboardFocus.OnDemand
    mask: Region { item: card }

    Surface {
        id: card
        anchors.horizontalCenter: parent.horizontalCenter
        y: Theme.panelHeight + Theme.s4 + (mc.voiceCard ? Theme.fontSize * 9 : 0)
        width: Math.min(660, mc.width - Theme.s6 * 2)
        height: row.implicitHeight + Theme.s3 * 2
        Row {
            id: row
            anchors.left: parent.left
            anchors.right: parent.right
            anchors.verticalCenter: parent.verticalCenter
            anchors.margins: Theme.s4
            spacing: Theme.s3
            Rectangle {
                width: Theme.fontSize * 2.6; height: width; radius: width / 2
                anchors.top: parent.top
                color: Theme.accentSoft
                Icon { anchors.centerIn: parent; name: mc.offer ? "spark" : "download"; size: Theme.fontSize * 1.5; color: Theme.accent }
            }
            DownloadView {
                width: row.width - Theme.fontSize * 2.6 - row.spacing
                offer: mc.offer
                job: mc.job
            }
        }
    }
}
