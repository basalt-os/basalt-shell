import QtQuick
import QtQuick.Layouts

// Settings, Voice and assistant: the person's own speech language and
// model, answer language, spoken answers and voice, and language model,
// within what the administrator allows. These settings are the person's
// only and independent of the desktop's language. Every change is sent
// to the daemon (voice.settings.set, shell UI only), which checks it
// against the policy and writes ~/.config/basalt/voice-and-assistant.conf;
// a refused change keeps the old value and says why. Everything here is
// plain buttons in wrapping rows: usable with the keyboard (Tab, Return,
// Space) and at narrow widths.
ColumnLayout {
    id: vs
    spacing: Theme.s4

    property var info: null
    readonly property var prefs: info ? info.prefs : null
    readonly property var eff: info ? info.effective : null
    readonly property var models: info ? info.models : null
    property string status: ""
    property bool statusError: false

    function load() {
        Bus.call("voice.settings", {}, (ok, res) => {
            if (ok) vs.info = res;
            else { vs.status = res; vs.statusError = true; }
        });
    }
    // save sends the whole settings with one change.
    function save(change, note) {
        if (!vs.prefs) return;
        Bus.saveVoiceSettings(vs.prefs, change, (ok, res) => {
            if (ok) { vs.info = res; vs.status = note || Tr.t("Saved. It applies to your next request."); vs.statusError = false; }
            else { vs.status = res; vs.statusError = true; }
        });
    }
    // A language other than English needs a multilingual model: when the
    // model in use understands English only, the same save switches to an
    // installed multilingual model the administrator allows: the base
    // quantized one first (fast, and it heard short requests best in the
    // lab), then the small quantized one (better for long dictation).
    function chooseLanguage(tag) {
        const change = { speech_language: tag };
        const english = tag === "" || tag.split("-")[0] === "en";
        if (!english && vs.speechModelInfo && !vs.speechModelInfo.multilingual) {
            // The daemon picks it (multilingualPick in internal/shell).
            const pick = (vs.models.stt || []).find(m => m.name === vs.info.multilingual_pick && m.multilingual && m.allowed);
            if (pick) {
                change.speech_model = pick.name;
                save(change, Tr.t("Saved. The multilingual speech model %1 is used for %2.").arg(pick.name).arg(tag === "auto" ? Tr.t("Automatic") : vs.langName(tag)));
                return;
            }
        }
        save(change);
    }
    function langName(tag) {
        const l = (vs.info ? vs.info.languages : []).find(x => x.tag === tag);
        return l ? l.native : tag;
    }
    function base(tag) { return (tag || "").split("-")[0]; }
    readonly property string speechModel: prefs && prefs.speech_model ? prefs.speech_model : (models ? models.default_stt : "")
    readonly property var speechModelInfo: models ? (models.stt || []).find(m => m.name === vs.speechModel) : null
    readonly property var answerVoices: models && eff ? (models.voices || []).filter(v => vs.base(v.lang) === vs.base(eff.answer_language)) : []

    onVisibleChanged: if (visible) load()
    Connections {
        target: Bus
        function onVoiceSettings(data) { vs.info = data; }
    }

    Txt { text: Tr.t("Voice and assistant"); role: "display" }
    Txt {
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
        text: Tr.t("These settings are yours only. They do not change the language of the desktop: you can speak to the assistant and get answers in another language.")
    }
    Repeater {
        model: vs.info ? (vs.info.problems || []) : []
        delegate: Txt { required property var modelData; text: modelData; color: Theme.warning; Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone }
    }
    Txt {
        visible: vs.status !== ""
        text: vs.status
        color: vs.statusError ? Theme.danger : Theme.success
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
    }

    // Speech language.
    Section { title: Tr.t("Speech language") }
    Txt {
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted; role: "small"
        text: Tr.t("The language you speak to the assistant. Automatic detection needs a multilingual model and is slower and less reliable on short requests.")
    }
    Flow {
        Layout.fillWidth: true
        spacing: Theme.s2
        Btn {
            text: Tr.t("System default"); variant: "outline"; focusable: true; e2e: "voice-lang-default"
            active: vs.prefs !== null && !vs.prefs.speech_language
            onClicked: vs.save({ speech_language: "" })
        }
        Btn {
            text: Tr.t("Automatic"); variant: "outline"; focusable: true; e2e: "voice-lang-auto"
            active: vs.prefs !== null && vs.prefs.speech_language === "auto"
            onClicked: vs.chooseLanguage("auto")
        }
        Repeater {
            model: vs.info ? vs.info.languages : []
            delegate: Btn {
                required property var modelData
                text: modelData.native; variant: "outline"; focusable: true; e2e: "voice-lang-" + modelData.tag
                active: vs.prefs !== null && vs.prefs.speech_language === modelData.tag
                onClicked: vs.chooseLanguage(modelData.tag)
            }
        }
    }

    // Speech model.
    Section { title: Tr.t("Speech model") }
    Txt {
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted; role: "small"
        text: Tr.t("Turns your speech into text, on this computer. Larger models understand better and take longer. English-only models do not understand other languages.")
    }
    Txt {
        visible: !!vs.speechModelInfo && !vs.speechModelInfo.multilingual && vs.eff !== null && vs.eff.speech_language !== "auto" && vs.base(vs.eff.speech_language) !== "en"
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.warning
        text: Tr.t("The model in use understands English only. Choose a multilingual model for %1.").arg(vs.eff ? vs.langName(vs.eff.speech_language) : "")
    }
    Btn {
        text: Tr.t("System default (%1)").arg(vs.models ? vs.models.default_stt : ""); variant: "outline"; focusable: true; e2e: "voice-model-default"
        active: vs.prefs !== null && !vs.prefs.speech_model
        onClicked: vs.save({ speech_model: "" })
    }
    Repeater {
        model: vs.models ? (vs.models.stt || []) : []
        delegate: RowLayout {
            required property var modelData
            Layout.fillWidth: true
            spacing: Theme.s3
            Btn {
                text: modelData.name; variant: "outline"; focusable: modelData.allowed; e2e: "voice-model-" + modelData.name
                opacity: modelData.allowed ? 1 : 0.5
                active: vs.prefs !== null && vs.prefs.speech_model === modelData.name
                onClicked: if (modelData.allowed) vs.save({ speech_model: modelData.name })
            }
            Txt {
                Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "small"; color: Theme.textMuted
                text: (modelData.multilingual ? Tr.t("%1 MB, multilingual").arg(modelData.size_mb) : Tr.t("%1 MB, English only").arg(modelData.size_mb)) +
                      (modelData.allowed ? "" : " " + Tr.t("(not allowed by your administrator)"))
            }
        }
    }
    Txt {
        visible: vs.models !== null && (vs.models.stt || []).length === 0
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
        text: Tr.t("No speech model is installed. An administrator installs them with basalt-voice-fetch.")
    }

    // Answer language.
    Section { title: Tr.t("Answer language") }
    Txt {
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted; role: "small"
        text: vs.eff ? Tr.t("The assistant answers in %1.").arg(vs.langName(vs.eff.answer_language)) : ""
    }
    Flow {
        Layout.fillWidth: true
        spacing: Theme.s2
        Btn {
            text: Tr.t("Same as speech"); variant: "outline"; focusable: true; e2e: "answer-lang-same"
            active: vs.prefs !== null && !vs.prefs.answer_language
            onClicked: vs.save({ answer_language: "" })
        }
        Repeater {
            model: vs.info ? vs.info.languages : []
            delegate: Btn {
                required property var modelData
                text: modelData.native; variant: "outline"; focusable: true; e2e: "answer-lang-" + modelData.tag
                active: vs.prefs !== null && vs.prefs.answer_language === modelData.tag
                onClicked: vs.save({ answer_language: modelData.tag })
            }
        }
    }

    // Spoken answers and the voice.
    Section { title: Tr.t("Spoken answers") }
    Row {
        spacing: Theme.s2
        Btn { text: Tr.t("On"); variant: "outline"; focusable: true; e2e: "spoken-on"; active: vs.prefs !== null && vs.prefs.spoken !== "no"; onClicked: vs.save({ spoken: "yes" }) }
        Btn { text: Tr.t("Off"); variant: "outline"; focusable: true; e2e: "spoken-off"; active: vs.prefs !== null && vs.prefs.spoken === "no"; onClicked: vs.save({ spoken: "no" }) }
    }
    Txt {
        visible: vs.eff !== null && vs.answerVoices.length === 0
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
        text: vs.eff ? Tr.t("No voice for %1 is installed: answers in it are shown, not spoken.").arg(vs.langName(vs.eff.answer_language)) : ""
    }
    Flow {
        visible: vs.answerVoices.length > 0
        Layout.fillWidth: true
        spacing: Theme.s2
        Repeater {
            model: vs.answerVoices
            delegate: Btn {
                required property var modelData
                text: modelData.name; variant: "outline"; focusable: modelData.allowed; e2e: "voice-" + modelData.name
                opacity: modelData.allowed ? 1 : 0.5
                active: vs.info !== null && vs.info.voice === modelData.name
                onClicked: {
                    if (!modelData.allowed) return;
                    const v = {}; v[vs.base(vs.eff.answer_language)] = modelData.name;
                    vs.save({ voices: v });
                }
            }
        }
    }

    // Language model.
    Section { title: Tr.t("Language model") }
    Txt {
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted; role: "small"
        text: Tr.t("The model that understands your requests and writes the summaries. Your administrator decides which ones you may choose.")
    }
    Btn {
        text: vs.info && vs.info.local ? Tr.t("Local model on this computer") : Tr.t("Local model (none configured: fixed phrases only)")
        variant: "outline"; focusable: true; e2e: "model-local"
        active: vs.eff !== null && vs.eff.model === "local"
        onClicked: vs.save({ model: "local" })
    }
    Repeater {
        model: vs.info && vs.info.policy ? (vs.info.policy.remotes || []) : []
        delegate: Btn {
            required property var modelData
            readonly property bool usable: vs.info.policy.allow_remote && vs.prefs && vs.prefs.allow_remote
            text: Tr.t("Remote: %1").arg(modelData.label); variant: "outline"; focusable: usable; e2e: "model-" + modelData.name
            opacity: usable ? 1 : 0.5
            active: vs.eff !== null && vs.eff.model === modelData.name
            onClicked: if (usable) vs.save({ model: modelData.name })
        }
    }
    Txt {
        visible: vs.info !== null && vs.info.policy && !vs.info.policy.allow_remote
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted; role: "small"
        text: Tr.t("Your administrator does not allow remote models on this computer.")
    }
    Row {
        visible: vs.info !== null && vs.info.policy && vs.info.policy.allow_remote && (vs.info.policy.remotes || []).length > 0
        spacing: Theme.s2
        Btn {
            text: vs.prefs && vs.prefs.allow_remote ? Tr.t("Allow a remote model: on") : Tr.t("Allow a remote model: off")
            variant: "outline"; focusable: true; e2e: "model-allow-remote"
            active: vs.prefs !== null && vs.prefs.allow_remote
            onClicked: vs.save(vs.prefs.allow_remote ? { allow_remote: false, model: "local" } : { allow_remote: true })
        }
    }
    Txt {
        visible: vs.info !== null && vs.info.policy && vs.info.policy.allow_remote
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted; role: "small"
        text: Tr.t("A remote model receives your requests and the text the assistant reads for you (files, messages, pages). Each answer says which model wrote it.")
    }
    Txt {
        visible: vs.info !== null
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted; role: "small"
        text: vs.info ? Tr.t("Saved in %1.").arg(vs.info.path) : ""
    }

    component Section: Txt {
        property string title
        text: title
        role: "large"
        font.weight: Font.DemiBold
        topPadding: Theme.s2
    }
}
