import QtQuick

// The exact preview of an acting skill's proposal, before the person
// confirms: an e-mail reply (recipient fixed, subject and text editable,
// what is sent is what is shown) or a list of file moves. Warnings about
// the content come first.
Column {
    id: ap
    property var proposal: null
    property var warnings: []
    readonly property var pv: proposal && proposal.previews && proposal.previews.length > 0 ? proposal.previews[0] : null
    readonly property bool isMail: pv !== null && pv.kind === "mail"
    readonly property bool isFiles: pv !== null && pv.kind === "files"
    // The person's edits (sent with the confirmation).
    function edits() {
        if (!isMail) return {};
        return { subject: subjectField.text, body: bodyEdit.text };
    }
    spacing: Theme.s3

    onPvChanged: if (isMail) { subjectField.text = pv.subject || ""; bodyEdit.text = pv.body || ""; }

    Rectangle {
        visible: ap.warnings && ap.warnings.length > 0
        width: parent.width
        height: warnCol.implicitHeight + Theme.s3 * 2
        radius: Theme.radiusMd
        color: Theme.alpha(Theme.warning, 0.12)
        border.width: 1; border.color: Theme.warning
        Column {
            id: warnCol
            anchors.fill: parent; anchors.margins: Theme.s3
            spacing: Theme.s1
            Repeater {
                model: ap.warnings || []
                delegate: Row {
                    required property var modelData
                    spacing: Theme.s2
                    Icon { name: "warning"; size: Theme.fontSize * 1.4; color: Theme.warning }
                    Txt { text: modelData; width: warnCol.width - Theme.s6; wrapMode: Text.Wrap; elide: Text.ElideNone }
                }
            }
        }
    }

    // An e-mail reply.
    Column {
        visible: ap.isMail
        width: parent.width
        spacing: Theme.s2
        Row {
            spacing: Theme.s2
            Txt { text: qsTr("From"); width: Theme.fontSize * 6; color: Theme.textMuted; anchors.verticalCenter: parent.verticalCenter }
            Txt { text: ap.pv ? ((ap.pv.from_name ? ap.pv.from_name + " " : "") + "<" + ap.pv.from + ">") : ""; anchors.verticalCenter: parent.verticalCenter }
        }
        Row {
            spacing: Theme.s2
            Txt { text: qsTr("To"); width: Theme.fontSize * 6; color: Theme.textMuted; anchors.verticalCenter: parent.verticalCenter }
            Txt { text: ap.pv ? ((ap.pv.to_name ? ap.pv.to_name + " " : "") + "<" + ap.pv.to + ">") : ""; font.weight: Font.DemiBold; anchors.verticalCenter: parent.verticalCenter }
            Txt { text: qsTr("(the sender of the message; not editable)"); role: "small"; color: Theme.textMuted; anchors.verticalCenter: parent.verticalCenter }
        }
        Row {
            spacing: Theme.s2
            width: parent.width
            Txt { text: qsTr("Subject"); width: Theme.fontSize * 6; color: Theme.textMuted; anchors.verticalCenter: parent.verticalCenter }
            Field { id: subjectField; width: parent.width - Theme.fontSize * 6 - Theme.s2 }
        }
        Rectangle {
            width: parent.width
            height: Math.max(Theme.fontSize * 10, Math.min(bodyEdit.contentHeight + Theme.s3 * 2, Theme.fontSize * 18))
            radius: Theme.radiusMd
            color: Theme.bg
            border.width: 1
            border.color: bodyEdit.activeFocus ? Theme.accent : Theme.border
            Flickable {
                id: bodyFlick
                anchors.fill: parent
                anchors.margins: Theme.s3
                contentHeight: bodyEdit.contentHeight
                clip: true
                TextEdit {
                    id: bodyEdit
                    width: bodyFlick.width
                    wrapMode: TextEdit.Wrap
                    textFormat: TextEdit.PlainText
                    color: Theme.text
                    selectionColor: Theme.accent
                    selectedTextColor: Theme.accentText
                    font.family: Theme.fontFamily
                    font.pointSize: Theme.fontSize
                    selectByMouse: true
                }
            }
        }
        Txt {
            width: parent.width
            wrapMode: Text.Wrap
            role: "small"
            color: Theme.textMuted
            text: qsTr("Plain text, no attachment. You can change the subject and the text; exactly this is sent, and only when you press Send.")
        }
    }

    // File moves: every file, from and to.
    Column {
        visible: ap.isFiles
        width: parent.width
        spacing: Theme.s1
        Repeater {
            model: ap.isFiles ? (ap.pv.moves || []) : []
            delegate: Column {
                required property var modelData
                width: ap.width
                Txt { text: modelData.from; width: parent.width; role: "mono"; elide: Text.ElideMiddle }
                Row {
                    spacing: Theme.s2
                    Icon { name: "arrow"; size: Theme.fontSize; color: Theme.accent; anchors.verticalCenter: parent.verticalCenter }
                    Txt { text: modelData.to; width: ap.width - Theme.s6; role: "mono"; font.weight: Font.DemiBold; elide: Text.ElideMiddle }
                }
            }
        }
        Txt {
            visible: ap.isFiles && ap.pv.create && ap.pv.create.length > 0
            text: ap.isFiles && ap.pv.create ? qsTr("New folder: %1").arg(ap.pv.create.join(", ")) : ""
            role: "small"; color: Theme.textMuted
        }
        Txt {
            visible: ap.isFiles
            width: parent.width
            wrapMode: Text.Wrap
            role: "small"
            color: Theme.textMuted
            text: qsTr("Nothing is deleted or replaced. Say \"undo\" afterwards to put the files back.")
        }
    }
}
