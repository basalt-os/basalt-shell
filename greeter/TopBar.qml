import QtQuick

// The buttons at the top right: keyboard layout, network, battery,
// accessibility, language and power. Each opens a small menu or acts.
// Keyboard: Tab reaches them; Left and Right (Home, End) move along them.
Row {
    id: bar
    property Item popoverHost: null
    spacing: G.s2
    Accessible.role: Accessible.ToolBar
    Accessible.name: I18n.t("System")

    // The visible buttons, left to right.
    function buttons() {
        const out = [];
        for (let i = 0; i < children.length; i++) {
            const c = children[i];
            if (c.visible && c.activeFocusOnTab) out.push(c);
        }
        return out;
    }
    function move(delta, toEnd) {
        const list = buttons();
        if (list.length === 0) return false;
        let i = list.findIndex(x => x.activeFocus);
        let j = toEnd ? (delta > 0 ? list.length - 1 : 0) : Math.max(0, Math.min(list.length - 1, i + delta));
        if (i < 0 && !toEnd) j = delta > 0 ? 0 : list.length - 1;
        if (j === i) return false;
        list[j].forceActiveFocus(delta > 0 ? Qt.TabFocusReason : Qt.BacktabFocusReason);
        return true;
    }
    Keys.onPressed: e => {
        const rtl = Qt.application.layoutDirection === Qt.RightToLeft;
        switch (e.key) {
        case Qt.Key_Left: e.accepted = move(rtl ? 1 : -1, false); break;
        case Qt.Key_Right: e.accepted = move(rtl ? -1 : 1, false); break;
        case Qt.Key_Home: e.accepted = move(-1, true); break;
        case Qt.Key_End: e.accepted = move(1, true); break;
        default: e.accepted = false;
        }
    }

    GBtn {
        id: layoutBtn
        visible: Sys.layout !== ""
        variant: "chip"
        icon: "keyboard"
        text: Sys.layout
        e2e: "layout"
        label: Sys.canSwitchLayout ? I18n.t("Keyboard layout: %1. Press to switch.").arg(Sys.layoutName)
                                   : I18n.t("Keyboard layout: %1").arg(Sys.layoutName)
        onClicked: Sys.nextLayout()
    }
    GBtn {
        id: netBtn
        variant: "chip"
        icon: Sys.net.kind === "wifi" ? "wifi" : (Sys.net.kind === "ethernet" ? "network" : "offline")
        e2e: "network"
        enabled: true
        label: Sys.net.kind === "wifi" ? I18n.t("Connected to %1").arg(Sys.net.name)
             : (Sys.net.kind === "ethernet" ? I18n.t("Connected by cable") : I18n.t("No network"))
        onClicked: infoPop.show(netBtn, label)
    }
    GBtn {
        id: batBtn
        visible: Sys.hasBattery
        variant: "chip"
        icon: Sys.charging ? "charging" : "battery"
        text: Sys.batteryPercent + "%"
        e2e: "battery"
        label: Sys.charging ? I18n.t("Charging, %1 percent").arg(Sys.batteryPercent) : I18n.t("Battery at %1 percent").arg(Sys.batteryPercent)
        onClicked: infoPop.show(batBtn, label)
    }
    GBtn {
        id: a11yBtn
        variant: "chip"
        icon: "a11y"
        e2e: "accessibility"
        label: I18n.t("Accessibility")
        active: G.largeText || G.highContrast
        onClicked: a11yMenu.openAt(a11yBtn)
    }
    GBtn {
        id: langBtn
        variant: "chip"
        icon: "globe"
        text: I18n.lang === "en" ? "EN" : I18n.lang.replace("_", "-").toUpperCase().slice(0, 2)
        e2e: "language"
        label: I18n.t("Language: %1").arg(I18n.languageName)
        onClicked: langMenu.openAt(langBtn)
    }
    GBtn {
        id: powerBtn
        variant: "chip"
        icon: "power"
        e2e: "power"
        label: I18n.t("Power")
        onClicked: powerMenu.openAt(powerBtn)
    }

    // Plain information (network, battery): a one-line menu.
    Popover {
        id: infoPop
        parent: bar.popoverHost
        e2e: "info"
        function show(btn, text) { items = [{ id: "ok", text: text }]; openAt(btn); }
        onPicked: { close(); Login.focusRequested(); }
    }
    Popover {
        id: a11yMenu
        parent: bar.popoverHost
        title: I18n.t("Accessibility")
        e2e: "accessibility-menu"
        items: [
            { id: "large", icon: "text", text: I18n.t("Large text"), checked: G.largeText, e2e: "a11y:large" },
            { id: "contrast", icon: "contrast", text: I18n.t("High contrast"), checked: G.highContrast, e2e: "a11y:contrast" }
        ]
        onPicked: id => {
            if (id === "large") G.largeText = !G.largeText;
            if (id === "contrast") G.highContrast = !G.highContrast;
            const s = Sys.state;
            s.largeText = G.largeText;
            s.highContrast = G.highContrast;
            Sys.saveState(s);
            close();
            Login.focusRequested();
        }
    }
    Popover {
        id: langMenu
        parent: bar.popoverHost
        title: I18n.t("Language")
        e2e: "language-menu"
        items: I18n.languages.map(l => ({ id: l.id, text: l.name, checked: l.id === I18n.lang, e2e: "lang:" + l.id }))
        onPicked: id => {
            I18n.chosen = id;
            const s = Sys.state;
            s.language = id;
            Sys.saveState(s);
            close();
            Login.focusRequested();
        }
    }
    Popover {
        id: powerMenu
        parent: bar.popoverHost
        title: I18n.t("Power")
        e2e: "power-menu"
        items: [
            { id: "suspend", icon: "moon", text: I18n.t("Suspend"), e2e: "power:suspend" },
            { id: "restart", icon: "restart", text: I18n.t("Restart"), e2e: "power:restart" },
            { id: "poweroff", icon: "power", text: I18n.t("Power off"), e2e: "power:poweroff" }
        ]
        onPicked: id => { close(); Sys.power(id); }
    }
}
