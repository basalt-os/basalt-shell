import QtQuick

// Text in the login screen's typography. Plain text only.
Text {
    textFormat: Text.PlainText
    property string role: "body" // body, small, large, title, clock
    color: G.text
    font.family: G.fontFamily
    font.pointSize: {
        switch (role) {
        case "small": return G.fontSmall;
        case "large": return G.fontLarge;
        case "title": return G.fontTitle;
        case "clock": return G.fontClock;
        default: return G.fontSize;
        }
    }
    font.weight: role === "title" ? Font.DemiBold : (role === "clock" ? Font.Light : Font.Normal)
    elide: Text.ElideRight
    verticalAlignment: Text.AlignVCenter
    Behavior on color { ColorAnimation { duration: G.normal; easing.type: G.easing } }
}
