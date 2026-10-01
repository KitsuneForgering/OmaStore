import QtQuick

// Text link reachable from the keyboard (Tab, then Enter or Space) and
// announced as a link by screen readers. Opens url, or emits activated when
// url is empty.
Text {
    id: link
    property string url: ""
    signal activated()

    function trigger() {
        if (url !== "")
            Qt.openUrlExternally(url)
        else
            activated()
    }

    color: theme.accent
    font.underline: linkArea.containsMouse || activeFocus
    activeFocusOnTab: true
    Accessible.role: Accessible.Link
    Accessible.name: text
    Accessible.onPressAction: trigger()
    Keys.onReturnPressed: trigger()
    Keys.onEnterPressed: trigger()
    Keys.onSpacePressed: trigger()

    // Focus ring: outside the text, in the theme's focus color (3:1).
    Rectangle {
        anchors.fill: parent
        anchors.margins: -3
        radius: 3
        color: "transparent"
        border.color: theme.focus
        border.width: 2
        visible: link.activeFocus
    }
    MouseArea {
        id: linkArea
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onClicked: link.trigger()
    }
}
