import QtQuick

// Shows what a proposal would do: the steps and, for theme changes, the
// token diff (color swatches or values, before and after).
Column {
    id: pv
    property var proposal: null
    property int maxDiffRows: 14
    spacing: Theme.s2

    readonly property var diff: proposal && proposal.diff ? proposal.diff : []

    function isColor(v) { return typeof v === "string" && /^#[0-9a-fA-F]{6}([0-9a-fA-F]{2})?$/.test(v); }
    function fmt(v) {
        if (v === undefined || v === null) return "-";
        if (typeof v === "number") return (Math.round(v * 100) / 100).toString();
        return "" + v;
    }
    // QML colors want #AARRGGBB; tokens use #RRGGBBAA.
    function qcolor(v) { return v.length === 9 ? "#" + v.substr(7, 2) + v.substr(1, 6) : v; }

    Repeater {
        model: pv.proposal ? pv.proposal.steps : []
        delegate: Row {
            required property var modelData
            spacing: Theme.s2
            Icon { name: "arrow"; size: Theme.fontSize * 1.3; color: Theme.accent; anchors.verticalCenter: parent.verticalCenter }
            Txt { text: modelData; width: pv.width - Theme.s6; wrapMode: Text.Wrap; elide: Text.ElideNone }
        }
    }

    Rectangle {
        visible: pv.diff.length > 0
        width: parent.width
        height: diffCol.implicitHeight + Theme.s3 * 2
        radius: Theme.radiusMd
        color: Theme.bg
        border.width: 1
        border.color: Theme.border
        Column {
            id: diffCol
            anchors.fill: parent
            anchors.margins: Theme.s3
            spacing: Theme.s1
            Txt { text: "Token changes (" + pv.diff.length + ")"; role: "small"; color: Theme.textMuted }
            Repeater {
                model: pv.diff.slice(0, pv.maxDiffRows)
                delegate: Row {
                    required property var modelData
                    spacing: Theme.s2
                    height: Theme.fontSize * 2
                    Txt { text: modelData.key; role: "mono"; width: diffCol.width * 0.38; anchors.verticalCenter: parent.verticalCenter }
                    Rectangle {
                        visible: pv.isColor(modelData.from)
                        width: Theme.fontSize * 1.4; height: width; radius: Theme.radiusSm / 2
                        color: visible ? pv.qcolor(modelData.from) : "transparent"
                        border.width: 1; border.color: Theme.border
                        anchors.verticalCenter: parent.verticalCenter
                    }
                    Txt { text: pv.fmt(modelData.from); role: "mono"; color: Theme.textMuted; width: diffCol.width * 0.2; anchors.verticalCenter: parent.verticalCenter }
                    Icon { name: "arrow"; size: Theme.fontSize; color: Theme.textMuted; anchors.verticalCenter: parent.verticalCenter }
                    Rectangle {
                        visible: pv.isColor(modelData.to)
                        width: Theme.fontSize * 1.4; height: width; radius: Theme.radiusSm / 2
                        color: visible ? pv.qcolor(modelData.to) : "transparent"
                        border.width: 1; border.color: Theme.border
                        anchors.verticalCenter: parent.verticalCenter
                    }
                    Txt { text: pv.fmt(modelData.to); role: "mono"; font.weight: Font.DemiBold; anchors.verticalCenter: parent.verticalCenter }
                }
            }
            Txt {
                visible: pv.diff.length > pv.maxDiffRows
                text: "and " + (pv.diff.length - pv.maxDiffRows) + " more"
                role: "small"; color: Theme.textMuted
            }
        }
    }
}
