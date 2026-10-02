import QtQuick

// Small status label ("installed", "update available"). The text uses a theme
// text color (7:1 on every background) and the pill is only an outline in the
// same tone, so it reads on cards, hover and selection alike.
Rectangle {
    id: badge
    property string text: ""
    // "neutral", "success", "warning" or "accent"
    property string tone: "neutral"
    readonly property color toneColor: tone === "success" ? theme.success
                                     : tone === "warning" ? theme.warning
                                     : tone === "accent" ? theme.accent : theme.muted

    implicitWidth: label.implicitWidth + theme.spaceS * 2
    implicitHeight: label.implicitHeight + theme.spaceXs
    radius: height / 2
    color: "transparent"
    border.color: toneColor
    border.width: 1
    Accessible.role: Accessible.StaticText
    Accessible.name: text

    Text {
        id: label
        anchors.centerIn: parent
        text: badge.text
        color: badge.toneColor
        font.family: theme.fontFamily
        font.pixelSize: theme.fontCaption
        font.weight: Font.DemiBold
    }
}
