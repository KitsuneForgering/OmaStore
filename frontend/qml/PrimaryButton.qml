import QtQuick
import QtQuick.Controls

// Main action button, in the theme's accent color.
Button {
    id: control
    contentItem: Text {
        text: control.text
        font: control.font
        color: theme.background
        horizontalAlignment: Text.AlignHCenter
        verticalAlignment: Text.AlignVCenter
        opacity: control.enabled ? 1 : 0.6
    }
    background: Rectangle {
        implicitWidth: 120
        implicitHeight: 38
        radius: 6
        color: !control.enabled ? theme.muted
             : control.down ? Qt.darker(theme.accent, 1.3)
             : control.hovered ? Qt.lighter(theme.accent, 1.15) : theme.accent
    }
}
