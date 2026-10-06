import QtQuick

// A model download on a card: the consent offer (Download, Not now) with
// what, how big and from where, or the download the person agreed to,
// with its progress, a wait for the network, or why it failed. Used by
// the voice card (push to talk without a speech model) and the model card
// (a skill that needs the assistant's local model). Nothing is downloaded
// before the person chooses Download; the daemon accepts that choice only
// from this UI (models.download).
Column {
    id: dv
    // Either an offer or a job (Bus.models).
    property var offer: null
    property var job: null
    // The speech language's own name, for a voice offer.
    property string langName: ""
    readonly property string kind: offer ? offer.kind : (job ? job.kind : "")
    readonly property string st: job ? job.state : ""
    spacing: Theme.s2

    function mb(bytes) { return Math.max(1, Math.round((bytes || 0) / 1000000)); }
    function gb(bytes) { return (Math.round((bytes || 0) / 100000000) / 10).toFixed(1); }

    function title() {
        if (offer) {
            if (offer.ask === "")
                return kind === "voice"
                    ? Tr.t("Voice needs the speech model for %1 (%2 MB). Model downloads are turned off on this computer: ask your administrator.").arg(langName).arg(mb(offer.bytes))
                    : Tr.t("The assistant's local model (%1 GB) is not on this computer, and model downloads are turned off: ask your administrator.").arg(gb(offer.bytes));
            return kind === "voice"
                ? Tr.t("Voice needs to download the speech model for %1 (%2 MB) from %3. Download now?").arg(langName).arg(mb(offer.bytes)).arg(offer.host)
                : Tr.t("Download the assistant's local model (%1 GB) from %2? It runs on this computer.").arg(gb(offer.bytes)).arg(offer.host);
        }
        if (!job) return "";
        switch (st) {
        case "authorizing": return Tr.t("Waiting for the approval to download");
        case "queued": return Tr.t("The download is starting");
        case "downloading":
            return kind === "voice"
                ? Tr.t("Downloading the speech model: %1 of %2 MB").arg(mb(job.bytes)).arg(mb(job.total))
                : Tr.t("Downloading the assistant's local model: %1 of %2 MB").arg(mb(job.bytes)).arg(mb(job.total));
        case "verifying": return Tr.t("Checking that the download is intact");
        case "enabling": return Tr.t("Turning the assistant's local model on");
        case "waiting": return Tr.t("No internet connection. The download starts again by itself when the computer is back online.");
        case "done": return kind === "voice" ? Tr.t("Voice is ready. Hold Super+V and speak.") : Tr.t("The assistant's local model is ready.");
        case "cancelled": return Tr.t("The download was cancelled.");
        }
        switch (job.error) {
        case "checksum": return Tr.t("The downloaded file arrived damaged or altered, so it was deleted. Nothing was installed.");
        case "refused": return Tr.t("The download was not approved.");
        case "policy": return Tr.t("The administrator turned model downloads off on this computer.");
        case "admin": return Tr.t("An administrator must approve model downloads on this computer.");
        case "missing": return Tr.t("The download tools are not installed on this computer.");
        case "storage": return Tr.t("The model folder cannot be written. Ask your administrator.");
        }
        return Tr.t("The download failed. Try again later.");
    }
    function detail() {
        if (offer) {
            if (offer.ask === "") return "";
            let s = kind === "voice"
                ? Tr.t("Speech is turned into text on this computer; your voice never leaves it. The file is checked before it is used: a damaged or altered download is never installed.")
                : Tr.t("It writes the summaries and understands requests in your own words. What you ask stays on this computer. The file is checked before it is used: a damaged or altered download is never installed.");
            if (offer.ask === "admin") s += " " + Tr.t("An administrator's password is needed for downloads on this computer.");
            return s;
        }
        if (st === "downloading" || st === "queued" || st === "verifying" || st === "authorizing")
            return Tr.t("You can keep working. This card says when it is ready.");
        return "";
    }

    Txt {
        width: parent.width
        wrapMode: Text.Wrap
        font.weight: Font.DemiBold
        text: dv.title()
        color: dv.job && dv.st === "failed" ? Theme.danger : Theme.text
    }
    // Progress.
    Rectangle {
        visible: dv.job !== null && (dv.st === "downloading" || dv.st === "verifying" || dv.st === "enabling")
        width: parent.width
        height: Theme.s1 * 1.5
        radius: height / 2
        color: Theme.surfaceAlt
        Rectangle {
            readonly property real frac: dv.job && dv.job.total > 0 ? Math.min(1, dv.job.bytes / dv.job.total) : 0
            width: parent.width * (dv.st === "downloading" ? frac : 1)
            height: parent.height
            radius: parent.radius
            color: Theme.accent
            Behavior on width { NumberAnimation { duration: Theme.normal } }
        }
    }
    Txt {
        visible: text !== ""
        width: parent.width
        wrapMode: Text.Wrap
        role: "small"
        color: Theme.textMuted
        text: dv.detail()
    }
    Row {
        spacing: Theme.s2
        // The offer.
        Btn {
            visible: dv.offer !== null && dv.offer.ask !== ""
            text: Tr.t("Download"); icon: "download"; variant: "primary"; focusable: true; e2e: "model-download"
            onClicked: Bus.modelsDownload(dv.offer.id)
        }
        Btn {
            visible: dv.offer !== null && dv.offer.ask !== ""
            text: Tr.t("Not now"); variant: "outline"; focusable: true; e2e: "model-not-now"
            onClicked: Bus.modelsDismiss(dv.offer.id)
        }
        Btn {
            visible: dv.offer !== null && dv.offer.ask === ""
            text: Tr.t("Close"); variant: "outline"; focusable: true; e2e: "model-offer-close"
            onClicked: Bus.modelsDismiss(dv.offer.id)
        }
        // A download waiting for the network, or one that failed.
        Btn {
            visible: dv.job !== null && (dv.st === "waiting" || dv.st === "failed")
            text: dv.st === "waiting" ? Tr.t("Retry now") : Tr.t("Try again"); variant: "primary"; focusable: true; e2e: "model-retry"
            onClicked: Bus.modelsRetry(dv.job.id)
        }
        Btn {
            visible: dv.job !== null && (dv.st === "waiting" || dv.st === "failed" || dv.st === "cancelled")
            text: dv.st === "waiting" ? Tr.t("Cancel") : Tr.t("Close"); variant: "outline"; focusable: true; e2e: "model-close"
            onClicked: Bus.modelsCancel(dv.job.id)
        }
    }
}
