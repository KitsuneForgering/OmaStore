import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

// Catalog grid card. The CatalogModel roles arrive as required
// properties. Its height comes from the text sizes (see CatalogPage), never a
// fixed number, so a larger system font does not cut the summary.
Rectangle {
    id: card
    required property string repo
    required property string name
    required property string summary
    required property string iconUrl
    required property int stars
    required property string installedVersion
    required property bool updateAvailable
    property bool blocked: false

    signal activated()

    readonly property var job: { backend.jobs.revision; return backend.jobs.forRepo(repo) }

    radius: theme.radiusM
    color: area.containsMouse || activeFocus ? theme.hover : theme.surface
    border.color: activeFocus ? theme.focus : theme.outline
    border.width: activeFocus ? 2 : 1
    Behavior on color { ColorAnimation { duration: theme.durationShort } }
    Behavior on border.color { ColorAnimation { duration: theme.durationShort } }
    Keys.onReturnPressed: activated()
    Keys.onEnterPressed: activated()
    Keys.onSpacePressed: activated()
    Accessible.role: Accessible.Button
    Accessible.name: name
    Accessible.description: summary + (updateAvailable ? " — " + qsTr("update available")
                                       : installedVersion !== "" ? " — " + qsTr("installed") : "")
    Accessible.onPressAction: activated()

    MouseArea {
        id: area
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onClicked: card.activated()
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: theme.spaceL
        spacing: theme.spaceM

        RowLayout {
            Layout.fillWidth: true
            spacing: theme.spaceM
            AppIcon {
                Layout.preferredWidth: 48
                Layout.preferredHeight: 48
                url: card.iconUrl
                name: card.name
            }
            ColumnLayout {
                Layout.fillWidth: true
                spacing: 2
                Text {
                    Layout.fillWidth: true
                    text: card.name
                    color: theme.foreground
                    font.pixelSize: theme.fontSubtitle
                    font.weight: Font.DemiBold
                    elide: Text.ElideRight
                }
                Text {
                    Layout.fillWidth: true
                    text: card.repo
                    color: theme.muted
                    font.family: theme.monoFamily
                    font.pixelSize: theme.fontCaption
                    elide: Text.ElideMiddle
                }
            }
        }

        Text {
            Layout.fillWidth: true
            Layout.fillHeight: true
            verticalAlignment: Text.AlignTop
            text: card.summary
            color: theme.foreground
            wrapMode: Text.Wrap
            maximumLineCount: 3
            elide: Text.ElideRight
        }

        ProgressBar {
            Layout.fillWidth: true
            visible: !!card.job.id
            indeterminate: !card.job.id || card.job.progress < 0
            value: card.job.progress >= 0 ? card.job.progress : 0
            Accessible.name: qsTr("Progress of %1").arg(card.name)
        }

        RowLayout {
            Layout.fillWidth: true
            spacing: theme.spaceS
            Text {
                text: "★ " + card.stars
                color: theme.muted
                font.pixelSize: theme.fontCaption
                Accessible.name: qsTr("%n star(s)", "", card.stars)
            }
            Item { Layout.fillWidth: true }
            Badge {
                visible: card.installedVersion !== ""
                text: card.blocked ? qsTr("blocked") : card.updateAvailable ? qsTr("update available") : qsTr("installed")
                tone: card.blocked || card.updateAvailable ? "warning" : "success"
            }
        }
    }
}
