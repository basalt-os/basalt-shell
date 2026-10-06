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
    property string settingsPage: "appearance"
    property string commandText: ""
    // The window menu: { win: window id, x, y: global position } or null.
    property var windowMenu: null

    // Modal surfaces (authentication, confirmation, choice). While one is
    // up, the others give up keyboard focus: compositors differ in which
    // of several exclusive-focus surfaces gets the keyboard (in the lab,
    // sway gave it to the newest and niri to an older one), so the
    // shell makes sure there is only one.
    property bool polkitActive: false
    property bool confirmActive: false
    property bool chooserActive: false
    readonly property bool modal: polkitActive || confirmActive || chooserActive

    function closeAll() {
        launcher = false; commandBar = false; quickSettings = false; drawer = false; windowMenu = null; powerMenu = false;
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
    function openWindowMenu(id, x, y) {
        launcher = false; commandBar = false; quickSettings = false; drawer = false;
        windowMenu = { win: id, x: Math.round(x), y: Math.round(y) };
    }
    function open(surface, page) {
        closeAll();
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
        const isOpen = { launcher: launcher, commandbar: commandBar, quicksettings: quickSettings,
                         activity: drawer && drawerTab === "activity", notifications: drawer && drawerTab === "notifications",
                         settings: settings, power: powerMenu }[surface];
        if (isOpen) {
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
        function toggle(surface: string): void { ui.toggle(surface); }
        function open(surface: string): void { ui.open(surface, ""); }
        function close(): void { ui.closeAll(); }
        function ask(text: string): void { ui.commandText = text; ui.open("commandbar", ""); }
        function settingsPage(page: string): void { ui.open("settings", page); }
        function dismissPopups(): void { Notifs.popups = []; }
        // minimize, maximize, left, right, restore, close, float, menu
        function window(op: string): void { ui.windowOp(op, ""); }
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
