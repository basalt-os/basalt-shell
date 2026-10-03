pragma Singleton

import QtQuick
import Quickshell

// Design tokens as QML properties. Values come from the daemon (one theme
// file + mode + the user's overrides, resolved); the defaults below are
// the Basalt dark theme, used until the daemon answers. Every component
// binds to these properties, so a token change restyles the shell live.
Singleton {
    id: t
    readonly property var tk: Bus.tokens || ({})

    function str(k, d) { const v = t.tk[k]; return (v === undefined || v === null || v === "") ? d : v; }
    function num(k, d) { const v = t.tk[k]; return (typeof v === "number") ? v : d; }
    // Tokens store colors as #RRGGBB or #RRGGBBAA; QML reads 8 digits as
    // #AARRGGBB, so the alpha moves to the front.
    function col(k, d) {
        const v = str(k, d);
        return (typeof v === "string" && v.length === 9) ? "#" + v.substr(7, 2) + v.substr(1, 6) : v;
    }
    function bool(k, d) { const v = t.tk[k]; return (typeof v === "boolean") ? v : d; }

    readonly property string themeId: str("theme", "basalt")
    readonly property string mode: str("mode", "dark")
    readonly property bool dark: mode === "dark"

    // Colors.
    readonly property color bg: col("color.bg", "#111418")
    readonly property color surface: col("color.surface", "#181c21")
    readonly property color surfaceAlt: col("color.surfaceAlt", "#22272e")
    readonly property color border: col("color.border", "#343b44")
    readonly property color text: col("color.text", "#ece7e1")
    readonly property color textMuted: col("color.textMuted", "#a0a6ae")
    readonly property color accent: col("color.accent", "#b5502f")
    readonly property color accentText: col("color.accentText", "#ffffff")
    readonly property color success: col("color.success", "#5fae7b")
    readonly property color warning: col("color.warning", "#d9a23a")
    readonly property color danger: col("color.danger", "#e0605a")
    readonly property color scrim: col("color.scrim", "#0a0c0fb3")
    // Hover and pressed overlays derived from the text color.
    readonly property color hover: Qt.rgba(text.r, text.g, text.b, 0.08)
    readonly property color pressed: Qt.rgba(text.r, text.g, text.b, 0.14)
    readonly property color accentSoft: Qt.rgba(accent.r, accent.g, accent.b, 0.16)

    // Typography (points).
    readonly property string fontFamily: str("font.family", "Inter")
    readonly property string fontMono: str("font.mono", "JetBrains Mono")
    readonly property real fontSize: num("font.size", 11)
    readonly property real scale: num("font.scale", 1.2)
    readonly property real fontSmall: Math.round(fontSize / scale * 10) / 10
    readonly property real fontLarge: Math.round(fontSize * scale * 10) / 10
    readonly property real fontTitle: Math.round(fontSize * scale * scale * 10) / 10
    readonly property real fontDisplay: Math.round(fontSize * scale * scale * scale * 10) / 10

    // Shape and space.
    readonly property real radiusSm: num("radius.sm", 6)
    readonly property real radiusMd: num("radius.md", 10)
    readonly property real radiusLg: num("radius.lg", 16)
    readonly property real unit: num("spacing.unit", 4)
    readonly property real s1: unit
    readonly property real s2: unit * 2
    readonly property real s3: unit * 3
    readonly property real s4: unit * 4
    readonly property real s6: unit * 6
    readonly property real panelHeight: num("panel.height", 36)
    readonly property string panelPosition: str("panel.position", "top")
    readonly property real panelOpacity: num("panel.opacity", 0.92)

    // Elevation.
    readonly property real shadowStrength: num("elevation.shadow", 0.35)
    readonly property real shadowBlur: num("elevation.blur", 24)

    // Motion: durations are 0 when motion is reduced (by the person, or
    // automatically on weak hardware), which turns every Behavior off.
    readonly property string motion: str("motion", "full")
    readonly property bool animate: motion === "full"
    readonly property int fast: num("motion.fast", 120)
    readonly property int normal: num("motion.normal", 200)
    readonly property int slow: num("motion.slow", 320)
    readonly property int easing: {
        switch (str("motion.easing", "OutCubic")) {
        case "OutQuart": return Easing.OutQuart;
        case "InOutQuad": return Easing.InOutQuad;
        case "OutBack": return Easing.OutBack;
        case "Linear": return Easing.Linear;
        default: return Easing.OutCubic;
        }
    }

    function alpha(c, a) { return Qt.rgba(c.r, c.g, c.b, a); }
}
