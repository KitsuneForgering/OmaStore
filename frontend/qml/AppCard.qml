import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

// Cartão do grid do catálogo. Os roles do CatalogModel chegam como
// propriedades obrigatórias.
Rectangle {
    id: card
    required property string repo
    required property string name
    required property string summary
    required property string iconUrl
    required property int stars
    required property string installedVersion
    required property bool updateAvailable

    signal activated()

    readonly property var job: { backend.jobs.revision; return backend.jobs.forRepo(repo) }

    radius: 8
    color: area.containsMouse || activeFocus ? Qt.lighter(theme.surface, 1.15) : theme.surface
    border.color: activeFocus ? theme.accent : "transparent"
    border.width: 2
    Keys.onReturnPressed: activated()
    Keys.onEnterPressed: activated()

    MouseArea {
        id: area
        anchors.fill: parent
        hoverEnabled: true
        cursorShape: Qt.PointingHandCursor
        onClicked: card.activated()
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 14
        spacing: 8

        RowLayout {
            spacing: 12
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
                    font.pixelSize: 16
                    font.bold: true
                    elide: Text.ElideRight
                }
                Text {
                    Layout.fillWidth: true
                    text: card.repo
                    color: theme.muted
                    font.pixelSize: 11
                    elide: Text.ElideMiddle
                }
            }
        }

        Text {
            Layout.fillWidth: true
            Layout.fillHeight: true
            text: card.summary
            color: theme.foreground
            opacity: 0.85
            wrapMode: Text.Wrap
            maximumLineCount: 3
            elide: Text.ElideRight
        }

        ProgressBar {
            Layout.fillWidth: true
            visible: !!card.job.id
            indeterminate: !card.job.id || card.job.progress < 0
            value: card.job.progress >= 0 ? card.job.progress : 0
        }

        RowLayout {
            Text {
                text: "★ " + card.stars
                color: theme.warning
                font.pixelSize: 12
            }
            Item { Layout.fillWidth: true }
            Text {
                visible: card.installedVersion !== ""
                text: card.updateAvailable ? qsTr("atualização disponível") : qsTr("instalado")
                color: card.updateAvailable ? theme.warning : theme.success
                font.pixelSize: 12
            }
        }
    }
}
