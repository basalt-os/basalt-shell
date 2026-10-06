pragma Singleton

import QtQuick
import Quickshell
import Quickshell.Io

// Design tokens of the login screen. The greeter has no daemon to ask: it
// reads the same theme files as the shell (/usr/share/basalt-shell/themes)
// and picks the theme and mode from /etc/basalt/greeter.conf (THEME=,
// MODE=), Basalt dark by default. Large text and high contrast are the
// person's accessibility choices (remembered in the greeter's state).
Singleton {
    id: g

    readonly property string shellData: Quickshell.env("BASALT_GREETER_SHELL_DATA") || "/usr/share/basalt-shell"
    property string themeId: "basalt"
    property string mode: "dark"
    property bool highContrast: false
    property bool largeText: false

    property var themeData: ({})
    function loadTheme() {
        let d = {};
        try { d = JSON.parse(themeFile.text()) || {}; } catch (e) { d = {}; }
        themeData = d;
    }
    onThemeIdChanged: loadTheme()
    Component.onCompleted: loadTheme()
    FileView {
        id: themeFile
        path: g.shellData + "/themes/" + g.themeId + ".json"
        blockLoading: true
        printErrors: false
        onLoaded: g.loadTheme()
    }

    readonly property var tk: themeData.tokens || ({})
    readonly property var palette: (g.mode === "light" ? themeData.light : themeData.dark) || ({})
    readonly property bool dark: highContrast || mode !== "light"

    function tok(k, d) { const v = tk[k]; return (v === undefined || v === null || v === "") ? d : v; }
    // Theme files store #RRGGBBAA; QML reads 8 digits as #AARRGGBB.
    function col(k, d) {
        const v = palette[k];
        if (typeof v !== "string" || v === "") return d;
        return v.length === 9 ? "#" + v.substr(7, 2) + v.substr(1, 6) : v;
    }

    // Colors (high contrast: white on black, yellow focus and accent).
    readonly property color bg: highContrast ? "#000000" : col("color.bg", "#111418")
    readonly property color surface: highContrast ? "#000000" : col("color.surface", "#181c21")
    readonly property color surfaceAlt: highContrast ? "#000000" : col("color.surfaceAlt", "#22272e")
    readonly property color border: highContrast ? "#ffffff" : col("color.border", "#343b44")
    readonly property color text: highContrast ? "#ffffff" : col("color.text", "#ece7e1")
    readonly property color textMuted: highContrast ? "#ffffff" : col("color.textMuted", "#a0a6ae")
    readonly property color accent: highContrast ? "#ffd400" : col("color.accent", "#b5502f")
    readonly property color accentText: highContrast ? "#000000" : col("color.accentText", "#ffffff")
    readonly property color warning: highContrast ? "#ffd400" : col("color.warning", "#d9a23a")
    readonly property color danger: highContrast ? "#ff6b6b" : col("color.danger", "#e0605a")
    readonly property color success: highContrast ? "#7dff9b" : col("color.success", "#5fae7b")
    readonly property color hover: Qt.rgba(text.r, text.g, text.b, highContrast ? 0.22 : 0.08)
    readonly property color pressed: Qt.rgba(text.r, text.g, text.b, highContrast ? 0.32 : 0.14)
    readonly property color accentSoft: Qt.rgba(accent.r, accent.g, accent.b, 0.18)
    // The focus ring: the accent moved toward white (dark) or black (light)
    // until it has 3:1 against the card, the field and the background
    // (WCAG 2.2); plain yellow in high contrast.
    readonly property color focusRing: highContrast ? accent : g.ensureContrast(accent, [bg, surface, surfaceAlt], 3.0)
    function luminance(c) {
        const f = v => v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
        return 0.2126 * f(c.r) + 0.7152 * f(c.g) + 0.0722 * f(c.b);
    }
    function contrast(a, b) {
        const la = luminance(a), lb = luminance(b);
        return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
    }
    function ensureContrast(c, backs, min) {
        const target = backs.reduce((s, b) => s + luminance(b), 0) / backs.length < 0.18 ? 1 : 0;
        let out = Qt.rgba(c.r, c.g, c.b, 1);
        for (let i = 0; i <= 20; i++) {
            if (backs.every(b => contrast(out, b) >= min)) return out;
            const k = (i + 1) / 20;
            out = Qt.rgba(c.r + (target - c.r) * k, c.g + (target - c.g) * k, c.b + (target - c.b) * k, 1);
        }
        return out;
    }
    // The card floats over the blurred wallpaper: slightly translucent.
    readonly property color card: highContrast ? "#000000" : Qt.rgba(surface.r, surface.g, surface.b, dark ? 0.82 : 0.88)
    readonly property real borderWidth: highContrast ? 2 : 1

    // Typography: the shell's base size, a little larger on the login
    // screen; large text multiplies everything by 1.4.
    readonly property string fontFamily: tok("font.family", "Inter")
    readonly property real textScale: largeText ? 1.4 : 1.0
    readonly property real fontSize: tok("font.size", 11) * 1.15 * textScale
    readonly property real ratio: tok("font.scale", 1.2)
    readonly property real fontSmall: Math.round(fontSize / ratio * 10) / 10
    readonly property real fontLarge: Math.round(fontSize * ratio * 10) / 10
    readonly property real fontTitle: Math.round(fontSize * ratio * ratio * 10) / 10
    readonly property real fontClock: Math.round(fontSize * 5.2 * 10) / 10

    // Shape and space.
    readonly property real radiusSm: tok("radius.sm", 6)
    readonly property real radiusMd: tok("radius.md", 10)
    readonly property real radiusLg: tok("radius.lg", 16)
    readonly property real unit: tok("spacing.unit", 4) * textScale
    readonly property real s1: unit
    readonly property real s2: unit * 2
    readonly property real s3: unit * 3
    readonly property real s4: unit * 4
    readonly property real s6: unit * 6
    readonly property real s8: unit * 8

    // Motion.
    readonly property int fast: tok("motion.fast", 120)
    readonly property int normal: tok("motion.normal", 200)
    readonly property int slow: tok("motion.slow", 320)
    readonly property int easing: {
        switch (tok("motion.easing", "OutCubic")) {
        case "OutQuart": return Easing.OutQuart;
        case "InOutQuad": return Easing.InOutQuad;
        case "Linear": return Easing.Linear;
        default: return Easing.OutCubic;
        }
    }

    function alpha(c, a) { return Qt.rgba(c.r, c.g, c.b, a); }
}
