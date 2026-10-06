import QtQuick
import QtQuick.Layouts
import Quickshell

// Settings, Additional drivers: the graphics hardware and the driver that
// fits it, today the NVIDIA driver of Basalt OS's opt-in basalt-nonfree
// repository for GPUs of the Turing generation and newer. Everything
// comes from the system assistant (basalt drivers): the GPUs by PCI id,
// NVIDIA's list of supported GPUs, the state of an installed driver, the
// Secure Boot state and the NVIDIA license text. Nothing changes here by
// itself: Install stores the assistant's driver.install proposal, the
// person reads it on the confirmation step and applies it (pkexec, the
// person authenticates; basalt apply takes a snapshot first). After a
// failed first start the page explains why and offers the rollback to the
// snapshot taken before the install, the same way.
ColumnLayout {
    id: ds
    spacing: Theme.s4

    property var info: null
    property bool loading: false
    property string status: ""
    property bool statusError: false
    property bool accepted: false
    property bool showLicense: false
    property bool busy: false
    property var proposal: null        // a stored proposal waiting for confirmation: {id, code, report, title}
    property bool installed: false     // the install was applied in this session

    readonly property var rec: info ? info.recommendation : null
    readonly property var st: info ? info.state : null
    readonly property var sb: info ? info.secure_boot : null
    readonly property var gpus: info ? (info.gpus || []) : []
    readonly property bool geforce: gpus.some(g => g.geforce)
    readonly property bool caBlocked: sb !== null && sb.enabled && !sb.ca_enrolled && !sb.ca_pending

    function load() {
        loading = true;
        Bus.call("drivers.state", {}, (ok, res) => {
            loading = false;
            if (ok) { info = res; }
            else { status = res; statusError = true; }
        });
    }
    // Store the proposal (install or rollback), then show it for confirmation.
    function propose(op) {
        busy = true; status = ""; statusError = false;
        Bus.call(op, op === "drivers.propose" ? { variant: rec ? rec.variant : "" } : {}, (ok, res) => {
            busy = false;
            if (ok) { proposal = res; }
            else { status = res; statusError = true; }
        });
    }
    function decide(apply) {
        if (!proposal) return;
        busy = true;
        status = apply ? Tr.t("Waiting for authentication. Installing can take a few minutes.") : "";
        statusError = false;
        Bus.call(apply ? "assistant.apply" : "assistant.ignore", apply ? { id: proposal.id, code: proposal.code } : { id: proposal.id }, (ok, res) => {
            busy = false;
            const good = ok && res.ok;
            if (apply) {
                status = good ? Tr.t("Done. Restart the computer to finish: the first start checks the driver.") : Tr.t("Not applied. The assistant's output is below.");
                statusError = !good;
                if (good) installed = true;
                result = ok && res.output ? res.output : "";
            } else {
                status = "";
            }
            proposal = null;
            Bus.refreshAssistant();
            load();
        });
    }
    property string result: ""

    function supportText(g) {
        if (g.vendor !== "nvidia") return g.driver ? Tr.t("Driver in use: %1").arg(g.driver) : Tr.t("No driver in use");
        switch (g.support) {
        case "supported": return Tr.t("Supported by the NVIDIA driver %1 (open kernel modules)").arg(info.recommendation.version || "");
        case "legacy-580": return Tr.t("Needs NVIDIA's 580 legacy driver (Maxwell, Pascal, Volta)");
        case "unsupported": return Tr.t("Only NVIDIA's old %1 driver supports it; nouveau drives it").arg(g.branch);
        default: return Tr.t("Not in the list of this NVIDIA driver version");
        }
    }

    onVisibleChanged: if (visible) load()

    Txt { text: Tr.t("Additional drivers"); role: "display" }
    Txt {
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
        text: Tr.t("Drivers that are not free software but make your hardware work better. Nothing is installed until you read what changes and confirm.")
    }
    Txt {
        visible: ds.loading
        text: Tr.t("Looking at the hardware.")
        color: Theme.textMuted
    }
    Txt {
        visible: ds.status !== ""
        text: ds.status
        color: ds.statusError ? Theme.danger : Theme.success
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
    }

    // The hardware.
    Section { title: Tr.t("Graphics hardware") }
    Repeater {
        model: ds.gpus
        delegate: Rectangle {
            required property var modelData
            Layout.fillWidth: true
            implicitHeight: gcol.implicitHeight + Theme.s3 * 2
            radius: Theme.radiusMd; color: Theme.bg; border.width: 1; border.color: Theme.border
            Column {
                id: gcol
                anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top
                anchors.margins: Theme.s3
                spacing: Theme.s1
                Txt { text: modelData.name; font.weight: Font.DemiBold; width: parent.width; wrapMode: Text.Wrap; elide: Text.ElideNone }
                Txt {
                    width: parent.width; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted; role: "small"
                    text: modelData.vendor_id + ":" + modelData.device_id + "  " + (modelData.boot_vga ? Tr.t("shows the boot screen") + "  " : "") + ds.supportText(modelData)
                }
            }
        }
    }
    Txt {
        visible: ds.info !== null && ds.gpus.length === 0
        text: Tr.t("No display controller was found.")
        color: Theme.textMuted
    }

    // Nothing to do.
    Txt {
        visible: ds.rec !== null && ds.rec.action === "none"
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
        text: Tr.t("No NVIDIA GPU: the drivers in use are the right ones.")
    }

    // Maxwell, Pascal, Volta (for example a GTX 1070): no package yet.
    ColumnLayout {
        visible: ds.rec !== null && ds.rec.action === "guided"
        Layout.fillWidth: true
        spacing: Theme.s2
        Txt {
            Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
            text: ds.rec ? Tr.t("%1 needs NVIDIA's 580 legacy driver, made for Maxwell, Pascal and Volta GPUs. Basalt OS does not package it: the system assistant will guide that install step by step in a later version. Until then nouveau drives this GPU.").arg(ds.rec.gpu) : ""
        }
        Btn { text: Tr.t("Read more"); icon: "info"; variant: "outline"; focusable: true; e2e: "drivers-docs"; onClicked: Qt.openUrlExternally(ds.info.docs) }
    }
    Txt {
        visible: ds.rec !== null && (ds.rec.action === "unsupported" || ds.rec.action === "unknown")
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
        text: ds.rec && ds.rec.action === "unsupported"
              ? Tr.t("This NVIDIA GPU is supported only by an old NVIDIA driver that no longer gets updates. nouveau drives it.")
              : Tr.t("This NVIDIA GPU is not in the list of the NVIDIA driver Basalt OS ships; it may be newer than that driver. Nothing is recommended.")
    }

    // The driver fell back to nouveau: why, and the way back.
    Rectangle {
        visible: ds.rec !== null && ds.rec.action === "fallback"
        Layout.fillWidth: true
        implicitHeight: fcol.implicitHeight + Theme.s3 * 2
        radius: Theme.radiusMd; color: Theme.bg; border.width: 1; border.color: Theme.danger
        ColumnLayout {
            id: fcol
            anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top
            anchors.margins: Theme.s3
            spacing: Theme.s2
            Row {
                spacing: Theme.s2
                Icon { name: "warning"; color: Theme.danger; size: Theme.fontSize * 1.4 }
                Txt { text: Tr.t("The NVIDIA driver did not start"); font.weight: Font.DemiBold }
            }
            Txt {
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
                text: ds.st ? Tr.t("Reason: %1. The computer uses nouveau now, so the screen keeps working.").arg(ds.st.reason) : ""
            }
            Txt {
                visible: ds.st !== null && ds.st.snapshot !== undefined && ds.st.snapshot !== ""
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
                text: ds.st ? Tr.t("Snapshot %1 holds the system as it was before the install. Rolling back returns to it at the next start; your files in your home folder stay as they are.").arg(ds.st.snapshot) : ""
            }
            Flow {
                Layout.fillWidth: true
                spacing: Theme.s2
                Btn {
                    text: Tr.t("Roll back to the snapshot"); variant: "primary"; focusable: true; e2e: "drivers-rollback"
                    enabled: !ds.busy && ds.proposal === null
                    onClicked: ds.propose("drivers.rollback")
                }
            }
            Txt {
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted; role: "small"
                text: Tr.t("To try the NVIDIA driver again instead, run sudo basalt-nvidia retry in a terminal and restart.")
            }
        }
    }

    // Installed: its state.
    Txt {
        visible: ds.rec !== null && ds.rec.action === "installed"
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
        text: !ds.st ? "" : (ds.st.mode === "trial"
              ? Tr.t("The NVIDIA driver %1 is installed. Restart the computer: the first start checks it and goes back to nouveau if it fails.").arg(ds.st.version)
              : Tr.t("The NVIDIA driver %1 is in use.").arg(ds.st.version))
    }
    Txt {
        visible: ds.st !== null && ds.st.this_boot === "nouveau" && ds.rec !== null && ds.rec.action === "installed"
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.warning
        text: ds.st ? Tr.t("This start uses nouveau: %1.").arg(ds.st.this_boot_reason) : ""
    }
    Txt {
        visible: ds.st !== null && ds.st.held_kernel !== undefined && ds.st.held_kernel !== ""
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
        text: ds.st ? Tr.t("Kernel %1 waits for its signed NVIDIA module; the computer keeps starting the previous kernel until it is published.").arg(ds.st.held_kernel) : ""
    }

    // Install: what changes, the license, one button.
    ColumnLayout {
        visible: ds.rec !== null && ds.rec.action === "install" && !ds.installed
        Layout.fillWidth: true
        spacing: Theme.s3

        Section { title: ds.rec ? Tr.t("Recommended: NVIDIA driver %1 (proprietary)").arg(ds.rec.version) : "" }
        Rectangle {
            visible: ds.rec !== null && ds.rec.hybrid && ds.rec.variant === "display"
            Layout.fillWidth: true
            implicitHeight: hyb.implicitHeight + Theme.s3 * 2
            radius: Theme.radiusMd; color: Theme.bg; border.width: 1; border.color: Theme.border
            Txt {
                id: hyb
                anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top; anchors.margins: Theme.s3
                wrapMode: Text.Wrap; elide: Text.ElideNone
                text: ds.rec ? Tr.t("Laptop with two GPUs: %1 stays the display GPU and saves power. Programs run on %2 when they ask for it (PRIME offload, basalt-nvidia-run PROGRAM), and CUDA works on it.").arg(ds.rec.primary).arg(ds.rec.gpu) : ""
            }
        }
        Txt { text: Tr.t("What changes"); font.weight: Font.DemiBold }
        Repeater {
            model: ds.rec ? [
                Tr.t("The basalt-nonfree repository is turned on. Its packages are signed with the OpenBasalt release key, like the other Basalt repositories."),
                Tr.t("Installed: %1. NVIDIA's software comes unmodified; the open kernel modules are built and signed by Basalt OS for each kernel, so Secure Boot stays on.").arg((ds.rec.packages || []).join(", ")),
                ds.rec.variant === "compute" ? Tr.t("This system starts without a desktop: only the compute part is installed (CUDA, NVML, OpenCL, nvidia-smi), no display packages.")
                                             : Tr.t("nouveau, the open driver in use now, is turned off from the next start."),
                Tr.t("A snapshot is taken first. The next start checks the driver; if it fails, the start after it uses nouveau again and you can return to the snapshot."),
                Tr.t("Kernel updates wait until their signed NVIDIA module is published, so the computer never starts a kernel without it."),
                Tr.t("A restart is needed at the end.")
            ] : []
            delegate: Row {
                required property var modelData
                Layout.fillWidth: true
                spacing: Theme.s2
                Icon { name: "check"; color: Theme.textMuted; size: Theme.fontSize * 1.2 }
                Txt { text: modelData; width: ds.width - Theme.s6 * 2; wrapMode: Text.Wrap; elide: Text.ElideNone }
            }
        }

        // Secure Boot without the Basalt module CA: enroll it first.
        Txt {
            visible: ds.caBlocked
            Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.warning
            text: Tr.t("Secure Boot is on and the Basalt kernel module CA is not enrolled yet, so the kernel would refuse the NVIDIA module. Enroll it first: run sudo basalt-secureboot enroll-mok in a terminal and confirm at the next start (one time only).")
        }
        Txt {
            visible: ds.sb !== null && ds.sb.ca_pending
            Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
            text: Tr.t("The Basalt kernel module CA waits for its confirmation at the next start (blue MokManager screen); the NVIDIA module loads after it.")
        }
        Txt {
            visible: ds.geforce
            Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted; role: "small"
            text: Tr.t("GeForce and Titan software is not licensed for datacenter deployment (NVIDIA Driver License Agreement, section 2.8).")
        }

        // The license, before the button.
        Txt { text: Tr.t("NVIDIA Driver License Agreement"); font.weight: Font.DemiBold }
        Txt {
            Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted; role: "small"
            text: Tr.t("The NVIDIA driver is proprietary software. You may use it with NVIDIA GPUs; it may not be modified, and it comes with this license, in English. Read it before you accept.")
        }
        Btn {
            text: ds.showLicense ? Tr.t("Hide the license") : Tr.t("Read the license")
            icon: "list"; variant: "outline"; focusable: true; e2e: "drivers-license-show"
            onClicked: ds.showLicense = !ds.showLicense
        }
        Rectangle {
            visible: ds.showLicense
            Layout.fillWidth: true
            Layout.preferredHeight: 280
            radius: Theme.radiusMd; color: Theme.bg; border.width: 1; border.color: Theme.border
            clip: true
            Flickable {
                id: lic
                anchors.fill: parent; anchors.margins: Theme.s3
                contentHeight: licText.implicitHeight
                clip: true
                Txt {
                    id: licText
                    width: lic.width
                    role: "small"; wrapMode: Text.Wrap; elide: Text.ElideNone
                    text: ds.info && ds.info.license ? ds.info.license.text : ""
                }
            }
        }
        Btn {
            text: Tr.t("I accept the NVIDIA Driver License Agreement")
            icon: ds.accepted ? "check" : ""; variant: "outline"; active: ds.accepted; focusable: true; e2e: "drivers-license-accept"
            onClicked: ds.accepted = !ds.accepted
        }
        Flow {
            Layout.fillWidth: true
            spacing: Theme.s2
            Btn {
                text: Tr.t("Install the NVIDIA driver"); variant: "primary"; focusable: true; e2e: "drivers-install"
                enabled: ds.accepted && !ds.caBlocked && !ds.busy && ds.proposal === null
                opacity: enabled ? 1 : 0.5
                onClicked: ds.propose("drivers.propose")
            }
        }
    }

    // The stored proposal: exactly what will run, then confirm.
    Rectangle {
        visible: ds.proposal !== null
        Layout.fillWidth: true
        implicitHeight: pcol.implicitHeight + Theme.s3 * 2
        radius: Theme.radiusMd; color: Theme.bg; border.width: 1; border.color: Theme.accent
        ColumnLayout {
            id: pcol
            anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top
            anchors.margins: Theme.s3
            spacing: Theme.s2
            Txt { text: Tr.t("Confirm: the system assistant's proposal"); font.weight: Font.DemiBold }
            Txt {
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "mono"
                text: ds.proposal ? ds.proposal.report : ""
            }
            Flow {
                Layout.fillWidth: true
                spacing: Theme.s2
                Btn { text: Tr.t("Confirm and apply"); icon: "check"; variant: "primary"; focusable: true; e2e: "drivers-confirm"; enabled: !ds.busy; onClicked: ds.decide(true) }
                Btn { text: Tr.t("Cancel"); variant: "outline"; focusable: true; e2e: "drivers-cancel"; enabled: !ds.busy; onClicked: ds.decide(false) }
            }
        }
    }

    // After an apply: the assistant's output and the restart.
    Txt {
        visible: ds.result !== ""
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "mono"; color: Theme.textMuted
        text: ds.result
    }
    Btn {
        visible: ds.installed || (ds.st !== null && ds.st.mode === "trial")
        text: Tr.t("Restart now"); icon: "power"; variant: "outline"; focusable: true; e2e: "drivers-restart"
        onClicked: Quickshell.execDetached(["systemctl", "reboot"])
    }

    Txt {
        visible: ds.info !== null
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted; role: "small"
        text: Tr.t("Packaged by OpenBasalt, not supported by NVIDIA. NVIDIA, GeForce and CUDA are trademarks of NVIDIA Corporation.")
    }

    component Section: Txt {
        property string title
        text: title
        role: "large"
        font.weight: Font.DemiBold
        topPadding: Theme.s2
    }
}
