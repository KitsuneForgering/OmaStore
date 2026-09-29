import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

Rectangle {
    id: root
    color: theme.surface

    property string section: "discover"
    signal sectionSelected(string section)
    signal categorySelected(string category)

    component NavItem: ItemDelegate {
        id: item
        property bool selected: false
        property string badge: ""
        Layout.fillWidth: true
        highlighted: selected
        contentItem: RowLayout {
            Text {
                Layout.fillWidth: true
                text: item.text
                color: item.selected ? theme.accent : theme.foreground
                font.bold: item.selected
                elide: Text.ElideRight
            }
            Text {
                visible: item.badge !== ""
                text: item.badge
                color: theme.muted
            }
        }
        background: Rectangle {
            color: item.selected ? theme.selection : (item.hovered ? Qt.darker(theme.surface, 1.15) : "transparent")
            radius: 4
        }
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: 12
        spacing: 2

        Text {
            text: "OmaStore"
            color: theme.accent
            font.pixelSize: 22
            font.bold: true
            Layout.bottomMargin: 12
        }

        NavItem {
            text: qsTr("Descobrir")
            selected: root.section === "discover" && backend.catalog.category === ""
            onClicked: { backend.catalog.category = ""; root.sectionSelected("discover") }
        }
        NavItem {
            text: qsTr("Instalados")
            selected: root.section === "installed"
            badge: backend.updatesAvailable > 0 ? qsTr("%n atualização(ões)", "", backend.updatesAvailable) : ""
            onClicked: root.sectionSelected("installed")
        }

        Text {
            text: qsTr("Categorias")
            color: theme.muted
            font.pixelSize: 12
            Layout.topMargin: 16
            Layout.bottomMargin: 4
        }

        ListView {
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            model: backend.categories
            delegate: NavItem {
                width: ListView.view.width
                text: modelData.name
                badge: modelData.count
                selected: root.section === "discover" && backend.catalog.category === modelData.name
                onClicked: root.categorySelected(modelData.name)
            }
        }

        Button {
            Layout.fillWidth: true
            readonly property var job: { backend.jobs.revision; return backend.jobs.indexJob() }
            enabled: backend.connected && !job.id
            text: job.id ? qsTr("Atualizando catálogo…") : qsTr("Atualizar catálogo")
            onClicked: backend.refreshIndex(false)
            ToolTip.visible: hovered
            ToolTip.text: qsTr("Busca apps novos e versões no GitHub (Ctrl+R)")
        }

        RowLayout {
            Layout.topMargin: 6
            Rectangle {
                Layout.preferredWidth: 8
                Layout.preferredHeight: 8
                radius: 4
                color: backend.connected ? theme.success : theme.danger
            }
            Text {
                text: backend.connected ? qsTr("conectado") : qsTr("conectando ao omastored…")
                color: theme.muted
                font.pixelSize: 11
            }
        }
    }
}
