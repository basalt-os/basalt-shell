import QtQuick
import QtQuick.Layouts
import Quickshell

// The settings window: a normal (floating) window so it behaves like any
// app. Every token is editable here; simple controls on the Appearance
// page, the full token list on the Tokens page. Changes apply at once.
FloatingWindow {
    id: win
    visible: Ui.settings
    // One title: the window's (the sidebar shows the mark and the pages).
    title: Tr.t("Settings")
    // Fits a 1280x800 screen with the panel; never smaller than 720x480.
    implicitWidth: 960
    implicitHeight: 640
    minimumSize: Qt.size(720, 480)
    color: Theme.surface
    onVisibleChanged: {
        if (!visible) { Ui.settings = false; return; }
        Qt.callLater(win.focusSidebar);
    }

    // Keyboard: the sidebar is one Tab stop (Up, Down, Home, End and the
    // first letter move, and the page follows); Right, Return or Tab go
    // into the page; Left at the start of a row, or Escape, come back to
    // the sidebar. Ctrl+W closes the window.
    function focusSidebar() {
        const it = Nav.find(side, "settings-nav-" + Ui.settingsPage);
        if (it) it.forceActiveFocus(Qt.TabFocusReason);
    }
    function focusPage() {
        Nav.initial(page, "");
    }
    // A new page starts at its top.
    Connections {
        target: Ui
        function onSettingsPageChanged() { pageFlick.contentY = 0; }
    }

    readonly property var st: Bus.themeState
    readonly property var settings: st ? st.settings : null
    readonly property var specs: st ? st.specs : []

    function setTokens(obj) { Bus.act("theme.set_tokens", { tokens: obj }); }
    function overridden(key) {
        if (!settings) return false;
        const o = settings.overrides || {}, l = settings.light || {}, d = settings.dark || {};
        return key in o || (Theme.dark ? key in d : key in l);
    }

    readonly property var pages: [
        { id: "appearance", label: "Appearance", icon: "palette" },
        { id: "tokens", label: "Design tokens", icon: "sliders" },
        { id: "motion", label: "Motion", icon: "motion" },
        { id: "windows", label: "Windows", icon: "window" },
        { id: "keyboard", label: Tr.t("Keyboard"), icon: "keyboard" },
        { id: "ai", label: "Assistant and AI", icon: "spark" },
        { id: "voice", label: Tr.t("Voice and assistant"), icon: "mic" },
        { id: "updates", label: Tr.t("Updates and channels"), icon: "download" },
        { id: "drivers", label: Tr.t("Additional drivers"), icon: "chip" },
        { id: "about", label: "About", icon: "info" }
    ]

    Behavior on color { ColorAnimation { duration: Theme.normal; easing.type: Theme.easing } }

    RowLayout {
        anchors.fill: parent
        spacing: 0
        Keys.onPressed: e => {
            if ((e.modifiers & Qt.ControlModifier) && (e.key === Qt.Key_W || e.key === Qt.Key_Q)) { Ui.settings = false; e.accepted = true; return; }
            e.accepted = false;
        }

        // Sidebar.
        Rectangle {
            Layout.fillHeight: true
            Layout.preferredWidth: 240
            color: Theme.bg
            Behavior on color { ColorAnimation { duration: Theme.normal } }
            ColumnLayout {
                anchors.fill: parent
                anchors.margins: Theme.s4
                spacing: Theme.s1
                Item {
                    implicitWidth: mark.width
                    implicitHeight: mark.height + Theme.s4
                    Icon { id: mark; name: "logo"; size: Theme.fontTitle * 1.6 }
                }
                NavColumn {
                    id: side
                    Layout.fillWidth: true
                    spacing: Theme.s1
                    wrap: false
                    typeAhead: true
                    accessibleRole: Accessible.PageTabList
                    Accessible.name: Tr.t("Settings pages")
                    Keys.onRightPressed: win.focusPage()
                    Repeater {
                        model: win.pages
                        delegate: Btn {
                            required property var modelData
                            width: side.width
                            icon: modelData.icon
                            text: modelData.label
                            alignLeft: true
                            e2e: "settings-nav-" + modelData.id
                            accessibleRole: Accessible.PageTab
                            checkable: true
                            active: Ui.settingsPage === modelData.id
                            onClicked: {
                                // Return or Space on the current page goes into it.
                                if (Ui.settingsPage === modelData.id && Ui.focusVisible) win.focusPage();
                                Ui.settingsPage = modelData.id;
                            }
                            // The page follows the keyboard focus along the sidebar.
                            onActiveFocusChanged: if (activeFocus && Ui.focusVisible) Ui.settingsPage = modelData.id
                        }
                    }
                }
                Item { Layout.fillHeight: true }
                Txt { text: Bus.connected ? "Connected to basalt-shell " + Bus.version : "Daemon not running"; role: "small"; color: Bus.connected ? Theme.textMuted : Theme.danger; Layout.fillWidth: true }
            }
        }

        Flickable {
            id: pageFlick
            Layout.fillWidth: true
            Layout.fillHeight: true
            contentHeight: page.implicitHeight + Theme.s6 * 2
            clip: true
            ColumnLayout {
                id: page
                x: Theme.s6
                y: Theme.s6
                width: parent.width - Theme.s6 * 2
                spacing: Theme.s4
                Accessible.role: Accessible.PageTab
                Accessible.name: (win.pages.find(x => x.id === Ui.settingsPage) || { label: "" }).label
                // Keys no control of the page used.
                Keys.onPressed: e => {
                    if (e.key === Qt.Key_Escape || (e.key === Qt.Key_Left && !(e.modifiers & ~Qt.KeypadModifier))) {
                        Ui.focusVisible = true;
                        win.focusSidebar();
                        e.accepted = true;
                        return;
                    }
                    e.accepted = false;
                }

                // Appearance.
                ColumnLayout {
                    visible: Ui.settingsPage === "appearance"
                    Layout.fillWidth: true
                    spacing: Theme.s4
                    Txt { text: "Appearance"; role: "display" }
                    Txt { text: "Pick a theme, then make it yours. Everything below also changes your apps (GTK, libadwaita, Qt) and window borders."; color: Theme.textMuted; wrapMode: Text.Wrap; Layout.fillWidth: true; elide: Text.ElideNone }

                    NavFlow {
                        Layout.fillWidth: true
                        spacing: Theme.s3
                        Accessible.name: Tr.t("Theme")
                        Repeater {
                            model: win.st ? win.st.themes : []
                            delegate: ThemeCard { required property var modelData; meta: modelData }
                        }
                    }

                    Section { title: "Mode" }
                    NavRow {
                        spacing: Theme.s2
                        Accessible.name: Tr.t("Mode")
                        Btn { text: "Light"; icon: "sun"; variant: "outline"; checkable: true; e2e: "settings-mode-light"; active: !Theme.dark; onClicked: Bus.act("theme.switch", { mode: "light" }) }
                        Btn { text: "Dark"; icon: "moon"; variant: "outline"; checkable: true; e2e: "settings-mode-dark"; active: Theme.dark; onClicked: Bus.act("theme.switch", { mode: "dark" }) }
                    }

                    Section { title: "Accent color" }
                    NavRow {
                        spacing: Theme.s2
                        Accessible.name: Tr.t("Accent color")
                        Repeater {
                            model: [["#a3472e", Tr.t("Terra")], ["#c0392b", Tr.t("Red")], ["#d9772b", Tr.t("Orange")], ["#c99a06", Tr.t("Yellow")],
                                    ["#3f8f4f", Tr.t("Green")], ["#2f7d78", Tr.t("Teal")], ["#3b6fd1", Tr.t("Blue")], ["#7b4fc9", Tr.t("Violet")],
                                    ["#c4497f", Tr.t("Pink")], ["#5f6f7f", Tr.t("Slate")]]
                            delegate: Pressable {
                                required property var modelData
                                width: 30; height: 30; radius: 15
                                color: modelData[0]
                                border.width: active ? 3 : 0
                                border.color: Theme.text
                                accessibleName: modelData[1]
                                e2e: "settings-accent-" + modelData[0].slice(1)
                                checkable: true
                                active: ("" + Theme.accent) === modelData[0]
                                checked: active
                                onClicked: win.setTokens({ "color.accent": modelData[0] })
                            }
                        }
                    }

                    Section { title: "Shape and size" }
                    LabeledSlider {
                        e2e: "settings-radius"
                        label: "Corner roundness"; from: 0; to: 24; stepSize: 1; value: Theme.radiusMd; suffix: " px"
                        // One scale from one slider: chips, controls, popovers, sheets
                        // and windows keep their proportions (6, 10, 12, 16, 8
                        // at the default 10).
                        onCommitted: v => win.setTokens({ "radius.sm": Math.round(v * 0.6), "radius.md": v, "radius.lg": Math.min(40, Math.round(v * 1.2)),
                                                          "radius.xl": Math.min(48, Math.round(v * 1.6)), "radius.window": Math.min(32, Math.round(v * 0.8)) })
                    }
                    LabeledSlider {
                        label: "Text size"; from: 8; to: 16; stepSize: 0.5; value: Theme.fontSize; suffix: " pt"
                        onCommitted: v => win.setTokens({ "font.size": v })
                    }
                    LabeledSlider {
                        label: "Spacing"; from: 2; to: 8; stepSize: 1; value: Theme.unit; suffix: " px unit"
                        onCommitted: v => win.setTokens({ "spacing.unit": v })
                    }
                    LabeledSlider {
                        label: "Panel opacity"; from: 0.5; to: 1; stepSize: 0.05; value: Theme.panelOpacity
                        onCommitted: v => win.setTokens({ "panel.opacity": v })
                    }
                    NavRow {
                        spacing: Theme.s2
                        Accessible.name: Tr.t("Panel position")
                        Txt { text: "Panel"; width: 160; anchors.verticalCenter: parent.verticalCenter }
                        Btn { text: "Top"; variant: "outline"; checkable: true; active: Theme.panelPosition === "top"; onClicked: win.setTokens({ "panel.position": "top" }) }
                        Btn { text: "Bottom"; variant: "outline"; checkable: true; active: Theme.panelPosition === "bottom"; onClicked: win.setTokens({ "panel.position": "bottom" }) }
                    }
                    // Attached to the screen edge (the default) or the
                    // floating pill.
                    NavRow {
                        spacing: Theme.s2
                        Accessible.name: Tr.t("Panel style")
                        Txt { text: Tr.t("Panel style"); width: 160; anchors.verticalCenter: parent.verticalCenter }
                        Btn { text: Tr.t("Attached"); e2e: "settings-panel-attached"; variant: "outline"; checkable: true; active: !Theme.panelFloating; onClicked: win.setTokens({ "panel.style": "attached" }) }
                        Btn { text: Tr.t("Floating"); e2e: "settings-panel-floating"; variant: "outline"; checkable: true; active: Theme.panelFloating; onClicked: win.setTokens({ "panel.style": "floating" }) }
                    }

                    Section { title: "Keep this look" }
                    Row {
                        spacing: Theme.s2
                        Field { id: saveName; width: 260; placeholder: "Name for a new theme"; e2e: "settings-theme-name"; onAccepted: saveBtn.clicked() }
                        Btn {
                            id: saveBtn
                            text: "Save as theme"; variant: "primary"
                            onClicked: {
                                const name = saveName.text.trim();
                                if (!name) return;
                                const id = name.toLowerCase().replace(/[^a-z0-9]+/g, "-").replace(/^-|-$/g, "").slice(0, 40) || "custom";
                                Bus.call("theme.save_as", { id: id, name: name }, () => {});
                                saveName.text = "";
                            }
                        }
                        Btn { text: "Reset to theme"; variant: "outline"; onClicked: Bus.act("theme.reset", {}) }
                    }
                }

                // Tokens.
                ColumnLayout {
                    visible: Ui.settingsPage === "tokens"
                    Layout.fillWidth: true
                    spacing: Theme.s3
                    Txt { text: "Design tokens"; role: "display" }
                    Txt { text: "Every value the shell is drawn with. Colors apply to the current mode (" + Theme.mode + "). A dot marks your overrides."; color: Theme.textMuted; wrapMode: Text.Wrap; Layout.fillWidth: true; elide: Text.ElideNone }
                    Repeater {
                        model: ["color", "typography", "shape", "spacing", "elevation", "motion", "windows", "apps"]
                        delegate: ColumnLayout {
                            id: grp
                            required property var modelData
                            Layout.fillWidth: true
                            spacing: Theme.s1
                            Section { title: grp.modelData }
                            Repeater {
                                model: win.specs.filter(s => s.group === grp.modelData)
                                delegate: TokenRow { required property var modelData; spec: modelData; Layout.fillWidth: true }
                            }
                        }
                    }
                }

                // Motion.
                ColumnLayout {
                    visible: Ui.settingsPage === "motion"
                    Layout.fillWidth: true
                    spacing: Theme.s4
                    Txt { text: "Motion"; role: "display" }
                    Txt { text: "Subtle animations by default. Auto turns them off on weak hardware (software rendering, very few CPUs or little memory)."; color: Theme.textMuted; wrapMode: Text.Wrap; Layout.fillWidth: true; elide: Text.ElideNone }
                    NavRow {
                        spacing: Theme.s2
                        Accessible.name: Tr.t("Motion")
                        Repeater {
                            model: [["auto", "Auto"], ["full", "Full"], ["reduced", "Reduced"]]
                            delegate: Btn {
                                required property var modelData
                                text: modelData[1]; variant: "outline"; checkable: true
                                e2e: "settings-motion-" + modelData[0]
                                active: !!win.settings && win.settings.motion === modelData[0]
                                onClicked: Bus.act("motion.set", { motion: modelData[0] })
                            }
                        }
                    }
                    Rectangle {
                        Layout.fillWidth: true
                        implicitHeight: hwCol.implicitHeight + Theme.s3 * 2
                        radius: Theme.radiusMd
                        color: Theme.bg
                        border.width: 1; border.color: Theme.border
                        Column {
                            id: hwCol
                            anchors.fill: parent
                            anchors.margins: Theme.s3
                            spacing: Theme.s1
                            Txt { text: "Now: " + (Theme.animate ? "animations on" : "animations off"); font.weight: Font.DemiBold }
                            Txt { text: win.st ? ("GPU: " + win.st.hardware.gpu + ", CPUs: " + win.st.hardware.cpus + ", memory: " + win.st.hardware.mem_mib + " MiB") : ""; color: Theme.textMuted }
                            Txt { text: win.st && win.st.hardware.weak ? "Weak hardware: " + win.st.hardware.reasons.join("; ") : "Hardware can animate smoothly"; color: Theme.textMuted; width: parent.width; wrapMode: Text.Wrap; elide: Text.ElideNone }
                        }
                    }
                    Repeater {
                        model: win.specs.filter(s => s.group === "motion")
                        delegate: TokenRow { required property var modelData; spec: modelData; Layout.fillWidth: true }
                    }
                    Txt { text: "Preview"; role: "large" }
                    Rectangle {
                        id: demo
                        Layout.preferredWidth: 360; Layout.preferredHeight: 60
                        radius: Theme.radiusMd; color: Theme.bg; border.width: 1; border.color: Theme.border
                        property bool atEnd: false
                        Rectangle {
                            width: 44; height: 44; radius: Theme.radiusSm; color: Theme.accent
                            anchors.verticalCenter: parent.verticalCenter
                            x: demo.atEnd ? demo.width - width - 8 : 8
                            Behavior on x { NumberAnimation { duration: Theme.slow; easing.type: Theme.easing } }
                        }
                        Timer { interval: 1200; running: Ui.settings && Ui.settingsPage === "motion"; repeat: true; onTriggered: demo.atEnd = !demo.atEnd }
                    }
                }

                // Windows.
                ColumnLayout {
                    visible: Ui.settingsPage === "windows"
                    Layout.fillWidth: true
                    spacing: Theme.s4
                    Txt { text: "Windows"; role: "display" }
                    Txt {
                        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
                        text: "Compositor: " + (Bus.desktop.compositor || "none") + (Bus.desktop.version ? " (" + Bus.desktop.version + ")" : "") +
                              ". New windows float by default; tiling is one click away."
                    }
                    NavFlow {
                        Layout.fillWidth: true
                        spacing: Theme.s2
                        Accessible.name: Tr.t("Arrange windows")
                        Repeater {
                            model: [["grid", "Grid"], ["columns", "Side by side"], ["cascade", "Cascade"], ["center", "Center"], ["tile", "Tile"], ["float", "Float all"]]
                            delegate: Btn { required property var modelData; text: modelData[1]; variant: "outline"; onClicked: Bus.act("windows.arrange", { layout: modelData[0] }) }
                        }
                    }
                    Repeater {
                        model: win.specs.filter(s => s.group === "windows")
                        delegate: TokenRow { required property var modelData; spec: modelData; Layout.fillWidth: true }
                    }
                    Section { title: "What this compositor supports" }
                    Repeater {
                        model: Object.keys(Bus.desktop.caps || {})
                        delegate: Row {
                            required property var modelData
                            spacing: Theme.s2
                            Icon { name: Bus.desktop.caps[modelData] ? "check" : "close"; color: Bus.desktop.caps[modelData] ? Theme.success : Theme.textMuted; size: Theme.fontSize * 1.3 }
                            Txt { text: modelData.replace(/_/g, " ") }
                        }
                    }
                }

                // AI.
                ColumnLayout {
                    visible: Ui.settingsPage === "ai"
                    Layout.fillWidth: true
                    spacing: Theme.s4
                    Txt { text: "Assistant and AI"; role: "display" }
                    Txt {
                        Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone; color: Theme.textMuted
                        text: "Models never act directly. They ask for typed actions (" + Bus.actions.length + " kinds); every change waits for you on a confirmation sheet and is written to the activity log."
                    }
                    Row { spacing: Theme.s2; Icon { name: Bus.translatorAvailable ? "check" : "info"; color: Bus.translatorAvailable ? Theme.success : Theme.textMuted }
                          Txt { text: Bus.translatorAvailable ? "Command bar: local language model (fixed phrases as fallback)" : "Command bar: fixed phrases (no local model configured)" } }
                    Row { spacing: Theme.s2; Icon { name: Bus.assistantAvailable ? "check" : "info"; color: Bus.assistantAvailable ? Theme.success : Theme.textMuted }
                          Txt { text: Bus.assistantAvailable ? "System assistant (basalt) installed: system questions and its proposals work here" : "System assistant (basalt) not installed" } }
                    Row { spacing: Theme.s2
                          Icon { name: Bus.uiCheck && Bus.uiCheck.mode === "selinux" ? "check" : "info"; color: Bus.uiCheck && Bus.uiCheck.mode === "selinux" ? Theme.success : Theme.warning }
                          Txt { text: Bus.uiCheck && Bus.uiCheck.mode === "selinux"
                                ? "Confirmations: only this shell's SELinux domain can confirm; agents run confined and can only ask"
                                : "Confirmations: checked by program name only (" + (Bus.uiCheck ? Bus.uiCheck.reason : "unknown") + ")" } }
                    Section { title: "Connect an MCP client" }
                    Rectangle {
                        Layout.fillWidth: true
                        implicitHeight: mcpTxt.implicitHeight + Theme.s3 * 2
                        radius: Theme.radiusMd; color: Theme.bg; border.width: 1; border.color: Theme.border
                        Txt {
                            id: mcpTxt
                            anchors.fill: parent; anchors.margins: Theme.s3
                            role: "mono"; wrapMode: Text.Wrap; elide: Text.ElideNone
                            text: '{ "mcpServers": { "basalt-shell": { "command": "basalt-shell", "args": ["mcp"] } } }'
                        }
                    }
                    Section { title: "Available actions" }
                    Repeater {
                        model: Bus.actions
                        delegate: Column {
                            required property var modelData
                            Layout.fillWidth: true
                            Txt { text: modelData.title + "   " ; font.weight: Font.Medium }
                            Txt { text: modelData.name + ": " + modelData.description; role: "small"; color: Theme.textMuted; width: page.width; wrapMode: Text.Wrap; elide: Text.ElideNone }
                        }
                    }
                }

                // Keyboard: layouts, the switch key, Caps Lock, compose, repeat.
                KeyboardSettings {
                    visible: Ui.settingsPage === "keyboard" && Ui.settings
                    Layout.fillWidth: true
                }

                // Voice and assistant: the person's own languages and models.
                VoiceSettings {
                    visible: Ui.settingsPage === "voice" && Ui.settings
                    Layout.fillWidth: true
                }

                // Updates and channels (basalt updates, basalt channels).
                UpdatesSettings {
                    visible: Ui.settingsPage === "updates" && Ui.settings
                    Layout.fillWidth: true
                }

                // Additional drivers (the NVIDIA driver of basalt-nonfree).
                DriversSettings {
                    visible: Ui.settingsPage === "drivers" && Ui.settings
                    Layout.fillWidth: true
                }

                // About.
                ColumnLayout {
                    visible: Ui.settingsPage === "about"
                    Layout.fillWidth: true
                    spacing: Theme.s3
                    Txt { text: "About"; role: "display" }
                    Txt { text: "Basalt shell " + Bus.version + " (prototype)"; font.weight: Font.Medium }
                    Txt { text: "Compositor: " + (Bus.desktop.version || Bus.desktop.compositor); color: Theme.textMuted }
                    Txt { text: "Theme files: /usr/share/basalt-shell/themes and ~/.config/basalt-shell/themes (JSON). Settings: ~/.config/basalt-shell/settings.json."; color: Theme.textMuted; wrapMode: Text.Wrap; Layout.fillWidth: true; elide: Text.ElideNone }
                    Repeater {
                        model: win.st && win.st.errors ? win.st.errors : []
                        delegate: Txt { required property var modelData; text: modelData; color: Theme.warning; Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone }
                    }
                    Repeater {
                        model: win.st && win.st.apps_appearance ? (win.st.apps_appearance.applied || []) : []
                        delegate: Txt { required property var modelData; text: "Applied to apps: " + modelData; role: "small"; color: Theme.textMuted; Layout.fillWidth: true; wrapMode: Text.Wrap; elide: Text.ElideNone }
                    }
                    Btn { text: "Reload themes"; variant: "outline"; onClicked: Bus.call("reload", {}, () => {}) }
                }
            }
        }
    }

    component Section: Txt {
        property string title
        text: title.charAt(0).toUpperCase() + title.slice(1)
        role: "large"
        font.weight: Font.DemiBold
        topPadding: Theme.s2
    }

    component LabeledSlider: RowLayout {
        id: ls
        property string label
        property real from
        property real to
        property real stepSize
        property real value
        property string suffix: ""
        property string e2e: ""
        property real live: value
        signal committed(real v)
        Layout.fillWidth: true
        spacing: Theme.s3
        onValueChanged: live = value
        Txt { text: ls.label; Layout.preferredWidth: 160 }
        Slider {
            Layout.fillWidth: true
            accessibleName: ls.label
            e2e: ls.e2e
            from: ls.from; to: ls.to; stepSize: ls.stepSize; value: ls.live
            onMoved: v => { ls.live = v; commitTimer.restart(); }
        }
        Txt { text: (Math.round(ls.live * 100) / 100) + ls.suffix; Layout.preferredWidth: 90; role: "mono" }
        Timer { id: commitTimer; interval: 250; onTriggered: ls.committed(ls.live) }
    }

    component ThemeCard: Pressable {
        id: tc
        property var meta
        readonly property var sw: Theme.dark ? meta.dark : meta.light
        width: 200; height: 128
        radius: Theme.radiusLg
        color: sw[0]
        accessibleName: meta.name
        accessibleDescription: meta.description || ""
        e2e: "settings-theme-" + meta.id
        checkable: true
        active: Theme.themeId === meta.id
        checked: active
        onClicked: Bus.act("theme.switch", { theme: tc.meta.id })
        border.width: Theme.themeId === meta.id ? 3 : 1
        border.color: Theme.themeId === meta.id ? Theme.accent : Theme.border
        Behavior on border.color { ColorAnimation { duration: Theme.normal } }
        Rectangle { x: 12; y: 12; width: parent.width - 24; height: 16; radius: 5; color: tc.sw[1] }
        Rectangle { x: 12; y: 36; width: 70; height: 34; radius: 6; color: tc.sw[1] }
        Rectangle { x: 90; y: 36; width: 40; height: 14; radius: 7; color: tc.sw[2] }
        Txt { x: 12; anchors.bottom: parent.bottom; anchors.bottomMargin: 26; text: tc.meta.name; color: tc.sw[3]; font.weight: Font.DemiBold }
        Txt { x: 12; anchors.bottom: parent.bottom; anchors.bottomMargin: 8; width: parent.width - 24; text: tc.meta.description || ""; color: tc.sw[3]; opacity: 0.7; role: "small" }
    }

    component TokenRow: RowLayout {
        id: tr
        property var spec
        readonly property var val: Bus.tokens ? Bus.tokens[spec.key] : undefined
        // The number shown while a slider moves, until the daemon answers.
        property real liveNum: typeof val === "number" ? val : 0
        onValChanged: if (typeof val === "number") liveNum = val
        spacing: Theme.s3
        Rectangle { width: 6; height: 6; radius: 3; color: Bus.overridden(tr.spec.key) ? Theme.accent : "transparent" }
        Column {
            Layout.preferredWidth: 240
            Txt { text: tr.spec.label; width: 240 }
            Txt { text: tr.spec.key; role: "mono"; color: Theme.textMuted; width: 240 }
        }
        // Color.
        Row {
            visible: tr.spec.kind === "color"
            spacing: Theme.s2
            Rectangle {
                width: 30; height: 30; radius: Theme.radiusSm
                color: tr.spec.kind === "color" && tr.val ? (tr.val.length === 9 ? "#" + tr.val.substr(7, 2) + tr.val.substr(1, 6) : tr.val) : "transparent"
                border.width: 1; border.color: Theme.border
            }
            Field {
                width: 140
                accessibleName: tr.spec.label
                text: tr.spec.kind === "color" ? (tr.val || "") : ""
                onAccepted: Bus.setToken(tr.spec.key, text.trim())
            }
        }
        // Number.
        Slider {
            visible: tr.spec.kind === "number"
            Layout.fillWidth: true
            accessibleName: tr.spec.label
            from: tr.spec.min || 0; to: tr.spec.max || 1; stepSize: tr.spec.step || 0
            value: tr.liveNum
            onMoved: v => { tr.liveNum = v; numTimer.v = v; numTimer.restart(); }
            Timer { id: numTimer; property real v; interval: 250; onTriggered: Bus.setToken(tr.spec.key, v) }
        }
        Txt { id: numLive; visible: tr.spec.kind === "number"; text: Math.round(tr.liveNum * 100) / 100; role: "mono"; Layout.preferredWidth: 60 }
        // Bool.
        Btn {
            visible: tr.spec.kind === "bool"
            text: tr.val ? "On" : "Off"; variant: "outline"; active: tr.val === true
            checkable: true
            accessibleName: tr.spec.label
            onClicked: Bus.setToken(tr.spec.key, !tr.val)
        }
        // Enum.
        NavRow {
            visible: tr.spec.kind === "enum"
            spacing: Theme.s1
            Accessible.name: tr.spec.label
            Repeater {
                model: tr.spec.kind === "enum" ? tr.spec.options : []
                delegate: Btn { required property var modelData; text: modelData; variant: "outline"; checkable: true; active: tr.val === modelData
                    onClicked: Bus.setToken(tr.spec.key, modelData) }
            }
        }
        // Text.
        Field {
            visible: tr.spec.kind === "text"
            accessibleName: tr.spec.label
            Layout.preferredWidth: 220
            text: tr.spec.kind === "text" ? (tr.val || "") : ""
            onAccepted: Bus.setToken(tr.spec.key, text.trim())
        }
        Item { Layout.fillWidth: tr.spec.kind !== "number" }
        Btn {
            visible: Bus.overridden(tr.spec.key)
            text: "Reset"; variant: "ghost"
            accessibleName: Tr.t("Reset %1").arg(tr.spec.label)
            onClicked: Bus.act("theme.reset", { tokens: [tr.spec.key] })
        }
    }
}
