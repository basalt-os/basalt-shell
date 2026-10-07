import QtQuick

// Small line icons drawn as SVG with the theme color baked in, so they
// follow light/dark and accent changes without shader effects (which the
// software renderer of weak machines does not support).
Item {
    id: root
    property string name: "apps"
    property color color: Theme.text
    property real size: 18
    property real stroke: 1.8
    implicitWidth: size
    implicitHeight: size

    readonly property var paths: ({
        "apps": '<rect x="4" y="4" width="7" height="7" rx="2"/><rect x="13" y="4" width="7" height="7" rx="2"/><rect x="4" y="13" width="7" height="7" rx="2"/><rect x="13" y="13" width="7" height="7" rx="2"/>',
        "spark": '<path d="M12 3l1.8 7.2L21 12l-7.2 1.8L12 21l-1.8-7.2L3 12l7.2-1.8z"/>',
        "bell": '<path d="M6 16v-5a6 6 0 0 1 12 0v5l1.5 2h-15z"/><path d="M10 20.5a2 2 0 0 0 4 0"/>',
        "wifi": '<path d="M2.5 9a14 14 0 0 1 19 0"/><path d="M5.5 12.5a9.5 9.5 0 0 1 13 0"/><path d="M8.7 16a5 5 0 0 1 6.6 0"/><circle cx="12" cy="19.2" r="0.9"/>',
        "network": '<rect x="9" y="3" width="6" height="5" rx="1"/><rect x="3" y="16" width="6" height="5" rx="1"/><rect x="15" y="16" width="6" height="5" rx="1"/><path d="M12 8v4M6 16v-4h12v4"/>',
        "offline": '<path d="M2.5 9a14 14 0 0 1 19 0"/><path d="M8.7 16a5 5 0 0 1 6.6 0"/><path d="M4 4l16 16"/>',
        "volume": '<path d="M4 9h4l5-4v14l-5-4H4z"/><path d="M16 9a4 4 0 0 1 0 6"/><path d="M18.5 6.5a8 8 0 0 1 0 11"/>',
        "mute": '<path d="M4 9h4l5-4v14l-5-4H4z"/><path d="M16 9.5l5 5M21 9.5l-5 5"/>',
        "battery": '<rect x="3" y="7" width="16" height="10" rx="2"/><path d="M21 10.5v3"/>',
        "charging": '<rect x="3" y="7" width="16" height="10" rx="2"/><path d="M21 10.5v3"/><path d="M12 8.5l-3 4h4l-3 3"/>',
        "sun": '<circle cx="12" cy="12" r="4"/><path d="M12 2.5v2M12 19.5v2M2.5 12h2M19.5 12h2M5.3 5.3l1.4 1.4M17.3 17.3l1.4 1.4M5.3 18.7l1.4-1.4M17.3 6.7l1.4-1.4"/>',
        "moon": '<path d="M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z"/>',
        "sliders": '<path d="M4 6h9M17 6h3M4 12h3M11 12h9M4 18h11M19 18h1"/><circle cx="15" cy="6" r="2"/><circle cx="9" cy="12" r="2"/><circle cx="17" cy="18" r="2"/>',
        "close": '<path d="M6 6l12 12M18 6L6 18"/>',
        "check": '<path d="M5 12.5l4.5 4.5L19 7"/>',
        "download": '<path d="M12 4v11"/><path d="M7 10.5l5 5 5-5"/><path d="M5 20h14"/>',
        "list": '<path d="M9 6h11M9 12h11M9 18h11"/><circle cx="4.5" cy="6" r="0.9"/><circle cx="4.5" cy="12" r="0.9"/><circle cx="4.5" cy="18" r="0.9"/>',
        "search": '<circle cx="11" cy="11" r="6.5"/><path d="M20 20l-4.3-4.3"/>',
        "power": '<path d="M12 3v8"/><path d="M7 6.5a7 7 0 1 0 10 0"/>',
        "lock": '<rect x="5" y="11" width="14" height="10" rx="2"/><path d="M8 11V8a4 4 0 0 1 8 0v3"/>',
        "logout": '<path d="M14 4H6a2 2 0 0 0-2 2v12a2 2 0 0 0 2 2h8"/><path d="M10 12h10M16 8l4 4-4 4"/>',
        "restart": '<path d="M4 12a8 8 0 1 0 2.4-5.7"/><path d="M4 4v4.5h4.5"/>',
        "palette": '<path d="M12 3a9 9 0 1 0 0 18c1.4 0 2-0.9 2-1.9 0-1-1-1.6-1-2.6 0-1 0.9-1.5 1.9-1.5h2.1a4 4 0 0 0 4-4C21 6.5 17 3 12 3z"/><circle cx="7.5" cy="11" r="0.9"/><circle cx="10" cy="7" r="0.9"/><circle cx="14.5" cy="7" r="0.9"/>',
        "motion": '<path d="M3 12c3-6 6-6 9 0s6 6 9 0"/>',
        "window": '<rect x="3" y="5" width="18" height="14" rx="2.5"/><path d="M3 9.5h18"/>',
        "shield": '<path d="M12 3l8 3v6c0 5-3.5 8-8 9-4.5-1-8-4-8-9V6z"/>',
        "undo": '<path d="M9 14L4 9l5-5"/><path d="M4 9h10a6 6 0 0 1 0 12h-3"/>',
        "plus": '<path d="M12 5v14M5 12h14"/>',
        "key": '<circle cx="8" cy="15" r="4"/><path d="M11 12l9-9M17 6l3 3M15 8l2 2"/>',
        "box": '<path d="M3.5 7.5L12 3l8.5 4.5v9L12 21l-8.5-4.5z"/><path d="M3.5 7.5L12 12l8.5-4.5M12 12v9"/>',
        "arrow": '<path d="M5 12h14M13 6l6 6-6 6"/>',
        "terminal": '<path d="M4 17l6-5-6-5M12 19h8"/>',
        "chip": '<rect x="6" y="6" width="12" height="12" rx="2"/><rect x="9.5" y="9.5" width="5" height="5" rx="1"/><path d="M9 3v3M15 3v3M9 18v3M15 18v3M3 9h3M3 15h3M18 9h3M18 15h3"/>',
        "chevron": '<path d="M6 9l6 6 6-6"/>',
        "up": '<path d="M6 15l6-6 6 6"/>',
        "keyboard": '<rect x="2.5" y="6" width="19" height="12" rx="2"/><path d="M6.5 10h.01M10 10h.01M13.5 10h.01M17 10h.01M8 14h8"/>',
        "warning": '<path d="M12 4l9 16H3z"/><path d="M12 10v4"/><circle cx="12" cy="17" r="0.8"/>',
        "info": '<circle cx="12" cy="12" r="9"/><path d="M12 11v5"/><circle cx="12" cy="8" r="0.8"/>',
        "screen": '<rect x="3" y="4" width="18" height="12" rx="2"/><path d="M8 20h8M12 16v4"/>',
        "mic": '<rect x="9" y="3" width="6" height="11" rx="3"/><path d="M5.5 11a6.5 6.5 0 0 0 13 0M12 17.5V21"/>',
        "keyboard": '<rect x="2.5" y="6" width="19" height="12" rx="2"/><path d="M6 10h.5M9.5 10h.5M13 10h.5M16.5 10h1M6 14h.5M9 14h6M17.5 14h.5"/>',
        "eye": '<path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z"/><circle cx="12" cy="12" r="3"/>',
        "eye-off": '<path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z"/><circle cx="12" cy="12" r="3"/><path d="M4 4l16 16"/>',
        "spinner": '<path d="M12 3a9 9 0 1 1-9 9"/>',
        "logo": '<path d="M20.9 6.7L21.6 16.1L13.4 22.1L3.4 17.8L2.6 7.7L12.5 2.2" stroke-width="2.6"/><path d="M12 8.4L15.4 11L14.2 15.1L9.6 14.9L8.6 10.8Z" fill="ACCENT" stroke="none"/>'
    })

    readonly property string svg: {
        const c = "" + root.color;
        const body = (paths[name] || paths["info"]).replace(/ACCENT/g, "" + Theme.accent);
        return '<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="' + c +
               '" stroke-width="' + root.stroke + '" stroke-linecap="round" stroke-linejoin="round">' + body + '</svg>';
    }

    Image {
        anchors.fill: parent
        sourceSize.width: Math.ceil(root.size * 2)
        sourceSize.height: Math.ceil(root.size * 2)
        source: "data:image/svg+xml;base64," + Qt.btoa(root.svg)
        smooth: true
        mipmap: true
    }
}
