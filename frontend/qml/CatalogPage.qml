import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

Page {
    id: page
    required property var model
    property bool installedView: false
    signal appActivated(string repo)

    function focusSearch() {
        search.forceActiveFocus()
        search.selectAll()
    }

    background: Rectangle { color: theme.background }

    header: ToolBar {
        background: Rectangle { color: theme.background }
        contentHeight: 56
        RowLayout {
            anchors.fill: parent
            anchors.leftMargin: 20
            anchors.rightMargin: 20
            spacing: 12

            Text {
                text: page.installedView ? qsTr("Installed")
                                         : (page.model.category !== "" ? page.model.category : qsTr("Discover"))
                color: theme.foreground
                font.pixelSize: 20
                font.bold: true
            }
            Item { Layout.fillWidth: true }
            PrimaryButton {
                visible: page.installedView && backend.updatesAvailable > 0
                text: qsTr("Update all")
                onClicked: backend.updateAll()
            }
            TextField {
                id: search
                Layout.preferredWidth: 280
                placeholderText: qsTr("Search apps  ( / )")
                text: page.model.query
                onTextChanged: page.model.query = text
                Keys.onDownPressed: grid.forceActiveFocus()
                Keys.onEscapePressed: { text = ""; grid.forceActiveFocus() }
            }
        }
    }

    GridView {
        id: grid
        anchors.fill: parent
        anchors.margins: 16
        clip: true
        model: page.model
        keyNavigationEnabled: true
        focus: true
        readonly property int columns: Math.max(1, Math.floor(width / 300))
        cellWidth: width / columns
        cellHeight: 190
        ScrollBar.vertical: ScrollBar {}

        delegate: Item {
            required property int index
            required property string repo
            required property string name
            required property string summary
            required property string iconUrl
            required property int stars
            required property string installedVersion
            required property bool updateAvailable
            width: grid.cellWidth
            height: grid.cellHeight

            AppCard {
                anchors.fill: parent
                anchors.margins: 6
                repo: parent.repo
                name: parent.name
                summary: parent.summary
                iconUrl: parent.iconUrl
                stars: parent.stars
                installedVersion: parent.installedVersion
                updateAvailable: parent.updateAvailable
                focus: grid.currentIndex === parent.index
                onActivated: page.appActivated(repo)
            }
        }
        Keys.onReturnPressed: if (currentIndex >= 0) page.appActivated(page.model.get(currentIndex).repo)
    }

    // Empty states.
    ColumnLayout {
        anchors.centerIn: parent
        visible: grid.count === 0
        spacing: 12
        readonly property var job: { backend.jobs.revision; return backend.jobs.indexJob() }

        BusyIndicator {
            Layout.alignment: Qt.AlignHCenter
            running: parent.visible && (page.model.loading || !!parent.job.id || !backend.connected)
        }
        Text {
            Layout.alignment: Qt.AlignHCenter
            horizontalAlignment: Text.AlignHCenter
            color: theme.muted
            text: {
                if (!backend.connected)
                    return qsTr("Connecting to omastored…")
                if (parent.job.id)
                    return qsTr("Indexing the catalog… %1/%2\n%3").arg(parent.job.done).arg(parent.job.total).arg(parent.job.message || "")
                if (page.model.error !== "")
                    return page.model.error
                if (page.installedView)
                    return qsTr("No apps installed yet.")
                if (page.model.query !== "")
                    return qsTr("Nothing found for “%1”.").arg(page.model.query)
                return qsTr("No compatible apps yet.\nApps join the store when their repository publishes an omastore.toml.")
            }
        }
        Button {
            Layout.alignment: Qt.AlignHCenter
            visible: backend.connected && !parent.job.id && !page.installedView && page.model.query === ""
            text: qsTr("Refresh catalog")
            onClicked: backend.refreshIndex(false)
        }
    }
}
