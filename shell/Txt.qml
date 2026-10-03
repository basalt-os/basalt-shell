import QtQuick

// Text in the theme's typography.
Text {
    property string role: "body" // body, small, large, title, display, mono
    color: Theme.text
    font.family: role === "mono" ? Theme.fontMono : Theme.fontFamily
    font.pointSize: {
        switch (role) {
        case "small": return Theme.fontSmall;
        case "large": return Theme.fontLarge;
        case "title": return Theme.fontTitle;
        case "display": return Theme.fontDisplay;
        case "mono": return Theme.fontSmall;
        default: return Theme.fontSize;
        }
    }
    font.weight: role === "title" || role === "display" ? Font.DemiBold : Font.Normal
    elide: Text.ElideRight
    verticalAlignment: Text.AlignVCenter
    Behavior on color { ColorAnimation { duration: Theme.normal; easing.type: Theme.easing } }
}
