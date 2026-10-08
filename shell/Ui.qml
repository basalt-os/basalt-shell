pragma Singleton

import QtQuick
import Quickshell
import Quickshell.Io

// Which shell surfaces are open. Keybindings reach it through
// `qs ipc call shell <function>` (see the compositor configs).
Singleton {
    id: ui

    property bool launcher: false
    property bool commandBar: false
    property bool quickSettings: false
    property bool drawer: false          // notifications and activity
    property string drawerTab: "notifications"
    property bool settings: false
    // The power menu (lock, log out, suspend, restart, power off).
    property bool powerMenu: false
    // Opens the power menu straight on a countdown: "update" is the
    // restart into a staged offline update (Settings, Updates).
    property string powerStart: ""
    property string settingsPage: "appearance"
    property string commandText: ""
    // An app's question for the command bar ({ id, text, from }): it opens
    // with the text and runs it in the daemon's narrow mode for apps
    // (internal/shell/appask.go) unless the person changes the text.
    property var commandAsk: null
    // The window menu: { win: window id, x, y: global position } or null.
    property var windowMenu: null

    // Keyboard and focus (docs/design.md, Keyboard and focus).
    //
    // focusVisible: the last input was the keyboard, so the focused
    // control shows its focus ring (the focus-visible rule of the web: a
    // click focuses without a ring, Tab or an arrow shows it). Any key a
    // control sees turns it on, a mouse press on a control turns it off,
    // and a surface opened by a key binding starts with it on.
    property bool focusVisible: false
    // The panel holds the keyboard (Ctrl+Alt+Tab or Super+B): Left and
    // Right move along it, Escape gives the keyboard back to the window.
    property bool panelFocus: false
    // The control to focus when a surface opens: its e2e name (set when a
    // surface is opened from another one, or when focus returns to it).
    property string focusKey: ""
    // Where focus goes back when a surface is dismissed with Escape:
    // { surface, key } (surface "panel" is the panel itself).
    property var returnTo: null
    // The name of the control that holds the keyboard (navKey, e2e or its
    // label), for screen reader checks and the keyboard tests (ipc focused).
    property string focusedControl: ""
    // The control itself, when its caller passes it (for ipc focusedRect).
    property var focusedItem: null
    function noteFocused(name, on, item) {
        if (on) { focusedControl = name; focusedItem = item || null; }
        else if (focusedControl === name) { focusedControl = ""; focusedItem = null; }
    }

    // Modal surfaces (authentication, confirmation, choice). While one is
    // up, the others give up keyboard focus: compositors differ in which
    // of several exclusive-focus surfaces gets the keyboard (in the lab,
    // sway gave it to the newest and niri to an older one), so the
    // shell makes sure there is only one.
    property bool polkitActive: false
    property bool confirmActive: false
    property bool chooserActive: false
    readonly property bool modal: polkitActive || confirmActive || chooserActive
    // The lock screen holds the session (Lock.qml): the daemon refuses
    // push to talk and agent input while it does.
    property bool locked: false
    // A surface the person opened holds the keyboard: cards that ask
    // something on their own (a model download offer) wait for it to close
    // before they take the keyboard.
    readonly property bool surfaceOpen: launcher || commandBar || quickSettings || drawer || powerMenu || windowMenu !== null || panelFocus

    function closeAll() {
        launcher = false; commandBar = false; quickSettings = false; drawer = false; windowMenu = null; powerMenu = false;
        panelFocus = false;
    }

    // The panel takes the keyboard (on the screen with the focused
    // workspace); focus starts on its first control, or on key.
    function focusPanel(key) {
        closeAll();
        returnTo = null;
        focusKey = key || "";
        focusVisible = true;
        panelFocus = true;
    }
    // Escape on a surface: close it and put the focus back where the
    // surface was opened from (a panel button, a quick settings button).
    // Without an opener the keyboard simply goes back to the window that
    // had it (the compositor does that when the surface closes).
    // The restart into the staged offline update: the power menu's 60
    // second countdown, focused on Cancel (which keeps it staged).
    function restartForUpdate() {
        powerStart = "update";
        powerMenu = true;
    }
    function dismiss() {
        const back = returnTo;
        returnTo = null;
        closeAll();
        if (!back) return;
        focusKey = back.key || "";
        if (back.surface === "panel") panelFocus = true;
        else open(back.surface, "");
    }
    function isOpen(surface) {
        return ({ launcher: launcher, commandbar: commandBar, quicksettings: quickSettings,
                  activity: drawer && drawerTab === "activity", notifications: drawer && drawerTab === "notifications",
                  settings: settings, power: powerMenu })[surface] === true;
    }
    // A surface opened from a control of another one: Escape comes back to
    // that control (from: "panel", "quicksettings"; key: its e2e name).
    function openFrom(surface, page, from, key) {
        const back = from ? { surface: from, key: key || "" } : null;
        open(surface, page);
        returnTo = back;
    }
    function toggleFrom(surface, from, key) {
        if (isOpen(surface)) toggle(surface);
        else openFrom(surface, "", from, key);
    }

    function windowById(id) {
        return (Bus.desktop.windows || []).find(w => w.id === id) || null;
    }
    function focusedWindow() {
        return (Bus.desktop.windows || []).find(w => w.focused) || null;
    }
    // Window states from keys, the panel and the window menu. Toggles
    // look at the state the daemon reports (maximized, left, right,
    // minimized) so a second press puts the window back.
    function windowOp(op, id) {
        const w = id ? windowById(id) : focusedWindow();
        if (!w) return;
        const st = w.state || "";
        const set = s => Bus.act("window.set_state", { window: w.id, state: s });
        switch (op) {
        case "minimize": set("minimized"); break;
        case "maximize": set(st === "maximized" ? "normal" : "maximized"); break;
        case "left": set(st === "left" ? "normal" : "left"); break;
        case "right": set(st === "right" ? "normal" : "right"); break;
        // Super+Down: a maximized or snapped window goes back to its size,
        // a normal one is minimized (as on GNOME and Windows).
        case "restore": set(st === "" ? "minimized" : "normal"); break;
        case "normal": set("normal"); break;
        case "close": Bus.act("window.close", { window: w.id }); break;
        case "float": Bus.act("window.set_floating", { window: w.id, floating: !w.floating }); break;
        case "menu": {
            // Under the window's title bar (sway's mouse bindings and keys
            // do not say where the pointer is). The rectangle comes fresh
            // from the compositor: sway sends no event when a floating
            // window is dragged, so the last known one may be old.
            const at = win => {
                const r = win.rect || { x: 0, y: 0, width: 0, height: 0 };
                openWindowMenu(win.id, r.x + Theme.s2, r.y + Theme.s6 + Theme.s2);
            };
            Bus.call("desktop", {}, (ok, d) => {
                const fresh = ok && d ? (d.windows || []).find(x => x.id === w.id) : null;
                at(fresh || w);
            });
            break;
        }
        }
    }
    // from, key: the panel's window list entry it was opened from (Escape
    // goes back there), or nothing.
    function openWindowMenu(id, x, y, from, key) {
        launcher = false; commandBar = false; quickSettings = false; drawer = false; powerMenu = false;
        panelFocus = false;
        returnTo = from ? { surface: from, key: key || "" } : null;
        windowMenu = { win: id, x: Math.round(x), y: Math.round(y) };
    }
    function open(surface, page) {
        closeAll();
        returnTo = null;
        switch (surface) {
        case "launcher": launcher = true; break;
        case "commandbar": commandBar = true; break;
        case "quicksettings": quickSettings = true; break;
        case "activity": drawerTab = "activity"; drawer = true; break;
        case "notifications": drawerTab = "notifications"; drawer = true; break;
        case "settings": if (page) settingsPage = page; settings = true; break;
        case "power": powerMenu = true; break;
        }
    }
    function toggle(surface) {
        if (isOpen(surface)) {
            if (surface === "settings") settings = false; else closeAll();
        } else {
            open(surface, "");
        }
    }

    Connections {
        target: Bus
        function onOpenRequested(surface, page) { ui.open(surface, page); }
    }

    IpcHandler {
        target: "shell"
        // Key bindings: the surface they open shows the focus ring at once.
        function toggle(surface: string): void { ui.focusVisible = true; ui.focusKey = ""; ui.toggle(surface); }
        function open(surface: string): void { ui.focusVisible = true; ui.focusKey = ""; ui.open(surface, ""); }
        function close(): void { ui.closeAll(); }
        function ask(text: string): void { ui.commandText = text; ui.focusVisible = true; ui.open("commandbar", ""); }
        function settingsPage(page: string): void { ui.focusVisible = true; ui.open("settings", page); }
        function dismissPopups(): void { Notifs.popups = []; }
        // The panel takes the keyboard (Ctrl+Alt+Tab, Super+B).
        function focusPanel(): void { ui.focusPanel(""); }
        // For the keyboard tests (lab/keyboard): the control with the
        // keyboard focus, and the surfaces that are open.
        function focused(): string { return ui.focusedControl; }
        // Where that control is drawn in its window: "x y width height"
        // (the panel's window list must stay clear of the clock).
        function focusedRect(): string {
            const it = ui.focusedItem;
            if (!it) return "";
            const r = it.mapToItem(null, 0, 0);
            return [r.x, r.y, it.width, it.height].map(v => Math.round(v)).join(" ");
        }
        function surfaces(): string {
            return ["launcher", "commandbar", "quicksettings", "activity", "notifications", "settings", "power"]
                .filter(s => ui.isOpen(s)).concat(ui.windowMenu ? ["windowmenu"] : []).concat(ui.panelFocus ? ["panel"] : [])
                .concat(ui.settings ? ["page:" + ui.settingsPage] : []).join(" ");
        }
        // minimize, maximize, left, right, restore, close, float, menu
        function window(op: string): void { if (op === "menu") ui.focusVisible = true; ui.windowOp(op, ""); }
        // Push to talk from the key binding: press, release, cancel.
        // "toggle" is a press with no release to follow (niri has no
        // key-release bindings): the same path as press, and the daemon
        // treats that utterance as "press to start and stop".
        function voice(op: string): void {
            if (op === "press") Bus.voicePress(false);
            else if (op === "release") Bus.voiceRelease();
            else if (op === "cancel") Bus.voiceCancel();
            else if (op === "toggle") Bus.voicePress(true);
            // Dictation waiting on the card: Super+Return types it, Super+BackSpace drops it.
            else if (op === "insert") Bus.dictationDecide(true);
            else if (op === "discard") Bus.dictationDecide(false);
        }
    }
}
