import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "categories.js" as Categories

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

    // Card heights from the font sizes: icon row, three summary lines and the
    // footer, so every card fits its text at any system font size.
    FontMetrics { id: bodyMetrics; font.pixelSize: theme.fontBody }
    FontMetrics { id: subtitleMetrics; font.pixelSize: theme.fontSubtitle }
    FontMetrics { id: captionMetrics; font.pixelSize: theme.fontCaption }

    header: ToolBar {
        background: Rectangle { color: theme.background }
        topPadding: theme.spaceXl
        bottomPadding: theme.spaceL
        leftPadding: theme.spaceXl
        rightPadding: theme.spaceXl
        RowLayout {
            anchors.fill: parent
            spacing: theme.spaceM

            ColumnLayout {
                spacing: 2
                Text {
                    objectName: "catalogTitle"
                    text: page.installedView ? qsTr("Installed")
                                             : (page.model.category !== "" ? Categories.display(page.model.category) : qsTr("Discover"))
                    color: theme.foreground
                    font.pixelSize: theme.fontTitle
                    font.weight: Font.DemiBold
                    Accessible.role: Accessible.Heading
                }
                Text {
                    objectName: "catalogCount"
                    visible: grid.count > 0
                    text: page.model.query !== "" ? qsTr("%n result(s)", "", grid.count) : qsTr("%n app(s)", "", grid.count)
                    color: theme.muted
                    font.pixelSize: theme.fontCaption
                }
            }
            Item { Layout.fillWidth: true }
            PrimaryButton {
                visible: page.installedView && backend.updatesAvailable > 0
                text: qsTr("Update all (%1)").arg(backend.updatesAvailable)
                onClicked: backend.updateAll()
            }
            TextField {
                id: search
                objectName: "searchField"
                Layout.preferredWidth: Math.min(320, page.width * 0.4)
                placeholderText: qsTr("Search apps  ( / )")
                placeholderTextColor: theme.muted
                color: theme.foreground
                selectionColor: theme.focus
                selectedTextColor: theme.onFocus
                leftPadding: theme.spaceM
                rightPadding: clearSearch.visible ? clearSearch.width + theme.spaceS : theme.spaceM
                topPadding: theme.spaceS
                bottomPadding: theme.spaceS
                text: page.model.query
                onTextChanged: page.model.query = text
                Keys.onDownPressed: grid.forceActiveFocus()
                Keys.onEscapePressed: { text = ""; grid.forceActiveFocus() }
                Accessible.name: qsTr("Search apps")
                background: Rectangle {
                    implicitHeight: Math.max(38, theme.fontBody * 2.6)
                    radius: theme.radiusS
                    color: theme.surface
                    border.color: search.activeFocus ? theme.focus : theme.border
                    border.width: search.activeFocus ? 2 : 1
                }
                // Clearing is one click, not only Esc.
                ActionButton {
                    id: clearSearch
                    kind: "quiet"
                    anchors.right: parent.right
                    anchors.verticalCenter: parent.verticalCenter
                    visible: search.text !== ""
                    text: "✕"
                    Accessible.name: qsTr("Clear search")
                    onClicked: { search.text = ""; search.forceActiveFocus() }
                }
            }
        }
    }

    GridView {
        id: grid
        objectName: "catalogGrid"
        anchors.fill: parent
        // The cards' gutter (spaceS on each side) makes the outer edges line
        // up with the header's spaceXl margin.
        anchors.leftMargin: theme.spaceXl - theme.spaceS
        anchors.rightMargin: theme.spaceXl - theme.spaceS
        anchors.bottomMargin: theme.spaceL
        clip: true
        model: page.model
        keyNavigationEnabled: true
        focus: true
        readonly property int columns: Math.max(1, Math.floor(width / 300))
        cellWidth: Math.floor(width / columns)
        cellHeight: theme.spaceS * 2 + theme.spaceL * 2
                    + Math.max(48, subtitleMetrics.height + captionMetrics.height + 2)
                    + theme.spaceM + bodyMetrics.lineSpacing * 3
                    + theme.spaceM + captionMetrics.height + theme.spaceXs + 2
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
                anchors.margins: theme.spaceS
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
        width: Math.min(parent.width - theme.spaceXxl * 2, 640)
        visible: grid.count === 0
        spacing: theme.spaceL
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
            font.pixelSize: theme.fontTitle
            font.weight: Font.DemiBold
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
            lineHeight: 1.3
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
            spacing: theme.spaceM
            visible: !empty.waiting
            PrimaryButton {
                visible: page.installedView
                text: qsTr("Discover apps")
                onClicked: page.discoverRequested()
            }
            ActionButton {
                visible: !page.installedView && page.model.query !== ""
                text: qsTr("Clear search")
                onClicked: { search.text = ""; search.forceActiveFocus() }
            }
            ActionButton {
                visible: !page.installedView && page.model.query === ""
                text: qsTr("Refresh catalog")
                onClicked: backend.refreshIndex(false)
            }
        }

        // For people who make apps: the way in.
        Rectangle {
            objectName: "authorInvite"
            Layout.fillWidth: true
            Layout.topMargin: theme.spaceS
            visible: empty.welcome
            implicitHeight: invite.implicitHeight + theme.spaceL * 2
            radius: theme.radiusM
            color: theme.surface
            border.color: theme.outline
            RowLayout {
                id: invite
                anchors.fill: parent
                anchors.margins: theme.spaceL
                spacing: theme.spaceL
                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: theme.spaceXs
                    Text {
                        Layout.fillWidth: true
                        text: qsTr("Do you make an app for Omarchy?")
                        color: theme.foreground
                        font.weight: Font.DemiBold
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
