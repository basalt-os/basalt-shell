import QtQuick
import QtQuick.Layouts

// Settings, Keyboard: the person's layouts (the first is the default),
// the key that switches between them, Caps Lock, the compose key and key
// repeat, a field to try them, and the system's keyboard for the login
// screen and new accounts. Every change goes to the daemon
// (keyboard.set, shell UI only), which checks it against the system's XKB
// registry, writes ~/.config/basalt/keyboard.conf and applies it to the
// running session at once. The system's keyboard is a proposal of the
// system assistant (keyboard.system), shown on a sheet and applied like
// every other system change (the approval gate, or an administrator's
// password): the page never runs localectl.
ColumnLayout {
    id: kb
    spacing: Theme.s4

    property var info: null
    readonly property var settings: info ? info.settings : null
    readonly property var effective: info ? (info.effective || []) : []
    readonly property bool own: info !== null && info.own === true
    readonly property var limits: info ? info.limits : ({ layouts: 4, delay_min: 150, delay_max: 1000, rate_min: 10, rate_max: 80, delay_default: 600, rate_default: 25 })
    property var entries: []           // keyboard.layouts: the picker's list
    property bool picking: false
    property string query: ""
    property string status: ""
    property bool statusError: false
    property bool busy: false
    property var proposal: null        // the stored keyboard.system proposal
    property bool showDetails: false
    property bool applying: false
    // What happened to the system's keyboard, shown under its button.
    property string sysStatus: ""
    property bool sysError: false
    // The login screen already types with the person's layouts and options.
    readonly property bool sameAsSystem: info !== null && info.same_as_system === true
        && JSON.stringify(info.options || []) === JSON.stringify(info.system ? (info.system.options || []) : [])

    function load() {
        Bus.call("keyboard.state", {}, (ok, res) => {
            if (ok) kb.info = res; else { kb.status = String(res); kb.statusError = true; }
        });
    }
    function loadEntries() {
        if (kb.entries.length > 0) return;
        Bus.call("keyboard.layouts", {}, (ok, res) => { if (ok && Array.isArray(res)) kb.entries = res; });
    }
    // save sends the whole settings with one change.
    function save(change, note, after) {
        if (!kb.settings) return;
        const s = Object.assign({}, kb.settings, change);
        kb.busy = true;
        Bus.call("keyboard.set", s, (ok, res) => {
            kb.busy = false;
            if (ok) { kb.info = res; kb.status = note || Tr.t("Saved. Your keyboard changed at once."); kb.statusError = false; }
            else { kb.status = String(res); kb.statusError = true; }
            if (after) Qt.callLater(after);
        });
    }
    // The layouts as a plain list ({layout, variant}).
    function plain(list) { return list.map(c => ({ layout: c.layout, variant: c.variant || "" })); }
    function setLayouts(list, note, focusKey) {
        kb.save({ layouts: list }, note, () => { if (focusKey) kb.focusOn(focusKey); });
    }
    function closePicker() {
        kb.picking = false;
        kb.query = "";
        search.text = "";
    }
    function focusOn(key) {
        const it = Nav.find(kb, key) || Nav.find(kb, "keyboard-add");
        if (it) it.forceActiveFocus(Qt.TabFocusReason);
    }
    function move(i, d) {
        const l = kb.plain(kb.effective);
        const j = i + d;
        if (j < 0 || j >= l.length) return;
        const t = l[i]; l[i] = l[j]; l[j] = t;
        // The focus stays on the moved layout's button, or its other
        // arrow when it reached an end.
        const key = d < 0 ? (j === 0 ? "keyboard-down-0" : "keyboard-up-" + j)
                          : (j === l.length - 1 ? "keyboard-up-" + j : "keyboard-down-" + j);
        kb.setLayouts(l, j === 0 ? Tr.t("%1 is now the default layout.").arg(kb.nameOf(l[0])) : "", key);
    }
    function remove(i) {
        const l = kb.plain(kb.effective);
        const gone = l.splice(i, 1)[0];
        kb.setLayouts(l, Tr.t("Removed %1.").arg(kb.nameOf(gone)), "keyboard-remove-" + Math.min(i, l.length - 1));
    }
    function add(e) {
        const l = kb.plain(kb.effective);
        if (l.some(c => c.layout === e.layout && c.variant === (e.variant || ""))) { kb.closePicker(); Qt.callLater(() => kb.focusOn("keyboard-add")); return; }
        if (l.length >= kb.limits.layouts) {
            kb.status = Tr.t("At most %1 layouts can be used at once. Remove one first.").arg(kb.limits.layouts);
            kb.statusError = true;
            return;
        }
        l.push({ layout: e.layout, variant: e.variant || "" });
        kb.closePicker();
        kb.setLayouts(l, Tr.t("Added %1. Switch to it with Super+Shift+Space.").arg(kb.nameOf(e)), "keyboard-add");
    }

    // Names people look for, where the registry's wording differs.
    function friendly(c) {
        const key = c.layout + (c.variant ? "(" + c.variant + ")" : "");
        switch (key) {
        case "br": return Tr.t("Portuguese (Brazil, ABNT2)");
        case "us(intl)": return Tr.t("English (US, international with dead keys)");
        case "us(alt-intl)": return Tr.t("English (US, alternative international)");
        case "us(altgr-intl)": return Tr.t("English (US, international with AltGr dead keys)");
        }
        return "";
    }
    function nameOf(c) {
        const f = kb.friendly(c);
        if (f) return f;
        if (c.name) return c.name;
        const e = kb.entries.find(x => x.layout === c.layout && (x.variant || "") === (c.variant || ""));
        return e ? e.name : c.layout + (c.variant ? " (" + c.variant + ")" : "");
    }
    function fold(s) {
        let t = String(s || "").toLowerCase();
        try { t = t.normalize("NFD").replace(/[̀-ͯ]/g, ""); } catch (e) {}
        return t;
    }
    // The picker's matches: every word of the query in the name, the
    // English name or the code.
    readonly property var matches: {
        if (!kb.picking) return [];
        const words = kb.fold(kb.query).split(/\s+/).filter(w => w !== "");
        const out = [];
        for (let i = 0; i < kb.entries.length && out.length < 60; i++) {
            const e = kb.entries[i];
            const code = e.layout + (e.variant ? "(" + e.variant + ")" : "");
            const hay = kb.fold(kb.friendly(e) + " " + e.name + " " + e.english + " " + code + " " + e.layout + " " + e.variant);
            if (words.every(w => hay.indexOf(w) >= 0)) out.push(e);
        }
        return out;
    }

    function switchText(v) {
        switch (v) {
        case "alt+shift": return Tr.t("Alt+Shift");
        case "ctrl+shift": return Tr.t("Ctrl+Shift");
        case "both-alts": return Tr.t("Both Alt keys");
        default: return Tr.t("Super+Shift+Space only");
        }
    }
    function capsText(v) {
        switch (v) {
        case "ctrl": return Tr.t("Ctrl");
        case "escape": return Tr.t("Escape");
        case "swap-escape": return Tr.t("Swap with Escape");
        case "off": return Tr.t("Off");
        default: return Tr.t("Caps Lock");
        }
    }
    function composeText(v) {
        switch (v) {
        case "right-alt": return Tr.t("Right Alt");
        case "menu": return Tr.t("Menu key");
        case "right-ctrl": return Tr.t("Right Ctrl");
        case "caps-lock": return Tr.t("Caps Lock");
        default: return Tr.t("None");
        }
    }
    readonly property string layoutList: kb.effective.map(c => kb.nameOf(c)).join(", ")
    readonly property string systemList: kb.info && kb.info.system ? kb.info.system.layouts.map(c => kb.nameOf(c)).join(", ") : ""
    // The active layout, when a keyboard reports it.
    readonly property string activeName: {
        const k = Bus.keyboard || {};
        if (!k.live) return "";
        const i = k.current || 0;
        if ((k.names || []).length === kb.effective.length && i < kb.effective.length) return kb.nameOf(kb.effective[i]);
        return (k.names || [])[i] || "";
    }

    // The system's keyboard: store the proposal, then confirm it.
    function proposeSystem() {
        kb.busy = true; kb.sysStatus = ""; kb.sysError = false; kb.showDetails = false;
        Bus.call("keyboard.system", {}, (ok, res) => {
            kb.busy = false;
            if (ok) kb.proposal = res;
            else { kb.sysStatus = String(res); kb.sysError = true; }
        });
    }
    function decide(apply) {
        if (!kb.proposal) return;
        if (!apply) { kb.proposal = null; Qt.callLater(() => kb.focusOn("keyboard-system")); return; }
        kb.busy = true; kb.applying = true; kb.sysError = false;
        kb.sysStatus = "";
        Bus.assistantApply(kb.proposal.id, kb.proposal.code, (ok, res) => {
            kb.busy = false; kb.applying = false;
            const good = ok && res.ok;
            kb.sysError = !good;
            kb.sysStatus = good ? Tr.t("Done. The login screen and new accounts use these layouts from now on.")
                                : Tr.t("Nothing was changed, or not everything worked: %1").arg(ok && res.output ? res.output : String(res));
            kb.proposal = null;
            Bus.refreshAssistant();
            kb.load();
            Qt.callLater(() => kb.focusOn(good ? "keyboard-add" : "keyboard-system"));
        });
    }

    onVisibleChanged: if (visible) { load(); loadEntries(); }

    Txt { text: Tr.t("Keyboard"); role: "display" }
    Txt {
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
        text: Tr.t("Your layouts and how your keys behave. These settings are yours: other people on this computer keep theirs.")
    }
    Repeater {
        model: kb.info ? (kb.info.problems || []).concat(kb.info.registry_error ? [kb.info.registry_error] : []).concat(kb.info.apply_error ? [kb.info.apply_error] : []) : []
        delegate: Txt { required property var modelData; text: modelData; color: Theme.warning; Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone }
    }
    Txt {
        visible: kb.status !== "" && kb.proposal === null
        text: kb.status
        color: kb.statusError ? Theme.danger : Theme.success
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
        Accessible.role: Accessible.AlertMessage
        Accessible.name: kb.status
    }

    // ---- Layouts -------------------------------------------------------
    Section { title: Tr.t("Layouts") }
    Txt {
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
        text: kb.own ? Tr.t("The first one is the default. Super+Shift+Space switches to the next one.")
                     : Tr.t("These are the system's layouts, the same as the login screen. Change them here to make them yours.")
    }
    Repeater {
        model: kb.effective
        delegate: Rectangle {
            id: row
            required property var modelData
            required property int index
            Layout.fillWidth: true
            implicitHeight: rowLay.implicitHeight + Theme.s2 * 2
            radius: Theme.radiusMd; color: Theme.bg; border.width: 1
            border.color: index === 0 ? Theme.accent : Theme.border
            RowLayout {
                id: rowLay
                anchors.left: parent.left; anchors.right: parent.right; anchors.verticalCenter: parent.verticalCenter
                anchors.leftMargin: Theme.s3; anchors.rightMargin: Theme.s2
                spacing: Theme.s3
                Rectangle {
                    implicitWidth: Math.max(Theme.fontSize * 3, shortTxt.implicitWidth + Theme.s3); implicitHeight: Theme.fontSize * 2
                    radius: Theme.radiusSm; color: Theme.alpha(Theme.accent, 0.14)
                    Txt { id: shortTxt; anchors.centerIn: parent; text: row.modelData.short; role: "small"; font.weight: Font.DemiBold; color: Theme.accent }
                }
                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: 0
                    Txt { Layout.fillWidth: true; text: kb.nameOf(row.modelData); font.weight: Font.Medium; elide: Text.ElideRight }
                    Txt {
                        Layout.fillWidth: true; role: "small"; color: Theme.textMuted; elide: Text.ElideRight
                        text: (row.index === 0 ? Tr.t("Default") + "  " : "") + row.modelData.layout + (row.modelData.variant ? "(" + row.modelData.variant + ")" : "")
                    }
                }
                NavRow {
                    spacing: Theme.s1
                    Accessible.name: Tr.t("Change %1").arg(kb.nameOf(row.modelData))
                    Btn {
                        icon: "up"; e2e: "keyboard-up-" + row.index
                        accessibleName: Tr.t("Move %1 up").arg(kb.nameOf(row.modelData))
                        enabled: row.index > 0 && !kb.busy
                        onClicked: kb.move(row.index, -1)
                    }
                    Btn {
                        icon: "chevron"; e2e: "keyboard-down-" + row.index
                        accessibleName: Tr.t("Move %1 down").arg(kb.nameOf(row.modelData))
                        enabled: row.index < kb.effective.length - 1 && !kb.busy
                        onClicked: kb.move(row.index, 1)
                    }
                    Btn {
                        icon: "close"; e2e: "keyboard-remove-" + row.index
                        accessibleName: Tr.t("Remove %1").arg(kb.nameOf(row.modelData))
                        enabled: kb.effective.length > 1 && !kb.busy
                        onClicked: kb.remove(row.index)
                    }
                }
            }
        }
    }
    Flow {
        Layout.fillWidth: true
        spacing: Theme.s2
        Btn {
            visible: !kb.picking
            text: Tr.t("Add a layout"); icon: "plus"; variant: "outline"; e2e: "keyboard-add"
            enabled: kb.effective.length < kb.limits.layouts && !kb.busy
            onClicked: { kb.loadEntries(); kb.picking = true; Qt.callLater(() => search.focusInput()); }
        }
        Btn {
            visible: kb.own && !kb.picking
            text: Tr.t("Use the system's layouts"); icon: "undo"; variant: "ghost"; e2e: "keyboard-reset"
            enabled: !kb.busy
            onClicked: kb.setLayouts([], Tr.t("You use the system's layouts again."), "keyboard-add")
        }
    }

    // The picker: search, then choose (Return takes the first match).
    Rectangle {
        id: picker
        visible: kb.picking
        Layout.fillWidth: true
        implicitHeight: pickCol.implicitHeight + Theme.s3 * 2
        radius: Theme.radiusMd; color: Theme.bg; border.width: 1; border.color: Theme.accent
        // Escape closes the picker (not the page).
        Keys.onEscapePressed: e => { kb.closePicker(); Qt.callLater(() => kb.focusOn("keyboard-add")); e.accepted = true; }
        ColumnLayout {
            id: pickCol
            anchors.left: parent.left; anchors.right: parent.right; anchors.top: parent.top; anchors.margins: Theme.s3
            spacing: Theme.s2
            RowLayout {
                Layout.fillWidth: true
                spacing: Theme.s2
                Field {
                    id: search
                    Layout.fillWidth: true
                    icon: "search"; e2e: "keyboard-search"
                    placeholder: Tr.t("Search layouts, like Portuguese or ABNT2")
                    accessibleName: Tr.t("Search keyboard layouts")
                    onEdited: t => kb.query = t
                    onAccepted: if (kb.matches.length > 0) kb.add(kb.matches[0])
                    onDownPressed: { const it = Nav.find(results, "keyboard-pick-0"); if (it) it.forceActiveFocus(Qt.TabFocusReason); }
                }
                Btn { text: Tr.t("Cancel"); variant: "ghost"; e2e: "keyboard-pick-cancel"; onClicked: { kb.closePicker(); Qt.callLater(() => kb.focusOn("keyboard-add")); } }
            }
            Txt {
                visible: kb.entries.length > 0
                role: "small"; color: Theme.textMuted
                text: kb.matches.length === 0 ? Tr.t("No layout matches.") : Tr.n("%1 layout", "%1 layouts", kb.matches.length)
            }
            NavColumn {
                id: results
                Layout.fillWidth: true
                spacing: 2
                wrap: false
                Accessible.name: Tr.t("Keyboard layouts")
                Repeater {
                    model: kb.matches
                    delegate: Pressable {
                        id: pick
                        required property var modelData
                        required property int index
                        width: results.width
                        height: pickRow.implicitHeight + Theme.s2
                        radius: Theme.radiusSm
                        color: hovered ? Theme.hover : "transparent"
                        e2e: "keyboard-pick-" + index
                        navKey: "keyboard-pick-" + index
                        accessibleName: kb.nameOf(modelData)
                        onClicked: kb.add(modelData)
                        Keys.onUpPressed: e => { if (index === 0) { search.focusInput(); e.accepted = true; } else e.accepted = false; }
                        RowLayout {
                            id: pickRow
                            anchors.left: parent.left; anchors.right: parent.right; anchors.verticalCenter: parent.verticalCenter
                            anchors.leftMargin: Theme.s2; anchors.rightMargin: Theme.s2
                            Txt { Layout.fillWidth: true; text: kb.nameOf(pick.modelData); elide: Text.ElideRight }
                            Txt { text: pick.modelData.layout + (pick.modelData.variant ? "(" + pick.modelData.variant + ")" : ""); role: "small"; color: Theme.textMuted }
                        }
                    }
                }
            }
        }
    }

    // ---- Switching -----------------------------------------------------
    Section { title: Tr.t("Switch layouts") }
    Txt {
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
        text: Tr.t("Super+Shift+Space always switches, and so does the layout shown in the panel. You can add one more key combination:")
    }
    NavFlow {
        Layout.fillWidth: true
        spacing: Theme.s2
        Accessible.name: Tr.t("Switch layouts")
        Repeater {
            model: ["super+shift+space", "alt+shift", "ctrl+shift", "both-alts"]
            delegate: Btn {
                required property var modelData
                text: kb.switchText(modelData); variant: "outline"; checkable: true
                e2e: "keyboard-switch-" + modelData
                active: !!kb.settings && kb.settings.switch === modelData
                enabled: !kb.busy
                onClicked: kb.save({ switch: modelData })
            }
        }
    }

    // ---- Try it --------------------------------------------------------
    Section { title: Tr.t("Try typing") }
    Field {
        id: tryField
        Layout.fillWidth: true
        e2e: "keyboard-try"
        placeholder: Tr.t("Type here to try your layout")
        accessibleName: Tr.t("Try typing")
    }
    Txt {
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "small"; color: Theme.textMuted
        text: kb.activeName !== "" ? Tr.t("Now typing with: %1").arg(kb.activeName) : Tr.t("Your layouts: %1").arg(kb.layoutList)
    }

    // ---- Keys ----------------------------------------------------------
    Section { title: Tr.t("Caps Lock") }
    NavFlow {
        Layout.fillWidth: true
        spacing: Theme.s2
        Accessible.name: Tr.t("Caps Lock")
        Repeater {
            model: ["normal", "ctrl", "escape", "swap-escape", "off"]
            delegate: Btn {
                required property var modelData
                text: kb.capsText(modelData); variant: "outline"; checkable: true
                e2e: "keyboard-caps-" + modelData
                active: !!kb.settings && kb.settings.caps_lock === modelData
                enabled: !kb.busy && !(modelData !== "normal" && kb.settings && kb.settings.compose === "caps-lock")
                onClicked: kb.save({ caps_lock: modelData })
            }
        }
    }
    Section { title: Tr.t("Compose key") }
    Txt {
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
        text: Tr.t("Type accents and symbols in sequence: the compose key, then ' and e, gives é.")
    }
    NavFlow {
        Layout.fillWidth: true
        spacing: Theme.s2
        Accessible.name: Tr.t("Compose key")
        Repeater {
            model: ["none", "right-alt", "menu", "right-ctrl", "caps-lock"]
            delegate: Btn {
                required property var modelData
                text: kb.composeText(modelData); variant: "outline"; checkable: true
                e2e: "keyboard-compose-" + modelData
                active: !!kb.settings && kb.settings.compose === modelData
                enabled: !kb.busy && !(modelData === "caps-lock" && kb.settings && kb.settings.caps_lock !== "normal")
                onClicked: kb.save({ compose: modelData })
            }
        }
    }
    Section { title: Tr.t("Key repeat") }
    RepeatRow {
        e2e: "keyboard-delay"
        label: Tr.t("Delay before repeating")
        from: kb.limits.delay_min; to: kb.limits.delay_max; stepSize: 50
        value: kb.settings && kb.settings.repeat_delay > 0 ? kb.settings.repeat_delay : kb.limits.delay_default
        unit: Tr.t("%1 ms")
        onCommitted: v => kb.save({ repeat_delay: Math.round(v) })
    }
    RepeatRow {
        e2e: "keyboard-rate"
        label: Tr.t("Repeat speed")
        from: kb.limits.rate_min; to: kb.limits.rate_max; stepSize: 5
        value: kb.settings && kb.settings.repeat_rate > 0 ? kb.settings.repeat_rate : kb.limits.rate_default
        unit: Tr.t("%1 per second")
        onCommitted: v => kb.save({ repeat_rate: Math.round(v) })
    }

    // ---- The system's keyboard ----------------------------------------
    Section { title: Tr.t("Login screen and new accounts") }
    Txt {
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
        text: kb.info && kb.info.system && kb.info.system.set
              ? Tr.t("The login screen, the text console and new accounts use the system's layouts: %1.").arg(kb.systemList)
              : Tr.t("No layout was chosen for this computer, so the login screen uses English (US).")
    }
    Btn {
        text: Tr.t("Use my layouts there too"); icon: "shield"; variant: "outline"; e2e: "keyboard-system"
        visible: kb.info !== null && kb.info.assistant === true && !kb.sameAsSystem
        enabled: !kb.busy && kb.proposal === null
        onClicked: kb.proposeSystem()
    }
    Txt {
        visible: kb.sameAsSystem && kb.sysStatus === ""
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "small"; color: Theme.textMuted
        text: Tr.t("The login screen already uses your layouts.")
    }
    Txt {
        visible: kb.sysStatus !== "" && kb.proposal === null
        text: kb.sysStatus
        color: kb.sysError ? Theme.danger : Theme.success
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
        Accessible.role: Accessible.AlertMessage
        Accessible.name: kb.sysStatus
    }
    Txt {
        visible: kb.info !== null && kb.info.assistant !== true
        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "small"; color: Theme.textMuted
        text: Tr.t("Changing the keyboard of the login screen needs the system assistant (basalt).")
    }

    // ---- The stored proposal: a sheet over the window. It opens on Not
    // now (a stray Return never changes the system); Escape is Not now.
    Item {
        id: sheetHolder
        Layout.preferredWidth: 0
        Layout.preferredHeight: 0
        Item {
            id: sheet
            visible: kb.proposal !== null
            parent: kb.Window.window ? kb.Window.window.contentItem : sheetHolder
            anchors.fill: parent
            z: 1000
            focus: visible
            // On Not now, with the ring (rule 7): once the sheet is shown.
            onVisibleChanged: if (visible) Qt.callLater(() => { Ui.focusVisible = true; notNow.forceActiveFocus(Qt.TabFocusReason); })
            Keys.onEscapePressed: if (!kb.busy) kb.decide(false)
            Rectangle { anchors.fill: parent; color: Theme.scrim }
            MouseArea { anchors.fill: parent; hoverEnabled: true }   // the page below takes no input
            Rectangle {
                anchors.centerIn: parent
                width: Math.min(560, parent.width - Theme.s4 * 2)
                height: Math.min(sheetCol.implicitHeight + Theme.s4 * 2, parent.height - Theme.s4 * 2)
                radius: Theme.radiusLg; color: Theme.surface; border.width: 1; border.color: Theme.border
                Accessible.role: Accessible.Dialog
                Accessible.name: Tr.t("Use these layouts for the login screen?")
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
                                color: Theme.alpha(Theme.accent, 0.16)
                                Icon { anchors.centerIn: parent; name: "keyboard"; color: Theme.accent; size: Theme.fontSize * 1.6 }
                            }
                            Txt { text: Tr.t("Use these layouts for the login screen?"); role: "title"; anchors.verticalCenter: parent.verticalCenter }
                        }
                        Txt {
                            Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone
                            text: Tr.t("The login screen, the text console and accounts created from now on will use: %1. People who chose their own layouts keep them.").arg(kb.layoutList)
                        }
                        Txt {
                            Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; role: "small"; color: Theme.textMuted
                            text: Tr.t("This changes the whole computer, so it needs an administrator's password.")
                        }
                        Txt {
                            visible: kb.applying
                            Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
                            text: Tr.t("Waiting for your password.")
                        }
                        Btn {
                            text: kb.showDetails ? Tr.t("Hide details") : Tr.t("Details")
                            icon: "list"; variant: "ghost"; e2e: "keyboard-system-details"
                            onClicked: kb.showDetails = !kb.showDetails
                        }
                        Rectangle {
                            visible: kb.showDetails
                            Layout.fillWidth: true
                            Layout.preferredHeight: Math.min(detailsText.implicitHeight + Theme.s2 * 2, 240)
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
                                    text: kb.proposal ? kb.proposal.report : ""
                                }
                            }
                        }
                        Flow {
                            Layout.fillWidth: true
                            layoutDirection: Qt.RightToLeft
                            spacing: Theme.s2
                            Btn { text: Tr.t("Use for the login screen"); icon: "check"; variant: "primary"; e2e: "keyboard-system-confirm"; enabled: !kb.busy; onClicked: kb.decide(true) }
                            Btn { id: notNow; text: Tr.t("Not now"); variant: "outline"; e2e: "keyboard-system-cancel"; enabled: !kb.busy; onClicked: kb.decide(false) }
                        }
                    }
                }
            }
        }
    }

    component Section: Txt {
        property string title
        text: title
        role: "large"
        font.weight: Font.DemiBold
        topPadding: Theme.s2
    }
    // A number with a slider: the value shows while it moves and is saved
    // a moment after the last change.
    component RepeatRow: RowLayout {
        id: rr
        property string label
        property string unit: "%1"
        property string e2e: ""
        property real from
        property real to
        property real stepSize
        property real value
        property real live: value
        signal committed(real v)
        Layout.fillWidth: true
        spacing: Theme.s3
        onValueChanged: live = value
        Txt { text: rr.label; Layout.preferredWidth: 200; wrapMode: Text.Wrap; elide: Text.ElideNone }
        Slider {
            Layout.fillWidth: true
            accessibleName: rr.label
            e2e: rr.e2e
            from: rr.from; to: rr.to; stepSize: rr.stepSize; value: rr.live
            onMoved: v => { rr.live = v; commit.restart(); }
        }
        Txt { text: rr.unit.arg(Math.round(rr.live)); Layout.preferredWidth: 110; role: "mono" }
        Timer { id: commit; interval: 400; onTriggered: rr.committed(rr.live) }
    }
}
