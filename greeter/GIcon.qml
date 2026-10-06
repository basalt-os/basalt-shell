import QtQuick

// Line icons drawn as SVG with the color baked in (no shader effects, so
// they also draw with the software renderer). The same drawing style as
// the shell's icons.
Item {
    id: root
    property string name: "info"
    property color color: G.text
    property real size: 18
    property real stroke: 1.8
    implicitWidth: size
    implicitHeight: size
    Accessible.ignored: true

    readonly property var paths: ({
        "user": '<circle cx="12" cy="8.5" r="4"/><path d="M4.5 20.5a7.5 7.5 0 0 1 15 0"/>',
        "user-plus": '<circle cx="10" cy="8.5" r="4"/><path d="M2.5 20.5a7.5 7.5 0 0 1 15 0"/><path d="M19 7v6M16 10h6"/>',
        "lock": '<rect x="5" y="10.5" width="14" height="10" rx="2.5"/><path d="M8 10.5V8a4 4 0 0 1 8 0v2.5"/>',
        "eye": '<path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z"/><circle cx="12" cy="12" r="3"/>',
        "eye-off": '<path d="M2.5 12S6 5.5 12 5.5 21.5 12 21.5 12 18 18.5 12 18.5 2.5 12 2.5 12z"/><circle cx="12" cy="12" r="3"/><path d="M4 4l16 16"/>',
        "arrow": '<path d="M5 12h14M13 6l6 6-6 6"/>',
        "back": '<path d="M19 12H5M11 6l-6 6 6 6"/>',
        "chevron": '<path d="M6 9l6 6 6-6"/>',
        "check": '<path d="M5 12.5l4.5 4.5L19 7"/>',
        "power": '<path d="M12 3v8"/><path d="M7 6.5a7 7 0 1 0 10 0"/>',
        "restart": '<path d="M4.5 12a7.5 7.5 0 1 0 2.2-5.3"/><path d="M4 4v4.5h4.5"/>',
        "moon": '<path d="M20 14.5A8 8 0 1 1 9.5 4a6.5 6.5 0 0 0 10.5 10.5z"/>',
        "keyboard": '<rect x="2.5" y="6" width="19" height="12" rx="2"/><path d="M6 10h.5M9.5 10h.5M13 10h.5M16.5 10h1M6 14h.5M9 14h6M17.5 14h.5"/>',
        "globe": '<circle cx="12" cy="12" r="9"/><path d="M3 12h18M12 3a14 14 0 0 1 0 18M12 3a14 14 0 0 0 0 18"/>',
        "a11y": '<circle cx="12" cy="4.5" r="1.6"/><path d="M5 8.5l7 1.5 7-1.5M12 10v4.5l-3 6M12 14.5l3 6"/>',
        "text": '<path d="M4 18L9 6l5 12M5.8 14h6.4M15.5 18l3-7.5 3 7.5M16.4 15.8h4.2"/>',
        "contrast": '<circle cx="12" cy="12" r="8.5"/><path d="M12 3.5v17a8.5 8.5 0 0 0 0-17z" fill="CURRENT"/>',
        "wifi": '<path d="M2.5 9a14 14 0 0 1 19 0"/><path d="M5.5 12.5a9.5 9.5 0 0 1 13 0"/><path d="M8.7 16a5 5 0 0 1 6.6 0"/><circle cx="12" cy="19.2" r="0.9"/>',
        "network": '<rect x="9" y="3" width="6" height="5" rx="1"/><rect x="3" y="16" width="6" height="5" rx="1"/><rect x="15" y="16" width="6" height="5" rx="1"/><path d="M12 8v4M6 16v-4h12v4"/>',
        "offline": '<path d="M2.5 9a14 14 0 0 1 19 0"/><path d="M8.7 16a5 5 0 0 1 6.6 0"/><path d="M4 4l16 16"/>',
        "battery": '<rect x="3" y="7" width="16" height="10" rx="2"/><path d="M21 10.5v3"/>',
        "charging": '<rect x="3" y="7" width="16" height="10" rx="2"/><path d="M21 10.5v3"/><path d="M12 8.5l-3 4h4l-3 3"/>',
        "warning": '<path d="M12 4l9 16H3z"/><path d="M12 10v4"/><circle cx="12" cy="17" r="0.8"/>',
        "info": '<circle cx="12" cy="12" r="9"/><path d="M12 11v5"/><circle cx="12" cy="8" r="0.8"/>',
        "spinner": '<path d="M12 3a9 9 0 1 1-9 9"/>',
        "session": '<rect x="3" y="5" width="18" height="14" rx="2.5"/><path d="M3 9.5h18"/>',
        "logo": '<path d="M20.9 6.7L21.6 16.1L13.4 22.1L3.4 17.8L2.6 7.7L12.5 2.2" stroke-width="2.6"/><path d="M12 8.4L15.4 11L14.2 15.1L9.6 14.9L8.6 10.8Z" fill="ACCENT" stroke="none"/>'
    })

    readonly property string svg: {
        const c = "" + root.color;
        const body = (paths[name] || paths["info"]).replace(/ACCENT/g, "" + G.accent).replace(/CURRENT/g, c);
        return '<svg xmlns="http://www.w3.org/2000/svg" width="24" height="24" viewBox="0 0 24 24" fill="none" stroke="' + c +
               '" stroke-width="' + root.stroke + '" stroke-linecap="round" stroke-linejoin="round">' + body + '</svg>';
    }

    Image {
        anchors.fill: parent
        sourceSize.width: Math.ceil(root.size * 2)
        sourceSize.height: Math.ceil(root.size * 2)
        source: "data:image/svg+xml;charset=utf-8," + encodeURIComponent(root.svg)
        smooth: true
        mipmap: true
    }
}
