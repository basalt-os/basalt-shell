//@ pragma UseQApplication
//@ pragma Env QT_QUICK_FLICKABLE_WHEEL_DECELERATION=10000

import QtQuick
import Quickshell

// Basalt shell: panel, launcher, command bar, notifications, quick
// settings, activity, settings and confirmation sheets, all drawn from the
// design tokens served by the basalt-shell daemon.
ShellRoot {
    Variants {
        model: Quickshell.screens
        delegate: Wallpaper {}
    }
    Variants {
        model: Quickshell.screens
        delegate: Bar {}
    }
    Launcher {}
    CommandBar {}
    QuickSettings {}
    Drawer {}
    Popups {}
    ConfirmSheet {}
    ChooserSheet {}
    Settings {}
    PolkitDialog {}

    // Make sure the singletons start with the shell.
    Component.onCompleted: { Bus.connected; Notifs.unread; Ui.launcher; }
}
