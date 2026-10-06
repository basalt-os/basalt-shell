pragma Singleton

import QtQuick
import Quickshell
import Quickshell.Io
import "logic.js" as L

// Translations of the login screen (ADR 0014). English is the reference
// text and the message id; catalogs are JSON files like the shell's
// (/usr/share/basalt-greeter/locale/<lang>.json, {"English": ["Português"]}).
// The language is the one the person picked on this screen (remembered),
// else the system's (/etc/locale.conf), else English.
Singleton {
    id: i18n

    readonly property string dataDir: Quickshell.env("BASALT_GREETER_DATA") || "/usr/share/basalt-greeter"
    // Languages with a catalog, and their names in their own language.
    readonly property var languages: [
        { id: "en", name: "English" },
        { id: "pt_BR", name: "Português (Brasil)" }
    ]
    property string chosen: ""
    readonly property string systemLang: L.parseKeyValue(Sys.readFile(Sys.root + "/etc/locale.conf")).LANG || Quickshell.env("LANG") || ""
    readonly property string lang: L.uiLanguage(chosen !== "" ? chosen : systemLang, languages.map(l => l.id))
    // Qt's locale for dates and times: the language's usual country.
    readonly property var locale: Qt.locale(lang === "en" ? "en_US" : lang)
    readonly property string languageName: (languages.find(l => l.id === lang) || languages[0]).name

    property var cat: ({})
    function loadCatalog() {
        if (lang === "en") { cat = {}; return; }
        let c = {};
        try { c = JSON.parse(catFile.text()) || {}; } catch (e) { c = {}; }
        cat = c;
    }
    onLangChanged: loadCatalog()
    Component.onCompleted: loadCatalog()
    FileView {
        id: catFile
        path: i18n.dataDir + "/locale/" + (i18n.lang === "en" ? "pt_BR" : i18n.lang) + ".json"
        blockLoading: true
        printErrors: false
        onLoaded: i18n.loadCatalog()
    }

    // t translates a whole sentence; placeholders are %1, %2 (fill them
    // with .arg()).
    function t(s) {
        const v = cat[s];
        return v && v.length > 0 && v[0] ? v[0] : s;
    }
}
