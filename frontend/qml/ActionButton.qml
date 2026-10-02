import QtQuick
import QtQuick.Controls

// The store's button. kind:
//  - "primary": the page's main action, accent fill (accentFill/onAccent);
//  - "secondary": other actions, surface with a visible edge (3:1);
//  - "quiet": low-emphasis actions (Back, Cancel), text only until hovered.
// selected marks an on/off button that is on (e.g. starred): it takes the
// accent fill, so the state reads at a glance and not only from the label.
// Disabled buttons keep a readable label (muted, 7:1) on the plain surface
// with a quiet edge: no fill, so they never look like an available action.
Button {
    id: control
    property string kind: "secondary"
    property bool selected: false
    readonly property bool filled: kind === "primary" || selected

    Accessible.name: text
    font.family: theme.fontFamily
    font.pixelSize: theme.fontBody
    font.weight: filled ? Font.DemiBold : Font.Medium
    leftPadding: kind === "quiet" ? theme.spaceS : theme.spaceL
    rightPadding: leftPadding
    topPadding: theme.spaceS
    bottomPadding: theme.spaceS
    hoverEnabled: true

    contentItem: Text {
        text: control.text
        font: control.font
        color: !control.enabled ? theme.muted
             : control.filled ? theme.onAccent
             : control.kind === "quiet" ? theme.accent : theme.foreground
        horizontalAlignment: Text.AlignHCenter
        verticalAlignment: Text.AlignVCenter
        elide: Text.ElideRight
    }
    background: Rectangle {
        implicitWidth: control.kind === "quiet" ? 0 : 96
        implicitHeight: Math.max(38, control.font.pixelSize * 2.6)
        radius: theme.radiusS
        color: !control.enabled ? (control.kind === "quiet" ? "transparent" : theme.surface)
             : control.filled ? theme.accentFill
             : control.kind === "quiet" ? (control.hovered || control.down ? theme.hover : "transparent")
             : (control.hovered || control.down ? theme.hover : theme.surface)
        // Focus: a 3:1 ring; hover on a fill: an edge in the text color, so
        // the fill itself never changes and its 7:1 holds.
        border.color: control.visualFocus ? theme.focus
                    : !control.enabled ? theme.outline
                    : control.filled ? theme.onAccent
                    : theme.border
        border.width: control.visualFocus ? 3
                    : control.kind === "quiet" ? 0
                    : !control.enabled ? 1
                    : control.filled ? (control.hovered || control.down ? 1 : 0)
                    : 1
        Behavior on color { ColorAnimation { duration: theme.durationShort } }
    }
}
