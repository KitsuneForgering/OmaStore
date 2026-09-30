import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

Page {
    id: page
    signal backRequested()
    signal appActivated(string repo)

    readonly property var app: backend.detail
    readonly property string repo: app.repo || ""
    readonly property var job: { backend.jobs.revision; return app.repo ? backend.jobs.forRepo(app.repo) : ({}) }
    readonly property bool installed: !!app.install
    readonly property bool busy: !!job.id
    readonly property bool unverified: {
        if (!app.assets) return false
        for (let a of app.assets) if (a.verified) return false
        return app.assets.length > 0
    }

    background: Rectangle { color: theme.background }

    function flickToTop() { flick.contentY = 0 }

    function stageText(stage) {
        switch (stage) {
        case "download": return qsTr("Downloading")
        case "verify": return qsTr("Verifying")
        case "extract": return qsTr("Extracting")
        case "integrate": return qsTr("Integrating with the system")
        case "done": return qsTr("Done")
        }
        return qsTr("Preparing")
    }

    Dialog {
        id: confirmUnverified
        anchors.centerIn: parent
        modal: true
        title: qsTr("Install without verification?")
        standardButtons: Dialog.Yes | Dialog.No
        Label {
            width: 360
            wrapMode: Text.Wrap
            text: qsTr("This release publishes no checksum, so OmaStore cannot confirm that the downloaded file is the one the author published.")
        }
        onAccepted: backend.install(page.app.repo)
    }

    Dialog {
        id: confirmRemove
        anchors.centerIn: parent
        modal: true
        title: qsTr("Remove %1?").arg(page.app.name || "")
        standardButtons: Dialog.Yes | Dialog.No
        onAccepted: backend.uninstall(page.app.repo)
    }

    Flickable {
        id: flick
        anchors.fill: parent
        contentHeight: column.implicitHeight + 48
        clip: true
        ScrollBar.vertical: ScrollBar {}

        ColumnLayout {
            id: column
            objectName: "detailContent"
            x: 32
            y: 24
            width: flick.width - 64
            spacing: 28

            Button {
                text: qsTr("← Back")
                flat: true
                onClicked: page.backRequested()
            }

            BusyIndicator {
                visible: backend.detailLoading && !page.app.repo
                running: visible
            }

            RowLayout {
                visible: !!page.app.repo
                Layout.fillWidth: true
                spacing: 24
                AppIcon {
                    Layout.preferredWidth: 88
                    Layout.preferredHeight: 88
                    url: page.app.iconUrl || ""
                    name: page.app.name || ""
                }
                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: 8
                    Text {
                        objectName: "detailName"
                        text: page.app.name || ""
                        color: theme.foreground
                        font.pixelSize: 28
                        font.bold: true
                    }
                    Text {
                        text: page.app.repo || ""
                        color: theme.accent
                        font.underline: repoArea.containsMouse
                        MouseArea {
                            id: repoArea
                            anchors.fill: parent
                            hoverEnabled: true
                            cursorShape: Qt.PointingHandCursor
                            onClicked: if (page.app.htmlUrl) Qt.openUrlExternally(page.app.htmlUrl)
                        }
                    }
                    Text {
                        Layout.fillWidth: true
                        text: page.app.summary || ""
                        color: theme.foreground
                        wrapMode: Text.Wrap
                    }
                    Text {
                        Layout.fillWidth: true
                        color: theme.muted
                        font.pixelSize: 12
                        wrapMode: Text.Wrap
                        text: [
                            "★ " + (page.app.stars || 0),
                            page.app.category,
                            page.installed ? qsTr("installed: %1").arg(page.app.install.version) : ""
                        ].filter(s => !!s).join("  ·  ")
                    }
                }
            }

            GridLayout {
                id: hero
                objectName: "detailHero"
                Layout.fillWidth: true
                visible: !!page.app.repo
                columns: width >= 720 && gallery.visible ? 2 : 1
                columnSpacing: 20
                rowSpacing: 24

                ColumnLayout {
                    id: gallery
                    Layout.fillWidth: true
                    Layout.preferredWidth: hero.columns === 2 ? hero.width * 0.6 - 10 : hero.width
                    spacing: 10
                    visible: page.app.screenshots && page.app.screenshots.length > 0

                    SwipeView {
                        id: previewCarousel
                        objectName: "previewCarousel"
                        Layout.fillWidth: true
                        Layout.preferredHeight: hero.columns === 2 ? 340 : Math.min(320, hero.width * 0.62)
                        clip: true
                        activeFocusOnTab: true
                        Keys.onLeftPressed: decrementCurrentIndex()
                        Keys.onRightPressed: incrementCurrentIndex()

                        Repeater {
                            model: page.app.screenshots || []
                            Rectangle {
                                required property string modelData
                                radius: 6
                                color: theme.surface
                                Image {
                                    anchors.fill: parent
                                    anchors.margins: 4
                                    asynchronous: true
                                    fillMode: Image.PreserveAspectFit
                                    sourceSize: Qt.size(960, 600)
                                    source: "image://omastore/" + encodeURIComponent(modelData)
                                    BusyIndicator { anchors.centerIn: parent; running: parent.status === Image.Loading }
                                }
                            }
                        }
                    }

                    RowLayout {
                        objectName: "previewControls"
                        Layout.fillWidth: true
                        Button {
                            objectName: "previewPrevious"
                            text: qsTr("Previous")
                            enabled: previewCarousel.currentIndex > 0
                            onClicked: previewCarousel.decrementCurrentIndex()
                        }
                        Item { Layout.fillWidth: true }
                        Text {
                            objectName: "previewPosition"
                            text: qsTr("%1 / %2").arg(previewCarousel.currentIndex + 1).arg(previewCarousel.count)
                            color: theme.muted
                        }
                        Button {
                            objectName: "previewNext"
                            text: qsTr("Next")
                            enabled: previewCarousel.currentIndex < previewCarousel.count - 1
                            onClicked: previewCarousel.incrementCurrentIndex()
                        }
                    }

                    ListView {
                        id: previewStrip
                        objectName: "previewStrip"
                        Layout.fillWidth: true
                        Layout.preferredHeight: 72
                        visible: count > 1
                        orientation: ListView.Horizontal
                        spacing: 8
                        clip: true
                        model: page.app.screenshots || []
                        delegate: Rectangle {
                            required property string modelData
                            required property int index
                            width: 112
                            height: 68
                            radius: 5
                            color: theme.surface
                            border.width: index === previewCarousel.currentIndex ? 2 : 0
                            border.color: theme.accent
                            Image {
                                anchors.fill: parent
                                anchors.margins: 3
                                source: "image://omastore/" + encodeURIComponent(modelData)
                                fillMode: Image.PreserveAspectFit
                                asynchronous: true
                                sourceSize: Qt.size(224, 136)
                            }
                            MouseArea {
                                anchors.fill: parent
                                cursorShape: Qt.PointingHandCursor
                                onClicked: previewCarousel.setCurrentIndex(index)
                            }
                        }
                        Connections {
                            target: previewCarousel
                            function onCurrentIndexChanged() {
                                previewStrip.positionViewAtIndex(previewCarousel.currentIndex, ListView.Contain)
                            }
                        }
                    }
                }

                ColumnLayout {
                    id: infoPanel
                    objectName: "detailInfoPanel"
                    Layout.fillWidth: true
                    Layout.preferredWidth: hero.columns === 2 ? hero.width * 0.4 - 10 : hero.width
                    Layout.alignment: Qt.AlignTop
                    spacing: 18

                    Rectangle {
                        Layout.fillWidth: true
                        Layout.preferredHeight: infoContent.implicitHeight + 40
                        radius: 8
                        color: theme.surface
                        border.color: theme.selection

                        ColumnLayout {
                            id: infoContent
                            anchors.fill: parent
                            anchors.margins: 20
                            spacing: 16

                            Label {
                                text: qsTr("Version")
                                color: theme.muted
                                font.pixelSize: 12
                            }
                            Text {
                                Layout.topMargin: -12
                                text: page.app.latestVersion || qsTr("Unavailable")
                                color: theme.foreground
                                font.pixelSize: 16
                            }
                            Label {
                                text: qsTr("License")
                                color: theme.muted
                                font.pixelSize: 12
                            }
                            Text {
                                Layout.topMargin: -12
                                text: page.app.license || qsTr("Not specified")
                                color: theme.foreground
                            }
                            Text {
                                visible: !!page.app.htmlUrl
                                text: qsTr("View on GitHub ↗")
                                color: theme.accent
                                font.underline: githubArea.containsMouse
                                MouseArea {
                                    id: githubArea
                                    anchors.fill: parent
                                    hoverEnabled: true
                                    cursorShape: Qt.PointingHandCursor
                                    onClicked: Qt.openUrlExternally(page.app.htmlUrl)
                                }
                            }
                        }
                    }

                    PrimaryButton {
                        objectName: "installButton"
                        Layout.fillWidth: true
                        visible: !page.installed && !page.busy
                        enabled: backend.connected && !!page.app.installable
                        text: page.app.installable ? qsTr("Install") : qsTr("No Linux binary")
                        onClicked: page.unverified ? confirmUnverified.open() : backend.install(page.app.repo)
                    }
                    PrimaryButton {
                        Layout.fillWidth: true
                        visible: page.installed && !!page.app.updateAvailable && !page.busy
                        text: qsTr("Update to %1").arg(page.app.latestVersion)
                        onClicked: backend.update(page.app.repo)
                    }
                    Button {
                        Layout.fillWidth: true
                        visible: page.installed && !page.busy
                        text: qsTr("Remove")
                        onClicked: confirmRemove.open()
                    }
                    ColumnLayout {
                        visible: page.busy
                        Layout.fillWidth: true
                        Text {
                            text: page.stageText(page.job.stage)
                            color: theme.foreground
                        }
                        ProgressBar {
                            Layout.fillWidth: true
                            indeterminate: page.job.progress === undefined || page.job.progress < 0
                            value: page.job.progress > 0 ? page.job.progress : 0
                        }
                        Button {
                            text: qsTr("Cancel")
                            flat: true
                            onClicked: backend.cancelJob(page.job.id)
                        }
                    }
                    Text {
                        visible: page.unverified && !page.installed
                        text: qsTr("⚠ no checksum")
                        color: theme.warning
                        font.pixelSize: 11
                    }
                }
            }

            ColumnLayout {
                Layout.fillWidth: true
                Layout.maximumWidth: 900
                Layout.alignment: Qt.AlignHCenter
                visible: !!page.app.readme
                spacing: 18
                Text {
                    text: qsTr("README")
                    color: theme.foreground
                    font.pixelSize: 20
                    font.bold: true
                }
                Rectangle {
                    Layout.fillWidth: true
                    Layout.preferredHeight: 1
                    color: theme.selection
                }
                Text {
                    objectName: "detailReadme"
                    Layout.fillWidth: true
                    text: page.app.readme ? backend.readmeForDisplay(page.app.readme) : ""
                    textFormat: Text.MarkdownText
                    wrapMode: Text.Wrap
                    font.pixelSize: 17
                    lineHeight: 1.5
                    lineHeightMode: Text.ProportionalHeight
                    color: theme.foreground
                    linkColor: theme.accent
                    onLinkActivated: (link) => {
                        if (link.startsWith("https://") || link.startsWith("http://"))
                            Qt.openUrlExternally(link)
                    }
                    HoverHandler { cursorShape: parent.hoveredLink ? Qt.PointingHandCursor : Qt.ArrowCursor }
                }
            }

            // Similar apps (local recommendation from the daemon).
            ColumnLayout {
                Layout.fillWidth: true
                visible: backend.similar.length > 0
                spacing: 8
                Text {
                    text: qsTr("Similar apps")
                    color: theme.foreground
                    font.pixelSize: 16
                    font.bold: true
                }
                Flow {
                    Layout.fillWidth: true
                    spacing: 10
                    Repeater {
                        model: backend.similar
                        delegate: Rectangle {
                            required property var modelData
                            width: 220
                            height: 64
                            radius: 6
                            color: simArea.containsMouse ? Qt.lighter(theme.surface, 1.15) : theme.surface
                            RowLayout {
                                anchors.fill: parent
                                anchors.margins: 10
                                spacing: 10
                                AppIcon {
                                    Layout.preferredWidth: 40
                                    Layout.preferredHeight: 40
                                    url: modelData.iconUrl
                                    name: modelData.name
                                }
                                ColumnLayout {
                                    Layout.fillWidth: true
                                    spacing: 0
                                    Text {
                                        Layout.fillWidth: true
                                        text: modelData.name
                                        color: theme.foreground
                                        font.bold: true
                                        elide: Text.ElideRight
                                    }
                                    Text {
                                        Layout.fillWidth: true
                                        text: modelData.category
                                        color: theme.muted
                                        font.pixelSize: 11
                                        elide: Text.ElideRight
                                    }
                                }
                            }
                            MouseArea {
                                id: simArea
                                anchors.fill: parent
                                hoverEnabled: true
                                cursorShape: Qt.PointingHandCursor
                                onClicked: page.appActivated(modelData.repo)
                            }
                        }
                    }
                }
            }
        }
    }
}
