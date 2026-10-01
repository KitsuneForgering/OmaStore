import QtQuick
import QtQuick.Controls

// Main action button, in the theme's accent color. Fill and text keep 7:1
// (theme.accentFill/onAccent); hover and keyboard focus draw a ring in the
// text color instead of shifting the fill, which would lower the contrast.
Button {
    id: control
    Accessible.name: text
    contentItem: Text {
        text: control.text
        font: control.font
        color: control.enabled ? theme.onAccent : theme.muted
        horizontalAlignment: Text.AlignHCenter
        verticalAlignment: Text.AlignVCenter
    }
    background: Rectangle {
        implicitWidth: 120
        implicitHeight: 38
        radius: 6
        color: control.enabled ? theme.accentFill : theme.selection
        border.color: control.enabled ? theme.onAccent : theme.border
        border.width: control.visualFocus ? 3 : (control.down || control.hovered) && control.enabled ? 1 : 0
        Behavior on color { ColorAnimation { duration: theme.durationShort } }
    }
}
