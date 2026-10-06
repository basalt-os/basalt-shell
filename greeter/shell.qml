//@ pragma UseQApplication
//@ pragma Env QT_QUICK_FLICKABLE_WHEEL_DECELERATION=10000

import QtQuick
import Quickshell
import "logic.js" as L

// Basalt OS login screen: a greetd greeter drawn with Quickshell, run by
// basalt-greeter in a locked-down sway (see greeter/README.md).
ShellRoot {
    Variants {
        model: Quickshell.screens
        delegate: GreeterScreen {}
    }
    Component.onCompleted: {
        // The login screen never reloads itself when files change.
        Quickshell.watchFiles = false;
        G.themeId = Sys.conf.THEME || "basalt";
        G.mode = Sys.conf.MODE || "dark";
        G.largeText = Sys.state.largeText;
        G.highContrast = Sys.state.highContrast;
        I18n.chosen = Sys.state.language;
    }
}
