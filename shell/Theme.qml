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

    // The keyboard focus ring (2 px, 2 px away from the control): the
    // accent used as a foreground (color.accentFg when a theme defines it),
    // moved toward white in dark mode or black in light mode until it has
    // at least 3:1 against every surface a control sits on (WCAG 2.2,
    // 1.4.11 and 2.4.13), whatever accent the person picks.
    readonly property color focusRing: {
        const base = t.tk["color.accentFg"] ? col("color.accentFg", "#e2865f") : accent;
        return t.ensureContrast(base, [bg, surface, surfaceAlt], 3.0);
    }
    readonly property real focusWidth: 2
    readonly property real focusOffset: 2

    // Relative luminance and contrast ratio (WCAG 2.2).
    function luminance(c) {
        const f = v => v <= 0.04045 ? v / 12.92 : Math.pow((v + 0.055) / 1.055, 2.4);
        return 0.2126 * f(c.r) + 0.7152 * f(c.g) + 0.0722 * f(c.b);
    }
    function contrast(a, b) {
        const la = luminance(a), lb = luminance(b);
        return (Math.max(la, lb) + 0.05) / (Math.min(la, lb) + 0.05);
    }
    // ensureContrast mixes c toward white (dark backgrounds) or black
    // (light ones) in small steps until it reaches min against all of them.
    function ensureContrast(c, backs, min) {
        const darkBacks = backs.reduce((s, b) => s + luminance(b), 0) / backs.length < 0.18;
        const target = darkBacks ? 1 : 0;
        let out = Qt.rgba(c.r, c.g, c.b, 1);
        for (let i = 0; i <= 20; i++) {
            if (backs.every(b => contrast(out, b) >= min)) return out;
            const k = (i + 1) / 20;
            out = Qt.rgba(c.r + (target - c.r) * k, c.g + (target - c.g) * k, c.b + (target - c.b) * k, 1);
        }
        return out;
    }

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
