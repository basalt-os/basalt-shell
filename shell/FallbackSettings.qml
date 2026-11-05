import QtQuick
import QtQuick.Layouts

// Settings, About: GNOME as the fallback session. Shown when GNOME is
// installed (basalt-desktop-gnome-fallback). GNOME cannot lock the screen
// when the Basalt login screen (greetd) starts the sessions, so while the
// person uses GNOME, GNOME's own login screen (GDM) should start instead.
// The switch is a system change: the system assistant stores the
// desktop.fallback proposal, it is shown here, and the person applies it
// with an administrator's password (or the approval gate). It takes
// effect at the next restart and goes back the same way (here, or
// `basalt fallback basalt` in a terminal from GNOME).
ColumnLayout {
    id: fb
    spacing: Theme.s2

    property var info: null
    property var proposal: null
    property string target: ""
    property bool busy: false
    property string status: ""
    property bool statusError: false

    readonly property bool gdmActive: info !== null && info.display_manager === "gdm"

    function load() {
        Bus.call("fallback.state", {}, (ok, res) => { if (ok) fb.info = res; });
    }
    function propose(t) {
        fb.busy = true; fb.status = ""; fb.statusError = false; fb.target = t;
        Bus.call("fallback.propose", { target: t }, (ok, res) => {
            fb.busy = false;
            if (ok) fb.proposal = res;
            else { fb.status = String(res); fb.statusError = true; }
        });
    }
    function decide(apply) {
        if (!fb.proposal) return;
        if (!apply) { fb.proposal = null; return; }
        fb.busy = true; fb.statusError = false;
        fb.status = Tr.t("Waiting for your password.");
        Bus.assistantApply(fb.proposal.id, fb.proposal.code, (ok, res) => {
            fb.busy = false;
            const good = ok && res.ok;
            fb.statusError = !good;
            fb.status = good ? (fb.target === "gnome"
                                ? Tr.t("Done. GNOME's login screen starts from the next restart. To come back, use this page or run basalt fallback basalt.")
                                : Tr.t("Done. The Basalt login screen starts from the next restart."))
                             : Tr.t("Nothing was changed, or not everything worked: %1").arg(ok && res.output ? res.output : String(res));
            fb.proposal = null;
            Bus.refreshAssistant();
            fb.load();
        });
    }
    // Read again each time About is shown (GNOME may have been installed
    // since); the section itself is hidden until GNOME is there.
    Connections {
        target: Ui
        function onSettingsPageChanged() { if (Ui.settingsPage === "about") fb.load(); }
        function onSettingsChanged() { if (Ui.settings && Ui.settingsPage === "about") fb.load(); }
    }
    Component.onCompleted: load()

    visible: info !== null && info.gnome

    Txt { text: Tr.t("GNOME as a fallback"); role: "large"; font.weight: Font.DemiBold; topPadding: Theme.s2 }
    Txt {
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
        text: Tr.t("GNOME is installed next to Basalt. It cannot lock the screen when the Basalt login screen starts it: while you use GNOME, let GNOME's login screen (GDM) start instead. The change takes effect at the next restart, and you can switch back the same way.")
    }
    Txt {
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
        text: fb.gdmActive ? Tr.t("Login screen now: GNOME's (GDM).") : Tr.t("Login screen now: Basalt's.")
    }
    Txt {
        visible: fb.info !== null && !fb.info.gdm
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.warning
        text: Tr.t("GNOME's login screen (GDM) is not installed.")
    }
    Btn {
        visible: fb.proposal === null
        enabled: !fb.busy && (fb.gdmActive || (fb.info !== null && fb.info.gdm))
        icon: "shield"; variant: "outline"
        e2e: fb.gdmActive ? "fallback-basalt" : "fallback-gnome"
        text: fb.gdmActive ? Tr.t("Use the Basalt login screen again") : Tr.t("Use GNOME's login screen")
        onClicked: fb.propose(fb.gdmActive ? "basalt" : "gnome")
    }

    // The stored proposal, confirmed here.
    Rectangle {
        visible: fb.proposal !== null
        Layout.fillWidth: true
        implicitHeight: confirmCol.implicitHeight + Theme.s3 * 2
        radius: Theme.radiusMd; color: Theme.bg; border.width: 1; border.color: Theme.border
        ColumnLayout {
            id: confirmCol
            anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top
            anchors.margins: Theme.s3
            spacing: Theme.s2
            Txt {
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; font.weight: Font.DemiBold
                text: fb.target === "gnome" ? Tr.t("Use GNOME's login screen (GDM) from the next restart?")
                                            : Tr.t("Use the Basalt login screen from the next restart?")
            }
            Txt {
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "small"; color: Theme.textMuted
                text: Tr.t("This changes the whole computer, so it needs an administrator's password. Nothing changes in the sessions running now.")
            }
            Txt {
                visible: fb.proposal !== null && !!fb.proposal.report
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "mono"; color: Theme.textMuted
                text: fb.proposal ? fb.proposal.report : ""
                maximumLineCount: 12
            }
            Flow {
                Layout.fillWidth: true
                layoutDirection: Qt.RightToLeft
                spacing: Theme.s2
                Btn { text: Tr.t("Switch at the next restart"); icon: "check"; variant: "primary"; e2e: "fallback-confirm"; enabled: !fb.busy; onClicked: fb.decide(true) }
                Btn { text: Tr.t("Not now"); variant: "outline"; e2e: "fallback-cancel"; enabled: !fb.busy; onClicked: fb.decide(false) }
            }
        }
    }

    Txt {
        visible: fb.status !== ""
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
        color: fb.statusError ? Theme.danger : Theme.textMuted
        text: fb.status
    }
}
