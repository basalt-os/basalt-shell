pragma Singleton

import QtQuick
import Quickshell

// Translations of the shell UI (ADR 0014). The daemon sends the catalog
// of the session's language with its state (the same catalogs as the Go
// side, /usr/share/basalt-shell/locale/<lang>.json); English is the
// reference text and the message id. Placeholders are %1, %2 (fill them
// with .arg()). The UI follows the session's language; the voice and the
// assistant's answers follow the person's own language settings.
Singleton {
    readonly property var cat: Bus.uiCatalog || ({})

    // t translates a whole sentence.
    function t(s) {
        const v = cat[s];
        return v && v.length > 0 && v[0] ? v[0] : s;
    }
    // n translates a sentence with a count (%1 is the count): English and
    // Portuguese have two forms, one and other.
    function n(one, many, count) {
        const v = cat[one];
        let f = count === 1 ? one : many;
        if (v && v.length > 0) {
            const i = count === 1 ? 0 : 1;
            if (i < v.length && v[i]) f = v[i];
        }
        return f.arg(count);
    }
}
