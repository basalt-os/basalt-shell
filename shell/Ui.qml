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
    property string settingsPage: "appearance"
    property string commandText: ""

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
        launcher = false; commandBar = false; quickSettings = false; drawer = false;
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
        }
    }
    function toggle(surface) {
        const isOpen = { launcher: launcher, commandbar: commandBar, quicksettings: quickSettings,
                         activity: drawer && drawerTab === "activity", notifications: drawer && drawerTab === "notifications",
                         settings: settings }[surface];
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
    }
}
