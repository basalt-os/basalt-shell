import QtQuick
import QtQuick.Layouts
import Quickshell
import "updates.js" as U

// Settings, Updates and channels: check for updates and install them, see
// what needs a restart, undo the last update, and choose where software
// comes from (Basalt's channels and other software sources). Everything
// comes from the system assistant (basalt updates, basalt channels) and
// every change is one of its proposals: the page stores it, shows it in
// plain words with the exact commands under Details, and applies it the
// way every proposal is applied (pkexec and the person's password, or
// the approval gate). The page never runs dnf and never writes a
// repository file.
ColumnLayout {
    id: up
    spacing: Theme.s4

    property var upd: null             // basalt updates --json
    property var chan: null            // basalt channels --json
    property var drv: null             // drivers.state, for the hardware the driver channels are for
    property bool loading: false
    property bool checking: false
    property bool busy: false
    property bool applying: false
    property string status: ""
    property bool statusError: false
    property string result: ""
    property bool showResult: false
    property var proposal: null        // {id, code, report, title, evidence, actions}
    property string pkind: ""          // install, security, rollback, enable, disable, add, remove
    property string ptarget: ""        // the channel or source the proposal is about
    property bool showDetails: false
    property var openGroups: ({})
    property bool showAllChannels: false
    property bool adding: false        // the "Add a source" panel is open
    property bool restartAfter: false

    readonly property bool comingSoon: (upd !== null && upd.coming_soon === true) || (chan !== null && chan.coming_soon === true)
    readonly property var updates: upd && upd.updates ? upd.updates : []
    readonly property var counts: upd && upd.counts ? upd.counts : ({})
    readonly property var running: upd ? upd.running || null : null
    readonly property bool restartNeeded: (upd !== null && upd.restart && upd.restart.needed) || restartAfter
    readonly property var groups: ["security", "basalt", "apps", "system"]
    // The set replaces core parts of the system: it installs while the
    // computer restarts, before the desktop starts (dnf offline).
    readonly property bool offlineAll: upd !== null && upd.offline === true
    readonly property bool offlineSecurity: upd !== null && upd.offline_security === true
    // An offline update waits for the restart into it.
    readonly property bool staged: upd !== null && !!upd.scheduled
    readonly property bool pOffline: (pkind === "install" && offlineAll) || (pkind === "security" && offlineSecurity)
    // The driver channels matter on hardware the NVIDIA driver supports.
    readonly property bool driverHardware: {
        if (!drv || !drv.recommendation) return false;
        const a = drv.recommendation.action;
        return a === "install" || a === "installed" || a === "fallback";
    }

    function load() {
        if (loading) return;
        loading = true;
        Bus.call("updates.state", {}, (ok, res) => {
            loading = false;
            if (ok) upd = res; else { status = String(res); statusError = true; }
        });
        Bus.call("channels.state", {}, (ok, res) => { if (ok) chan = res; });
        Bus.call("drivers.state", {}, (ok, res) => { if (ok) drv = res; });
    }
    function check() {
        checking = true; status = ""; statusError = false;
        Bus.call("updates.check", {}, (ok, res) => {
            checking = false;
            if (ok) upd = res; else { status = Tr.t("Could not check for updates: %1").arg(String(res)); statusError = true; }
        });
    }
    // Store a proposal, then show it for confirmation.
    function propose(op, args, kind, target) {
        busy = true; status = ""; statusError = false; showDetails = false; showResult = false;
        Bus.call(op, args, (ok, res) => {
            busy = false;
            if (ok) { proposal = res; pkind = kind; ptarget = target || ""; }
            else {
                // One line a person reads; the whole error stays under
                // "Show the assistant's report".
                result = String(res);
                status = failureText(result, U.firstLine(result, 160));
                statusError = true;
            }
        });
    }
    // failureText: the one line for a change that did not work (see
    // updates.js failureKind); fallback when nothing better is known.
    function failureText(text, fallback) {
        switch (U.failureKind(text)) {
        case "unpublished": return Tr.t("Not available yet: a package this needs is not published.");
        case "network": return Tr.t("The software sources could not be reached. Check the internet connection and try again.");
        case "signature": return Tr.t("A package or a source failed its signature check, so it was not used.");
        case "space": return Tr.t("There is not enough free disk space for this change.");
        case "refused": return Tr.t("Nothing was changed: the approval was not given.");
        }
        return fallback;
    }
    function decide(apply) {
        if (!proposal) return;
        if (!apply) {
            // Not now changes nothing and asks for nothing: the stored
            // proposal stays pending (the assistant reuses it by its key
            // the next time) and is never applied without a confirmation.
            proposal = null; status = "";
            return;
        }
        busy = true; applying = true; statusError = false;
        status = Tr.t("Waiting for your password.");
        const kind = pkind, target = ptarget, offline = offlineAll, offlineSec = offlineSecurity;
        Bus.assistantApply(proposal.id, proposal.code, (ok, res) => {
            busy = false; applying = false;
            const good = ok && res.ok;
            result = ok && res.output ? res.output : (ok ? "" : String(res));
            statusError = !good;
            if (!good) status = failureText(ok ? (res.output || "") : String(res), Tr.t("Nothing was changed, or not everything worked. The assistant's report is under Details."));
            else if ((kind === "install" && offline) || (kind === "security" && offlineSec)) {
                // Downloaded and staged: the restart waits for the power
                // menu's countdown (Cancel keeps it staged).
                status = Tr.t("The updates are downloaded and ready. They install when the computer restarts.");
                Ui.restartForUpdate();
            }
            else if (kind === "install" || kind === "security") status = Tr.t("Updates installed. A snapshot from before them is kept, so you can undo them.");
            else if (kind === "rollback") { status = Tr.t("Done. The computer goes back to how it was before the update when it restarts."); restartAfter = true; }
            else if (kind === "add") { status = Tr.t("Source added. Its software can now be installed and updated."); adding = false; }
            else if (kind === "remove") status = Tr.t("Source removed.");
            else if (kind === "enable") status = Tr.t("%1 is on. Check for updates to see what it offers.").arg(target.indexOf("basalt") === 0 ? chanTitle(target) : target);
            else if (kind === "disable") status = Tr.t("%1 is off.").arg(target.indexOf("basalt") === 0 ? chanTitle(target) : target);
            else status = Tr.t("Done.");
            proposal = null;
            Bus.refreshAssistant();
            load();
        });
    }

    function size(n) {
        if (!n || n <= 0) return Tr.t("%1 kB").arg(0);
        // Decimal separator of the person's locale (688,2 MB in Portuguese).
        if (n >= 1e9) return Tr.t("%1 GB").arg((n / 1e9).toLocaleString(Qt.locale(), "f", 1));
        if (n >= 1e6) return Tr.t("%1 MB").arg((n / 1e6).toLocaleString(Qt.locale(), "f", 1));
        return Tr.t("%1 kB").arg(Math.max(1, Math.round(n / 1e3)));
    }
    function when(iso) {
        if (!iso) return "";
        return Qt.formatDateTime(new Date(iso), Qt.locale().dateTimeFormat(Locale.ShortFormat));
    }
    function groupTitle(g) {
        switch (g) {
        case "security": return Tr.t("Security updates");
        case "basalt": return Tr.t("Basalt OS components");
        case "apps": return Tr.t("Apps");
        default: return Tr.t("System");
        }
    }
    function groupText(g) {
        switch (g) {
        case "security": return Tr.t("Fix security problems. Install these soon.");
        case "basalt": return Tr.t("The desktop, the assistant and the other parts made for Basalt OS.");
        case "apps": return Tr.t("Programs you open from the launcher.");
        default: return Tr.t("Libraries, drivers and services that everything else runs on.");
        }
    }
    function groupIcon(g) {
        switch (g) {
        case "security": return "shield";
        case "basalt": return "logo";
        case "apps": return "apps";
        default: return "box";
        }
    }
    // The newest install of the history (the one Undo applies to).
    readonly property int firstInstall: {
        const h = upd && upd.history ? upd.history : [];
        for (let i = 0; i < h.length; i++) if (h[i].kind === "install") return i;
        return -1;
    }
    function groupItems(g) { return updates.filter(u => u.group === g); }
    function groupSize(g) { return groupItems(g).reduce((s, u) => s + (u.download_size || 0), 0); }
    function toggleGroup(g) { const o = Object.assign({}, openGroups); o[g] = !o[g]; openGroups = o; }

    function chanTitle(id) {
        switch (id) {
        case "basalt": return Tr.t("Basalt OS");
        case "basalt-tools": return Tr.t("Basalt tools");
        case "basalt-testing": return Tr.t("Basalt testing");
        case "basalt-nonfree": return Tr.t("Additional drivers");
        default: return Tr.t("Additional drivers testing");
        }
    }
    function chanWhat(id) {
        switch (id) {
        case "basalt": return Tr.t("The software that makes Basalt OS: the desktop, the assistant, the security tools.");
        case "basalt-tools": return Tr.t("Optional tools from OpenBasalt, such as Samba Conductor.");
        case "basalt-testing": return Tr.t("Preview builds of Basalt OS components, before they reach everyone.");
        case "basalt-nonfree": return Tr.t("Drivers that are not free software, such as NVIDIA's graphics driver.");
        default: return Tr.t("Preview builds of the additional drivers, tested here before they reach everyone.");
        }
    }
    function chanWho(id) {
        switch (id) {
        case "basalt": return Tr.t("For everyone.");
        case "basalt-tools": return Tr.t("For people who use those tools.");
        case "basalt-testing": return Tr.t("For testers who want to help find problems early.");
        case "basalt-nonfree": return Tr.t("For computers with that hardware.");
        default: return Tr.t("For testers with supported hardware.");
        }
    }
    function chanRisk(id) {
        switch (id) {
        case "basalt": return Tr.t("Tested before release. Always on.");
        case "basalt-tools": return Tr.t("Stable. Turning it off stops updates of those tools.");
        case "basalt-testing": return Tr.t("Preview builds can break things.");
        case "basalt-nonfree": return Tr.t("Turned on by Additional drivers, together with the driver.");
        default: return Tr.t("Preview builds can break things, the display included.");
        }
    }
    function sigText(c) {
        if (!c.defined) return Tr.t("Not installed on this computer.");
        const s = c.signature || {};
        if (!s.gpgcheck) return Tr.t("Package signatures are not checked.");
        if (s.openbasalt) return Tr.t("Signed with the OpenBasalt key %1").arg(s.short);
        return Tr.t("Signed with a key that is not the OpenBasalt release key (%1)").arg(s.short || "?");
    }
    // A channel whose package is not published yet cannot be turned on
    // (updates.js channelAvailable); turning it off always works.
    function chanAvailable(c) { return U.channelAvailable(c, up.chan, up.drv); }
    function chanToggle(c) {
        if (!c.enabled && !chanAvailable(c)) return;
        if (c.enabled) up.propose("channels.propose", { op: "disable", repo: c.id }, "disable", c.id);
        else if (c.testing) up.propose("channels.propose", { op: "enable", repo: c.id, consent: "preview-builds-1" }, "enable", c.id);
        else up.propose("channels.propose", { op: "enable", repo: c.id }, "enable", c.id);
    }
    function visibleChannel(c) {
        if (c.id === "basalt-nonfree-testing") return up.driverHardware || up.showAllChannels || c.enabled;
        if (c.id === "basalt-nonfree") return c.defined || up.driverHardware || up.showAllChannels;
        return true;
    }
    // The key's fingerprint and owner of a stored source.add proposal.
    function evidence(prefix) {
        const ev = proposal && proposal.evidence ? proposal.evidence : [];
        for (let i = 0; i < ev.length; i++) if (ev[i].indexOf(prefix) === 0) return ev[i].substring(prefix.length);
        return "";
    }
    // The source a stored source.add is about: the catalog entry, or the
    // address the person gave.
    function sourceLabel() {
        const cat = chan && chan.catalog ? chan.catalog : [];
        for (let i = 0; i < cat.length; i++) if (cat[i].id === ptarget) return Tr.t("%1, from %2").arg(cat[i].name).arg(cat[i].publisher);
        const ev = proposal && proposal.evidence && proposal.evidence.length > 0 ? proposal.evidence[0] : "";
        const sp = ev.lastIndexOf(" ");
        return sp > 0 ? ev.substring(sp + 1) : ev;
    }
    function confirmTitle() {
        if (applying && (pkind === "install" || pkind === "security")) return Tr.t("Installing updates");
        if (applying && pkind === "rollback") return Tr.t("Undoing the update");
        switch (pkind) {
        case "install": return offlineAll ? Tr.t("Restart and install the updates?") : Tr.t("Install the updates?");
        case "security": return offlineSecurity ? Tr.t("Restart and install the security updates?") : Tr.t("Install the security updates?");
        case "rollback": return Tr.t("Undo the last update?");
        case "enable": return proposal && ptarget.indexOf("testing") >= 0 ? Tr.t("Turn on preview builds?") : Tr.t("Turn on %1?").arg(chanTitle(ptarget));
        case "disable": return Tr.t("Turn off %1?").arg(ptarget.indexOf("basalt") === 0 ? chanTitle(ptarget) : ptarget);
        case "add": return Tr.t("Add this software source?");
        default: return Tr.t("Remove this software source?");
        }
    }
    function confirmText() {
        switch (pkind) {
        case "install":
        case "security": return pOffline
                         ? Tr.t("Some of these updates replace core parts of the system, so they install while the computer restarts, before the desktop starts. Once they are downloaded, the computer restarts after a 60 second countdown you can cancel. A snapshot is taken before and after, so you can undo it from this page.")
                         : Tr.t("A snapshot of the system is taken first, so you can undo the update from this page. Some updates need a restart to take effect.");
        case "rollback": return Tr.t("The system goes back to the snapshot taken just before the last update, at the next start. Your files in your home folder stay as they are.");
        case "enable": return ptarget.indexOf("testing") >= 0
                       ? Tr.t("Preview builds can break things. A snapshot is taken before each update so you can go back.")
                       : Tr.t("Updates will also come from this channel. Its packages are signed with the OpenBasalt release key.");
        case "disable": return Tr.t("No more updates come from it. What is already installed stays.");
        case "add": return Tr.t("Software from this source can change your whole system. Only add sources you trust.");
        default: return Tr.t("No more software comes from it and its key is no longer trusted. Programs already installed from it stay, without updates.");
        }
    }
    function confirmVerb() {
        switch (pkind) {
        case "install":
        case "security": return pOffline ? Tr.t("Restart and update")
                                : (pkind === "install" ? Tr.n("Install %1 update", "Install %1 updates", up.counts.total || 0)
                                                       : Tr.n("Install %1 security update", "Install %1 security updates", up.counts.security || 0));
        case "rollback": return Tr.t("Undo the update");
        case "enable": return ptarget.indexOf("testing") >= 0 ? Tr.t("Turn on preview builds") : Tr.t("Turn on");
        case "disable": return Tr.t("Turn off");
        case "add": return Tr.t("Trust and add");
        default: return Tr.t("Remove source");
        }
    }

    onVisibleChanged: if (visible) load()
    // While an update installs, follow its steps.
    property bool polling: false
    Timer {
        interval: 2500; repeat: true
        running: up.visible && (up.applying || up.running !== null)
        onTriggered: {
            if (up.polling) return;
            up.polling = true;
            Bus.call("updates.progress", {}, (ok, res) => {
                up.polling = false;
                if (ok && up.upd) up.upd = Object.assign({}, up.upd, { running: res.running || null, history: res.history || [], undo: res.undo || null });
            });
        }
    }

    Txt { text: Tr.t("Updates and channels"); role: "display" }
    Txt {
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
        text: Tr.t("Keep Basalt OS up to date and choose where its software comes from. Every update starts with a snapshot, so you can go back.")
    }
    Txt { visible: up.loading && up.upd === null; text: Tr.t("Looking for what is installed."); color: Theme.textMuted }
    Card {
        visible: up.comingSoon
        accent: Theme.border
        ColumnLayout {
            Layout.fillWidth: true
            Txt { text: Tr.t("Updates from this page are coming soon"); font.weight: Font.DemiBold }
            Txt {
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
                text: Tr.t("The system assistant on this computer is older than this page. A coming update of Basalt OS brings it.")
            }
        }
    }
    Txt {
        visible: up.status !== "" && !up.applying
        text: up.status
        color: up.statusError ? Theme.danger : Theme.success
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
        Accessible.role: Accessible.AlertMessage
        Accessible.name: up.status
    }

    // ---- Updates ----------------------------------------------------------
    Section { visible: !up.comingSoon; title: Tr.t("Updates") }

    // Installing now: the steps.
    Card {
        visible: up.running !== null || up.applying
        accent: Theme.accent
        ColumnLayout {
            Layout.fillWidth: true
            spacing: Theme.s2
            Txt { text: up.running && up.running.kind === "rollback" ? Tr.t("Undoing the update") : Tr.t("Installing updates"); font.weight: Font.DemiBold }
            Txt {
                visible: up.running !== null
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
                text: !up.running ? "" : Tr.t("Step %1 of %2: %3").arg(up.running.step).arg(up.running.steps)
                      .arg(up.running.what === "snapshot" ? Tr.t("taking a snapshot") : (up.running.what === "checks" ? Tr.t("checking that everything works") : up.running.what))
            }
            Rectangle {
                Layout.fillWidth: true; implicitHeight: 6; radius: 3; color: Theme.surfaceAlt
                Rectangle {
                    height: parent.height; radius: 3; color: Theme.accent
                    width: up.running && up.running.steps > 0 ? parent.width * Math.max(0.05, (up.running.step - 0.5) / up.running.steps) : parent.width * 0.05
                    Behavior on width { NumberAnimation { duration: Theme.normal } }
                }
                Accessible.role: Accessible.ProgressBar
                Accessible.name: Tr.t("Installing updates")
            }
            Txt {
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "small"; color: Theme.textMuted
                text: Tr.t("You can keep working. Do not turn the computer off.")
            }
        }
    }

    // Restart needed.
    Card {
        visible: up.restartNeeded && !up.applying
        accent: Theme.warning
        ColumnLayout {
            Layout.fillWidth: true
            spacing: Theme.s2
            Row {
                spacing: Theme.s2
                Icon { name: "restart"; color: Theme.warning; size: Theme.fontSize * 1.4 }
                Txt { text: Tr.t("Restart to finish"); font.weight: Font.DemiBold }
            }
            Txt {
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
                text: up.upd && up.upd.restart && up.upd.restart.rollback_pending || up.restartAfter
                      ? Tr.t("The system goes back to the snapshot when the computer restarts.")
                      : Tr.t("Some updates, such as a new kernel, take effect when the computer restarts. Save your work first.")
            }
            Flow {
                Layout.fillWidth: true
                Btn { text: Tr.t("Restart now"); icon: "power"; variant: "primary"; focusable: true; e2e: "updates-restart"; onClicked: Quickshell.execDetached(["systemctl", "reboot"]) }
            }
        }
    }

    // Summary and Check.
    Card {
        visible: !up.comingSoon && up.upd !== null && up.running === null && !up.applying
        ColumnLayout {
            Layout.fillWidth: true
            spacing: Theme.s2
            Row {
                spacing: Theme.s2
                Icon { name: up.updates.length === 0 ? "check" : "download"; color: up.updates.length === 0 ? Theme.success : Theme.accent; size: Theme.fontSize * 1.4 }
                Txt {
                    font.weight: Font.DemiBold
                    text: up.updates.length === 0 ? Tr.t("Basalt OS is up to date")
                          : Tr.n("%1 update is ready", "%1 updates are ready", up.updates.length)
                }
            }
            Txt {
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
                text: up.upd && up.upd.checked ? Tr.t("Last checked: %1").arg(up.when(up.upd.checked)) : Tr.t("Not checked yet on this computer.")
            }
            Txt {
                visible: up.updates.length > 0
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
                text: Tr.t("%1 to download.").arg(up.size(up.upd ? up.upd.download_size : 0))
            }
            Flow {
                Layout.fillWidth: true
                spacing: Theme.s2
                Btn {
                    text: up.checking ? Tr.t("Checking for updates") : Tr.t("Check for updates")
                    icon: "restart"; variant: up.updates.length === 0 ? "primary" : "outline"; focusable: true; e2e: "updates-check"
                    enabled: !up.checking && !up.busy; opacity: enabled ? 1 : 0.5
                    onClicked: up.check()
                }
            }
        }
    }

    // The groups.
    Repeater {
        model: up.running === null ? up.groups : []
        delegate: Card {
            id: grp
            required property var modelData
            readonly property var items: up.groupItems(modelData)
            visible: items.length > 0
            accent: modelData === "security" ? Theme.warning : Theme.border
            ColumnLayout {
                Layout.fillWidth: true
                spacing: Theme.s1
                RowLayout {
                    Layout.fillWidth: true
                    spacing: Theme.s2
                    Icon { name: up.groupIcon(grp.modelData); color: grp.modelData === "security" ? Theme.warning : Theme.textMuted; size: Theme.fontSize * 1.4 }
                    Txt { Layout.fillWidth: true; text: up.groupTitle(grp.modelData); font.weight: Font.DemiBold }
                    Txt { text: Tr.n("%1 update", "%1 updates", grp.items.length) + "  " + up.size(up.groupSize(grp.modelData)); color: Theme.textMuted; role: "small" }
                }
                Txt { Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted; text: up.groupText(grp.modelData) }
                Btn {
                    text: up.openGroups[grp.modelData] ? Tr.t("Hide the list") : Tr.t("Show the list")
                    icon: "chevron"; variant: "ghost"; focusable: true; e2e: "updates-group-" + grp.modelData
                    onClicked: up.toggleGroup(grp.modelData)
                }
                Repeater {
                    model: up.openGroups[grp.modelData] ? grp.items : []
                    delegate: ColumnLayout {
                        required property var modelData
                        Layout.fillWidth: true
                        spacing: 0
                        Txt { Layout.fillWidth: true; text: modelData.name; font.weight: Font.Medium; elide: Text.ElideRight }
                        Txt {
                            Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "small"; color: Theme.textMuted
                            text: (modelData.from ? Tr.t("%1 to %2").arg(modelData.from).arg(modelData.to) : Tr.t("new: %1").arg(modelData.to))
                                  + "  " + up.size(modelData.download_size)
                                  + (modelData.advisory ? "  " + modelData.advisory + (modelData.severity && modelData.severity !== "None" ? " (" + modelData.severity + ")" : "") : "")
                        }
                    }
                }
            }
        }
    }

    // An offline update is staged: it installs at the next restart.
    Card {
        visible: up.staged && up.running === null && !up.applying
        accent: Theme.accent
        ColumnLayout {
            Layout.fillWidth: true
            spacing: Theme.s2
            Row {
                spacing: Theme.s2
                Icon { name: "restart"; color: Theme.accent; size: Theme.fontSize * 1.4 }
                Txt { text: Tr.t("Updates ready to install"); font.weight: Font.DemiBold }
            }
            Txt {
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
                text: Tr.t("They are downloaded and install while the computer restarts, before the desktop starts. Save your work first.")
            }
            Flow {
                Layout.fillWidth: true
                Btn { text: Tr.t("Restart and update"); icon: "restart"; variant: "primary"; focusable: true; e2e: "updates-restart-staged"; onClicked: Ui.restartForUpdate() }
            }
        }
    }

    // Install.
    ColumnLayout {
        visible: up.updates.length > 0 && up.running === null && !up.applying && up.proposal === null && !up.staged
        Layout.fillWidth: true
        spacing: Theme.s2
        Flow {
            Layout.fillWidth: true
            spacing: Theme.s2
            Btn {
                visible: (up.counts.security || 0) > 0 && (up.counts.security || 0) < up.updates.length
                text: Tr.t("Security updates only"); icon: "shield"; variant: "outline"; focusable: true; e2e: "updates-install-security"
                enabled: !up.busy; onClicked: up.propose("updates.propose", { security: true }, "security")
            }
            Btn {
                text: up.offlineAll ? Tr.t("Restart and update") : Tr.t("Install updates")
                icon: up.offlineAll ? "restart" : "download"; variant: "primary"; focusable: true; e2e: "updates-install"
                enabled: !up.busy; onClicked: up.propose("updates.propose", { security: false }, "install")
            }
        }
        Txt {
            Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "small"; color: Theme.textMuted
            text: up.offlineAll
                  ? Tr.t("These updates include core parts of the system, so they install while the computer restarts, before the desktop starts. A snapshot is taken before and after.")
                  : Tr.t("A snapshot is taken first. You can undo the update from this page.")
        }
    }

    // Automatic security updates: off by default; a rule of the approval
    // gate once it starts scheduled jobs.
    RowLayout {
        visible: !up.comingSoon && up.upd !== null
        Layout.fillWidth: true
        spacing: Theme.s3
        ColumnLayout {
            Layout.fillWidth: true
            spacing: 0
            Txt { text: Tr.t("Install security updates automatically"); font.weight: Font.Medium; Layout.fillWidth: true }
            Txt {
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "small"; color: Theme.textMuted
                text: Tr.t("Coming with the approval gate's schedules. Until then, security updates are shown here first.")
            }
        }
        Toggle { checked: false; enabled: false; label: Tr.t("Install security updates automatically"); e2e: "updates-auto-security" }
    }

    // History and undo.
    Section { visible: up.upd !== null && up.upd.history && up.upd.history.length > 0; title: Tr.t("Update history") }
    Repeater {
        model: up.upd && up.upd.history ? up.upd.history : []
        delegate: RowLayout {
            required property var modelData
            required property int index
            Layout.fillWidth: true
            spacing: Theme.s2
            Icon {
                name: modelData.kind === "rollback" ? "undo" : (modelData.status === "applied" ? "check" : (modelData.status === "undone" ? "undo" : "warning"))
                color: modelData.status === "failed" ? Theme.danger : Theme.textMuted
                size: Theme.fontSize * 1.3
            }
            ColumnLayout {
                Layout.fillWidth: true
                spacing: 0
                Txt {
                    Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
                    text: modelData.kind === "rollback" ? Tr.t("Went back to snapshot %1").arg(modelData.snapshot)
                          : (modelData.scope === "security" ? Tr.n("%1 security update", "%1 security updates", modelData.count) : Tr.n("%1 update", "%1 updates", modelData.count))
                }
                Txt {
                    Layout.fillWidth: true; role: "small"; color: Theme.textMuted
                    text: up.when(modelData.time) + "  " + (modelData.kind === "rollback" ? (modelData.ok ? Tr.t("done") : Tr.t("did not finish"))
                          : (modelData.status === "applied" ? Tr.t("installed") : (modelData.status === "undone" ? Tr.t("undone")
                          : (modelData.status === "interrupted" ? Tr.t("stopped before the end: undo it to be sure")
                          : (modelData.status === "scheduled" ? Tr.t("installs at the next start") : Tr.t("did not finish"))))))
                }
            }
            Btn {
                // Not while a rollback already waits for the next start.
                visible: !!(up.upd && up.upd.undo && up.upd.undo.proposal === modelData.proposal && index === up.firstInstall)
                         && !up.restartAfter && !(up.upd.restart && up.upd.restart.rollback_pending)
                text: Tr.t("Undo the last update"); icon: "undo"; variant: "outline"; focusable: true; e2e: "updates-undo"
                enabled: !up.busy && up.proposal === null
                onClicked: up.propose("updates.rollback", {}, "rollback")
            }
        }
    }

    // ---- Channels ---------------------------------------------------------
    Section { visible: !up.comingSoon && up.chan !== null; title: Tr.t("Channels") }
    Txt {
        visible: up.chan !== null && !up.comingSoon
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
        text: Tr.t("Where Basalt OS gets its software. Everything that is not final goes to a testing channel first.")
    }
    Repeater {
        model: up.chan && up.chan.channels ? up.chan.channels : []
        delegate: Card {
            id: cc
            required property var modelData
            // Off and not available here yet: the toggle is disabled.
            readonly property bool available: modelData.enabled || up.chanAvailable(modelData)
            visible: up.visibleChannel(modelData)
            accent: modelData.testing && modelData.enabled ? Theme.warning : Theme.border
            RowLayout {
                Layout.fillWidth: true
                spacing: Theme.s3
                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: Theme.s1
                    Txt { text: up.chanTitle(cc.modelData.id); font.weight: Font.DemiBold; Layout.fillWidth: true }
                    Txt { Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; text: up.chanWhat(cc.modelData.id) }
                    Txt { Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted; text: up.chanWho(cc.modelData.id) + " " + up.chanRisk(cc.modelData.id) }
                    Row {
                        spacing: Theme.s1
                        Icon { name: "key"; size: Theme.fontSize * 1.1; color: cc.modelData.signature && cc.modelData.signature.openbasalt ? Theme.success : Theme.textMuted }
                        Txt { role: "small"; color: Theme.textMuted; text: up.sigText(cc.modelData) }
                    }
                    Txt {
                        visible: !cc.available
                        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "small"; color: Theme.textMuted
                        text: Tr.t("Not available yet: its packages are not published.")
                    }
                    Btn {
                        visible: cc.modelData.id === "basalt-nonfree"
                        text: Tr.t("Open Additional drivers"); icon: "chip"; variant: "ghost"; focusable: true; e2e: "channels-open-drivers"
                        onClicked: Ui.settingsPage = "drivers"
                    }
                }
                Txt {
                    visible: !cc.modelData.toggle
                    role: "small"; color: Theme.textMuted
                    text: cc.modelData.id === "basalt" ? Tr.t("Always on") : (cc.modelData.enabled ? Tr.t("On") : Tr.t("Off"))
                }
                Toggle {
                    visible: cc.modelData.toggle
                    checked: cc.modelData.enabled
                    busy: up.busy && up.ptarget === cc.modelData.id
                    // Shown, but off and disabled until its packages are published.
                    enabled: cc.available && !up.busy && up.proposal === null && !up.applying
                    label: up.chanTitle(cc.modelData.id)
                    e2e: "channel-" + cc.modelData.id
                    onToggled: up.chanToggle(cc.modelData)
                }
            }
        }
    }
    Btn {
        visible: up.chan !== null && !up.driverHardware && !up.showAllChannels
        text: Tr.t("Show all channels"); icon: "list"; variant: "ghost"; focusable: true; e2e: "channels-show-all"
        onClicked: up.showAllChannels = true
    }

    // ---- Other software sources ------------------------------------------
    Section { visible: up.chan !== null && !up.comingSoon; title: Tr.t("Other software sources") }
    Txt {
        visible: up.chan !== null && !up.comingSoon
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
        text: Tr.t("Apps and tools from other publishers. Each source is checked with its own signing key.")
    }
    Repeater {
        model: up.chan && up.chan.sources ? up.chan.sources : []
        delegate: Card {
            id: sc
            required property var modelData
            ColumnLayout {
                Layout.fillWidth: true
                spacing: Theme.s1
                RowLayout {
                    Layout.fillWidth: true
                    spacing: Theme.s3
                    Txt { Layout.fillWidth: true; text: sc.modelData.name; font.weight: Font.DemiBold; wrapMode: Text.Wrap; elide: Text.ElideNone }
                    Toggle {
                        visible: sc.modelData.kind === "rpm"
                        checked: sc.modelData.enabled
                        enabled: !up.busy && up.proposal === null && !up.applying
                        label: sc.modelData.name
                        e2e: "source-toggle-" + sc.modelData.id
                        onToggled: up.propose("channels.propose", { op: sc.modelData.enabled ? "disable" : "enable", repo: sc.modelData.id }, sc.modelData.enabled ? "disable" : "enable", sc.modelData.id)
                    }
                }
                Txt { Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "small"; color: Theme.textMuted; text: sc.modelData.url }
                Row {
                    spacing: Theme.s1
                    Icon { name: "key"; size: Theme.fontSize * 1.1; color: Theme.textMuted }
                    Txt { role: "small"; color: Theme.textMuted; text: Tr.t("Key %1, %2").arg(sc.modelData.short).arg(sc.modelData.key_owner || "") }
                }
                Txt {
                    Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "small"; color: Theme.textMuted
                    text: Tr.t("Added %1").arg(up.when(sc.modelData.added)) + (sc.modelData.by ? "  " + Tr.t("Decided by: %1").arg(sc.modelData.by) : "")
                }
                Btn {
                    text: Tr.t("Remove"); icon: "close"; variant: "ghost"; focusable: true; e2e: "source-remove-" + sc.modelData.group
                    enabled: !up.busy && up.proposal === null
                    onClicked: up.propose("channels.propose", { op: "remove", repo: sc.modelData.group }, "remove", sc.modelData.group)
                }
            }
        }
    }
    Repeater {
        model: up.chan && up.chan.other ? up.chan.other : []
        delegate: Txt {
            required property var modelData
            Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "small"; color: Theme.textMuted
            text: Tr.t("%1 was added outside these settings (%2); it is shown, not changed here.").arg(modelData.name || modelData.id).arg(modelData.file)
        }
    }
    Btn {
        visible: up.chan !== null && !up.comingSoon && !up.adding
        text: Tr.t("Add a source"); icon: "plus"; variant: "outline"; focusable: true; e2e: "sources-add"
        enabled: up.proposal === null
        onClicked: up.adding = true
    }

    // Add a source: the catalog, a COPR project, or a custom source.
    Card {
        visible: up.adding && up.proposal === null
        accent: Theme.accent
        ColumnLayout {
            Layout.fillWidth: true
            spacing: Theme.s3
            Txt { text: Tr.t("Add a software source"); font.weight: Font.DemiBold }
            Txt {
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
                text: Tr.t("Well-known sources, with the address and signing key Basalt OS checked for you.")
            }
            Flow {
                Layout.fillWidth: true
                spacing: Theme.s2
                Repeater {
                    model: up.chan && up.chan.catalog ? up.chan.catalog : []
                    delegate: Rectangle {
                        id: ce
                        required property var modelData
                        width: Math.min(300, up.width - Theme.s6 * 2)
                        implicitHeight: ccol.implicitHeight + Theme.s3 * 2
                        radius: Theme.radiusMd; color: Theme.bg; border.width: 1; border.color: Theme.border
                        ColumnLayout {
                            id: ccol
                            anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top; anchors.margins: Theme.s3
                            spacing: Theme.s1
                            Txt { text: ce.modelData.name; font.weight: Font.DemiBold; Layout.fillWidth: true }
                            Txt { Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "small"; text: ce.modelData.description }
                            Txt { Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "small"; color: Theme.textMuted; text: Tr.t("From %1, key %2").arg(ce.modelData.publisher).arg(ce.modelData.short) }
                            Btn {
                                text: ce.modelData.added ? Tr.t("Added") : (ce.modelData.available ? Tr.t("Add") : Tr.t("Needs Flatpak"))
                                icon: ce.modelData.added ? "check" : "plus"; variant: "outline"; focusable: true; e2e: "source-add-" + ce.modelData.id
                                enabled: !ce.modelData.added && ce.modelData.available && !up.busy
                                opacity: enabled ? 1 : 0.5
                                onClicked: up.propose("channels.propose", { op: "add", entry: ce.modelData.id }, "add", ce.modelData.id)
                            }
                        }
                    }
                }
            }
            Txt { text: Tr.t("A COPR project"); font.weight: Font.Medium }
            Txt {
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "small"; color: Theme.textMuted
                text: Tr.t("Packages built by people on Fedora's COPR service. Basalt OS does not review them.")
            }
            RowLayout {
                Layout.fillWidth: true
                spacing: Theme.s2
                Field { id: copr; Layout.fillWidth: true; placeholder: Tr.t("owner/project"); objectName: "e2e:source-copr-project"; Accessible.name: Tr.t("COPR project") }
                Btn {
                    text: Tr.t("Add"); icon: "plus"; variant: "outline"; focusable: true; e2e: "source-add-copr"
                    enabled: copr.text.indexOf("/") > 0 && !up.busy
                    onClicked: up.propose("channels.propose", { op: "add", entry: "copr", project: copr.text.trim() }, "add", "copr")
                }
            }
            Txt { text: Tr.t("Another source"); font.weight: Font.Medium }
            Txt {
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "small"; color: Theme.textMuted
                text: Tr.t("The address of its .repo file, or its address and the address of its signing key. Only https sources that sign their packages and package lists can be added.")
            }
            Field { id: repoUrl; Layout.fillWidth: true; placeholder: Tr.t("https://example.org/example.repo"); objectName: "e2e:source-repo-url"; Accessible.name: Tr.t("Address of the .repo file") }
            Field { id: baseUrl; Layout.fillWidth: true; visible: repoUrl.text === ""; placeholder: Tr.t("Address of the repository (https)"); objectName: "e2e:source-baseurl"; Accessible.name: Tr.t("Address of the repository (https)") }
            Field { id: keyUrl; Layout.fillWidth: true; visible: repoUrl.text === ""; placeholder: Tr.t("Address of its signing key (https)"); objectName: "e2e:source-key-url"; Accessible.name: Tr.t("Address of its signing key (https)") }
            Field { id: srcName; Layout.fillWidth: true; visible: repoUrl.text === ""; placeholder: Tr.t("A name for it"); objectName: "e2e:source-name"; Accessible.name: Tr.t("A name for it") }
            Txt {
                readonly property string all: repoUrl.text + baseUrl.text + keyUrl.text
                visible: /http:\/\//i.test(all)
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.danger
                text: Tr.t("Only https addresses are accepted: software and keys must not travel unprotected.")
            }
            Flow {
                Layout.fillWidth: true
                spacing: Theme.s2
                Btn { text: Tr.t("Not now"); variant: "ghost"; focusable: true; e2e: "sources-add-close"; onClicked: up.adding = false }
                Btn {
                    text: Tr.t("Check this source"); icon: "key"; variant: "primary"; focusable: true; e2e: "source-add-custom"
                    enabled: !up.busy && (repoUrl.text.indexOf("https://") === 0 || (baseUrl.text.indexOf("https://") === 0 && keyUrl.text.indexOf("https://") === 0 && srcName.text.trim() !== ""))
                    opacity: enabled ? 1 : 0.5
                    onClicked: up.propose("channels.propose", repoUrl.text !== ""
                                          ? { op: "add", entry: "custom", repo_url: repoUrl.text.trim() }
                                          : { op: "add", entry: "custom", baseurl: baseUrl.text.trim(), key_url: keyUrl.text.trim(), name: srcName.text.trim() }, "add", "custom")
                }
            }
        }
    }

    // ---- The stored proposal: a sheet over the window, plain words first,
    // the exact commands under Details. Escape or Not now dismisses it.
    // A holder outside the layout: the sheet itself is reparented to the
    // window's content item.
    Item {
        id: sheetHolder
        Layout.preferredWidth: 0
        Layout.preferredHeight: 0
        Item {
            id: sheet
            visible: up.proposal !== null
            parent: up.Window.window ? up.Window.window.contentItem : sheetHolder
            anchors.fill: parent
            z: 1000
            focus: visible
            onVisibleChanged: if (visible) primaryBtn.forceActiveFocus()
            Keys.onEscapePressed: if (!up.busy) up.decide(false)
            Rectangle { anchors.fill: parent; color: Theme.scrim }
            MouseArea { anchors.fill: parent; hoverEnabled: true }   // the page below takes no input
            Rectangle {
                id: sheetCard
                anchors.centerIn: parent
                width: Math.min(620, parent.width - Theme.s4 * 2)
                height: Math.min(sheetCol.implicitHeight + Theme.s4 * 2, parent.height - Theme.s4 * 2)
                radius: Theme.radiusLg; color: Theme.surface; border.width: 1
                border.color: up.pkind === "add" || (up.pkind === "enable" && up.ptarget.indexOf("testing") >= 0) || up.pkind === "rollback" ? Theme.warning : Theme.border
                Accessible.role: Accessible.Dialog
                Accessible.name: up.confirmTitle()
                Flickable {
                    anchors.fill: parent; anchors.margins: Theme.s4
                    contentHeight: sheetCol.implicitHeight
                    clip: true
                    ColumnLayout {
                        id: sheetCol
                        width: parent.width
                        spacing: Theme.s3
                        Row {
                            spacing: Theme.s2
                            Rectangle {
                                width: Theme.fontSize * 3; height: width; radius: Theme.radiusMd
                                readonly property bool calm: up.pkind === "install" || up.pkind === "security" || up.pkind === "disable" || up.pkind === "remove"
                                                             || (up.pkind === "enable" && up.ptarget.indexOf("testing") < 0)
                                color: Theme.alpha(calm ? Theme.accent : Theme.warning, 0.16)
                                Icon {
                                    anchors.centerIn: parent
                                    name: up.pkind === "add" ? "key" : (up.pkind === "rollback" ? "undo" : (up.pkind === "install" || up.pkind === "security" ? "download"
                                          : (up.pkind === "enable" && up.ptarget.indexOf("testing") >= 0 ? "warning" : "info")))
                                    color: parent.calm ? Theme.accent : Theme.warning; size: Theme.fontSize * 1.6
                                }
                            }
                            Txt { text: up.confirmTitle(); role: "title"; anchors.verticalCenter: parent.verticalCenter }
                        }
                        Txt { Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; text: up.confirmText() }
                        // A new source: who signs it.
                        ColumnLayout {
                            visible: up.pkind === "add"
                            Layout.fillWidth: true
                            spacing: Theme.s1
                            Txt { Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; font.weight: Font.Medium; text: up.sourceLabel() }
                            Txt { Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; text: Tr.t("Signing key: %1").arg(up.evidence("fingerprint: ")); role: "mono" }
                            Txt { Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; text: Tr.t("It belongs to: %1").arg(up.evidence("owner: ")) }
                            Txt {
                                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
                                text: up.ptarget === "custom" || up.ptarget === "copr"
                                      ? Tr.t("Basalt OS does not know this source. Compare the fingerprint with the one its publisher shows before you trust it.")
                                      : Tr.t("Basalt OS checked this key with its publisher, and checks it again when the source is added.")
                            }
                            Txt {
                                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted; role: "small"
                                text: Tr.t("Adding a source needs an administrator's password.")
                            }
                        }
                        Txt {
                            visible: up.pkind === "install" || up.pkind === "security"
                            Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
                            text: Tr.t("%1 to download.").arg(up.size(up.pkind === "security" ? up.groupSize("security") : (up.upd ? up.upd.download_size : 0)))
                        }
                        Txt {
                            visible: up.status !== "" && up.statusError
                            Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.danger
                            text: up.status
                        }
                        Txt {
                            visible: up.applying
                            Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
                            text: up.running ? Tr.t("Step %1 of %2: %3").arg(up.running.step).arg(up.running.steps)
                                                .arg(up.running.what === "snapshot" ? Tr.t("taking a snapshot") : (up.running.what === "checks" ? Tr.t("checking that everything works") : up.running.what))
                                             : Tr.t("Waiting for your password.")
                        }
                        Btn {
                            text: up.showDetails ? Tr.t("Hide details") : Tr.t("Details")
                            icon: "list"; variant: "ghost"; focusable: true; e2e: "updates-details"
                            onClicked: up.showDetails = !up.showDetails
                        }
                        Rectangle {
                            visible: up.showDetails
                            Layout.fillWidth: true
                            Layout.preferredHeight: Math.min(detailsText.implicitHeight + Theme.s2 * 2, 260)
                            radius: Theme.radiusMd; color: Theme.bg; border.width: 1; border.color: Theme.border
                            clip: true
                            Flickable {
                                anchors.fill: parent; anchors.margins: Theme.s2
                                contentHeight: detailsText.implicitHeight
                                clip: true
                                Txt {
                                    id: detailsText
                                    width: parent.width
                                    wrapMode: Text.Wrap; elide: Text.ElideNone; role: "mono"; color: Theme.textMuted
                                    text: up.proposal ? up.proposal.report : ""
                                }
                            }
                        }
                        Flow {
                            Layout.fillWidth: true
                            layoutDirection: Qt.RightToLeft
                            spacing: Theme.s2
                            Btn { id: primaryBtn; text: up.confirmVerb(); icon: "check"; variant: "primary"; focusable: true; e2e: "updates-confirm"; enabled: !up.busy; opacity: enabled ? 1 : 0.45; onClicked: up.decide(true) }
                            Btn { text: Tr.t("Not now"); variant: "outline"; focusable: true; e2e: "updates-cancel"; enabled: !up.busy; opacity: enabled ? 1 : 0.45; onClicked: up.decide(false) }
                        }
                    }
                }
            }
        }
    }

    // After an apply: the assistant's output, on demand.
    Btn {
        visible: up.result !== "" && up.proposal === null
        text: up.showResult ? Tr.t("Hide the assistant's report") : Tr.t("Show the assistant's report")
        icon: "list"; variant: "ghost"; focusable: true; e2e: "updates-result"
        onClicked: up.showResult = !up.showResult
    }
    Txt {
        visible: up.showResult && up.result !== ""
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "mono"; color: Theme.textMuted
        text: up.result
    }

    Btn {
        visible: up.chan !== null
        text: Tr.t("How updates are verified"); icon: "shield"; variant: "ghost"; focusable: true; e2e: "updates-docs"
        onClicked: Qt.openUrlExternally(up.chan.docs)
    }

    component Section: Txt {
        property string title
        text: title
        role: "large"
        font.weight: Font.DemiBold
        topPadding: Theme.s2
    }
    // A card: surface, 1 px border (the accent says what kind), content.
    component Card: Rectangle {
        id: card
        default property alias content: inner.data
        property color accent: Theme.border
        Layout.fillWidth: true
        implicitHeight: inner.implicitHeight + Theme.s3 * 2
        radius: Theme.radiusMd; color: Theme.bg; border.width: 1; border.color: accent
        ColumnLayout {
            id: inner
            anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top
            anchors.margins: Theme.s3
            spacing: Theme.s2
        }
    }
}
