import QtQuick
import QtQuick.Layouts
import Quickshell

// The settings window: a normal (floating) window so it behaves like any
// app. Every token is editable here; simple controls on the Appearance
// page, the full token list on the Tokens page. Changes apply at once.
FloatingWindow {
    id: win
    visible: Ui.settings
    title: "Basalt Settings"
    implicitWidth: 980
    implicitHeight: 700
    color: Theme.surface
    onVisibleChanged: if (!visible) Ui.settings = false

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
        { id: "ai", label: "Assistant and AI", icon: "spark" },
        { id: "about", label: "About", icon: "info" }
    ]

    Behavior on color { ColorAnimation { duration: Theme.normal; easing.type: Theme.easing } }

    RowLayout {
        anchors.fill: parent
        spacing: 0

        // Sidebar.
        Rectangle {
            Layout.fillHeight: true
            Layout.preferredWidth: 230
            color: Theme.bg
            Behavior on color { ColorAnimation { duration: Theme.normal } }
            ColumnLayout {
                anchors.fill: parent
                anchors.margins: Theme.s4
                spacing: Theme.s1
                Row {
                    spacing: Theme.s2
                    bottomPadding: Theme.s4
                    Icon { name: "logo"; size: Theme.fontTitle * 1.6; anchors.verticalCenter: parent.verticalCenter }
                    Txt { text: "Settings"; role: "title"; anchors.verticalCenter: parent.verticalCenter }
                }
                Repeater {
                    model: win.pages
                    delegate: Btn {
                        required property var modelData
                        Layout.fillWidth: true
                        icon: modelData.icon
                        text: modelData.label
                        active: Ui.settingsPage === modelData.id
                        onClicked: Ui.settingsPage = modelData.id
                    }
                }
                Item { Layout.fillHeight: true }
                Txt { text: Bus.connected ? "Connected to basalt-shell " + Bus.version : "Daemon not running"; role: "small"; color: Bus.connected ? Theme.textMuted : Theme.danger; Layout.fillWidth: true }
            }
        }

        Flickable {
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

                // Appearance.
                ColumnLayout {
                    visible: Ui.settingsPage === "appearance"
                    Layout.fillWidth: true
                    spacing: Theme.s4
                    Txt { text: "Appearance"; role: "display" }
                    Txt { text: "Pick a theme, then make it yours. Everything below also changes your apps (GTK, libadwaita, Qt) and window borders."; color: Theme.textMuted; wrapMode: Text.Wrap; Layout.fillWidth: true; elide: Text.ElideNone }

                    Flow {
                        Layout.fillWidth: true
                        spacing: Theme.s3
                        Repeater {
                            model: win.st ? win.st.themes : []
                            delegate: ThemeCard { required property var modelData; meta: modelData }
                        }
                    }

                    Section { title: "Mode" }
                    Row {
                        spacing: Theme.s2
                        Btn { text: "Light"; icon: "sun"; variant: "outline"; active: !Theme.dark; onClicked: Bus.act("theme.switch", { mode: "light" }) }
                        Btn { text: "Dark"; icon: "moon"; variant: "outline"; active: Theme.dark; onClicked: Bus.act("theme.switch", { mode: "dark" }) }
                    }

                    Section { title: "Accent color" }
                    Row {
                        spacing: Theme.s2
                        Repeater {
                            model: ["#a3472e", "#c0392b", "#d9772b", "#c99a06", "#3f8f4f", "#2f7d78", "#3b6fd1", "#7b4fc9", "#c4497f", "#5f6f7f"]
                            delegate: Rectangle {
                                required property var modelData
                                width: 30; height: 30; radius: 15
                                color: modelData
                                border.width: ("" + Theme.accent) === modelData ? 3 : 0
                                border.color: Theme.text
                                MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: win.setTokens({ "color.accent": modelData }) }
                            }
                        }
                    }

                    Section { title: "Shape and size" }
                    LabeledSlider {
                        label: "Corner roundness"; from: 0; to: 24; stepSize: 1; value: Theme.radiusMd; suffix: " px"
                        onCommitted: v => win.setTokens({ "radius.sm": Math.round(v * 0.6), "radius.md": v, "radius.lg": Math.min(40, Math.round(v * 1.6)), "radius.window": v })
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
                    Row {
                        spacing: Theme.s2
                        Txt { text: "Panel"; width: 160; anchors.verticalCenter: parent.verticalCenter }
                        Btn { text: "Top"; variant: "outline"; active: Theme.panelPosition === "top"; onClicked: win.setTokens({ "panel.position": "top" }) }
                        Btn { text: "Bottom"; variant: "outline"; active: Theme.panelPosition === "bottom"; onClicked: win.setTokens({ "panel.position": "bottom" }) }
                    }

                    Section { title: "Keep this look" }
                    Row {
                        spacing: Theme.s2
                        Field { id: saveName; width: 260; placeholder: "Name for a new theme" }
                        Btn {
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
                    Row {
                        spacing: Theme.s2
                        Repeater {
                            model: [["auto", "Auto"], ["full", "Full"], ["reduced", "Reduced"]]
                            delegate: Btn {
                                required property var modelData
                                text: modelData[1]; variant: "outline"
                                active: win.settings && win.settings.motion === modelData[0]
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
                        property bool right: false
                        Rectangle {
                            width: 44; height: 44; radius: Theme.radiusSm; color: Theme.accent
                            anchors.verticalCenter: parent.verticalCenter
                            x: demo.right ? demo.width - width - 8 : 8
                            Behavior on x { NumberAnimation { duration: Theme.slow; easing.type: Theme.easing } }
                        }
                        Timer { interval: 1200; running: Ui.settings && Ui.settingsPage === "motion"; repeat: true; onTriggered: demo.right = !demo.right }
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
                    Row {
                        spacing: Theme.s2
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
                          Txt { text: Bus.translatorAvailable ? "Command bar: local language model (with rules as fallback)" : "Command bar: rules (no local model configured)" } }
                    Row { spacing: Theme.s2; Icon { name: Bus.assistantAvailable ? "check" : "info"; color: Bus.assistantAvailable ? Theme.success : Theme.textMuted }
                          Txt { text: Bus.assistantAvailable ? "System assistant (basalt) installed: system questions and its proposals work here" : "System assistant (basalt) not installed" } }
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
        property real live: value
        signal committed(real v)
        Layout.fillWidth: true
        spacing: Theme.s3
        onValueChanged: live = value
        Txt { text: ls.label; Layout.preferredWidth: 160 }
        Slider {
            Layout.fillWidth: true
            from: ls.from; to: ls.to; stepSize: ls.stepSize; value: ls.live
            onMoved: v => { ls.live = v; commitTimer.restart(); }
        }
        Txt { text: (Math.round(ls.live * 100) / 100) + ls.suffix; Layout.preferredWidth: 90; role: "mono" }
        Timer { id: commitTimer; interval: 250; onTriggered: ls.committed(ls.live) }
    }

    component ThemeCard: Rectangle {
        id: tc
        property var meta
        readonly property var sw: Theme.dark ? meta.dark : meta.light
        width: 200; height: 128
        radius: Theme.radiusLg
        color: sw[0]
        border.width: Theme.themeId === meta.id ? 3 : 1
        border.color: Theme.themeId === meta.id ? Theme.accent : Theme.border
        Behavior on border.color { ColorAnimation { duration: Theme.normal } }
        Rectangle { x: 12; y: 12; width: parent.width - 24; height: 16; radius: 5; color: tc.sw[1] }
        Rectangle { x: 12; y: 36; width: 70; height: 34; radius: 6; color: tc.sw[1] }
        Rectangle { x: 90; y: 36; width: 40; height: 14; radius: 7; color: tc.sw[2] }
        Txt { x: 12; anchors.bottom: parent.bottom; anchors.bottomMargin: 26; text: tc.meta.name; color: tc.sw[3]; font.weight: Font.DemiBold }
        Txt { x: 12; anchors.bottom: parent.bottom; anchors.bottomMargin: 8; width: parent.width - 24; text: tc.meta.description || ""; color: tc.sw[3]; opacity: 0.7; role: "small" }
        MouseArea { anchors.fill: parent; cursorShape: Qt.PointingHandCursor; onClicked: Bus.act("theme.switch", { theme: tc.meta.id }) }
    }

    component TokenRow: RowLayout {
        id: tr
        property var spec
        readonly property var val: Bus.tokens ? Bus.tokens[spec.key] : undefined
        spacing: Theme.s3
        Rectangle { width: 6; height: 6; radius: 3; color: win.overridden(tr.spec.key) ? Theme.accent : "transparent" }
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
                text: tr.spec.kind === "color" ? (tr.val || "") : ""
                onAccepted: { const t = {}; t[tr.spec.key] = text.trim(); win.setTokens(t); }
            }
        }
        // Number.
        Slider {
            visible: tr.spec.kind === "number"
            Layout.fillWidth: true
            from: tr.spec.min || 0; to: tr.spec.max || 1; stepSize: tr.spec.step || 0
            value: typeof tr.val === "number" ? tr.val : 0
            onMoved: v => { numLive.text = Math.round(v * 100) / 100; numTimer.v = v; numTimer.restart(); }
            Timer { id: numTimer; property real v; interval: 250; onTriggered: { const t = {}; t[tr.spec.key] = v; win.setTokens(t); } }
        }
        Txt { id: numLive; visible: tr.spec.kind === "number"; text: typeof tr.val === "number" ? Math.round(tr.val * 100) / 100 : ""; role: "mono"; Layout.preferredWidth: 60 }
        // Bool.
        Btn {
            visible: tr.spec.kind === "bool"
            text: tr.val ? "On" : "Off"; variant: "outline"; active: tr.val === true
            onClicked: { const t = {}; t[tr.spec.key] = !tr.val; win.setTokens(t); }
        }
        // Enum.
        Row {
            visible: tr.spec.kind === "enum"
            spacing: Theme.s1
            Repeater {
                model: tr.spec.kind === "enum" ? tr.spec.options : []
                delegate: Btn { required property var modelData; text: modelData; variant: "outline"; active: tr.val === modelData
                    onClicked: { const t = {}; t[tr.spec.key] = modelData; win.setTokens(t); } }
            }
        }
        // Text.
        Field {
            visible: tr.spec.kind === "text"
            Layout.preferredWidth: 220
            text: tr.spec.kind === "text" ? (tr.val || "") : ""
            onAccepted: { const t = {}; t[tr.spec.key] = text.trim(); win.setTokens(t); }
        }
        Item { Layout.fillWidth: tr.spec.kind !== "number" }
        Btn {
            visible: win.overridden(tr.spec.key)
            text: "Reset"; variant: "ghost"
            onClicked: Bus.act("theme.reset", { tokens: [tr.spec.key] })
        }
    }
}
