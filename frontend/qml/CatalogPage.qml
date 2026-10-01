import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

Page {
    id: page
    required property var model
    property bool installedView: false
    signal appActivated(string repo)
    signal publishRequested()
    signal discoverRequested()

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

        populate: Transition {
            NumberAnimation { property: "opacity"; from: 0; to: 1; duration: theme.durationMedium; easing.type: Easing.OutCubic }
            NumberAnimation { property: "scale"; from: 0.96; to: 1; duration: theme.durationMedium; easing.type: Easing.OutCubic }
        }
        add: Transition {
            NumberAnimation { property: "opacity"; from: 0; to: 1; duration: theme.durationMedium; easing.type: Easing.OutCubic }
            NumberAnimation { property: "scale"; from: 0.96; to: 1; duration: theme.durationMedium; easing.type: Easing.OutCubic }
        }
        displaced: Transition {
            NumberAnimation { properties: "x,y"; duration: theme.durationMedium; easing.type: Easing.OutCubic }
        }

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
        id: empty
        objectName: "emptyState"
        anchors.centerIn: parent
        width: Math.min(parent.width - 64, 640)
        visible: grid.count === 0
        spacing: 16
        readonly property var job: { backend.jobs.revision; return backend.jobs.indexJob() }
        readonly property bool waiting: !backend.connected || !!job.id || page.model.loading
        // A fresh catalog: nothing indexed, no search, not the installed list.
        readonly property bool welcome: !waiting && !page.installedView && page.model.query === ""
                                        && page.model.category === "" && page.model.error === ""

        BusyIndicator {
            Layout.alignment: Qt.AlignHCenter
            visible: empty.waiting
            running: parent.visible && empty.waiting
        }
        Text {
            objectName: "emptyTitle"
            Layout.fillWidth: true
            horizontalAlignment: Text.AlignHCenter
            wrapMode: Text.Wrap
            color: theme.foreground
            font.pixelSize: 20
            font.bold: true
            text: {
                if (!backend.connected)
                    return qsTr("Connecting to omastored…")
                if (empty.job.id)
                    return qsTr("Looking for apps on GitHub…")
                if (page.installedView)
                    return qsTr("No apps installed yet")
                if (page.model.query !== "")
                    return qsTr("Nothing found for “%1”").arg(page.model.query)
                if (page.model.error !== "")
                    return qsTr("The catalog could not be loaded")
                return qsTr("The catalog is just getting started")
            }
        }
        Text {
            Layout.fillWidth: true
            horizontalAlignment: Text.AlignHCenter
            wrapMode: Text.Wrap
            color: theme.muted
            text: {
                if (!backend.connected)
                    return ""
                if (empty.job.id)
                    return qsTr("%1 of %2 repositories checked\n%3").arg(empty.job.done).arg(empty.job.total).arg(empty.job.message || "")
                if (page.model.error !== "")
                    return page.model.error
                if (page.installedView)
                    return qsTr("Apps you install from Discover show up here, with their updates.")
                if (page.model.query !== "")
                    return qsTr("Try another word, or a description of what the app does.")
                return qsTr("Apps join OmaStore when their authors add an omastore.toml to their GitHub repository. New apps show up here after each catalog refresh.")
            }
        }
        Text {
            objectName: "indexError"
            Layout.fillWidth: true
            horizontalAlignment: Text.AlignHCenter
            wrapMode: Text.Wrap
            visible: !empty.waiting && backend.indexError !== "" && !page.installedView
            text: qsTr("Last refresh failed: %1").arg(backend.indexError)
            color: theme.danger
        }
        RowLayout {
            Layout.alignment: Qt.AlignHCenter
            spacing: 12
            visible: !empty.waiting
            PrimaryButton {
                visible: page.installedView
                text: qsTr("Discover apps")
                onClicked: page.discoverRequested()
            }
            Button {
                visible: !page.installedView && page.model.query === ""
                text: qsTr("Refresh catalog")
                onClicked: backend.refreshIndex(false)
            }
        }

        // For people who make apps: the way in.
        Rectangle {
            objectName: "authorInvite"
            Layout.fillWidth: true
            Layout.topMargin: 8
            visible: empty.welcome
            implicitHeight: invite.implicitHeight + 32
            radius: 8
            color: theme.surface
            border.color: theme.selection
            RowLayout {
                id: invite
                anchors.fill: parent
                anchors.margins: 16
                spacing: 16
                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: 4
                    Text {
                        Layout.fillWidth: true
                        text: qsTr("Do you make an app for Omarchy?")
                        color: theme.foreground
                        font.bold: true
                        wrapMode: Text.Wrap
                    }
                    Text {
                        Layout.fillWidth: true
                        text: qsTr("Check your repository and get a ready-to-commit omastore.toml.")
                        color: theme.muted
                        wrapMode: Text.Wrap
                    }
                }
                PrimaryButton {
                    text: qsTr("Publish your app")
                    onClicked: page.publishRequested()
                }
            }
        }
    }
}
