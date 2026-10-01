import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

Page {
    id: page
    signal backRequested()
    signal appActivated(string repo)
    signal publishRequested(string repo)

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
        case "authorize": return qsTr("Installing system dependencies (administrator password)")
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
                    // Name and repository shrink with the window instead of
                    // widening the page: a long name wraps, the link elides.
                    Text {
                        objectName: "detailName"
                        Layout.fillWidth: true
                        text: page.app.name || ""
                        color: theme.foreground
                        font.pixelSize: 28
                        font.bold: true
                        wrapMode: Text.Wrap
                    }
                    LinkText {
                        Layout.fillWidth: true
                        Layout.maximumWidth: implicitWidth
                        elide: Text.ElideMiddle
                        text: page.app.repo || ""
                        url: page.app.htmlUrl || ""
                        Accessible.description: qsTr("Opens the repository on GitHub")
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
                // Liking an app stars its repository on GitHub. The wrapper
                // takes the hover so the tooltip also explains a disabled button.
                Item {
                    Layout.alignment: Qt.AlignTop
                    implicitWidth: starButton.implicitWidth
                    implicitHeight: starButton.implicitHeight
                    HoverHandler { id: starHover }
                    ToolTip.visible: starHover.hovered
                    ToolTip.delay: 400
                    ToolTip.text: backend.starHint !== "" ? backend.starHint
                                : starButton.starred ? qsTr("Remove your star on GitHub")
                                : qsTr("Star this app on GitHub")
                    Button {
                        id: starButton
                        objectName: "starButton"
                        anchors.fill: parent
                        readonly property bool starred: backend.starState === 1
                        enabled: backend.connected && backend.starState !== -1 && !backend.starBusy
                        text: (starred ? "★ " + qsTr("Starred") : "☆ " + qsTr("Star")) + "  " + (page.app.stars || 0)
                        onClicked: backend.toggleStar()
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
                    visible: !!page.app.screenshots && page.app.screenshots.length > 0

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
                                required property int index
                                radius: 6
                                color: theme.surface
                                Image {
                                    Accessible.role: Accessible.Graphic
                                    Accessible.name: qsTr("Screenshot %1 of %2 of %3").arg(parent.index + 1)
                                                         .arg(previewCarousel.count).arg(page.app.name || "")
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
                            LinkText {
                                visible: !!page.app.htmlUrl
                                text: qsTr("View on GitHub ↗")
                                url: page.app.htmlUrl || ""
                            }
                            // A prefilled issue on the app's repository; nothing is
                            // sent until the user submits it in the browser.
                            LinkText {
                                objectName: "reportLink"
                                visible: !!page.app.htmlUrl
                                Layout.topMargin: -8
                                text: qsTr("Report a problem ↗")
                                onActivated: Qt.openUrlExternally(backend.issueUrl())
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
                        objectName: "openButton"
                        Layout.fillWidth: true
                        visible: page.installed && !page.busy
                        text: qsTr("Open")
                        onClicked: backend.launch()
                    }
                    Button {
                        Layout.fillWidth: true
                        visible: page.installed && !page.busy
                        text: qsTr("Remove")
                        onClicked: confirmRemove.open()
                    }
                    // The last install/update of this app failed: say why and
                    // offer a prefilled report to its author.
                    ColumnLayout {
                        objectName: "failureBox"
                        Layout.fillWidth: true
                        visible: backend.detailFailure !== "" && !page.busy
                        spacing: 4
                        Text {
                            Layout.fillWidth: true
                            text: qsTr("The last attempt failed: %1").arg(backend.detailFailure)
                            color: theme.danger
                            font.pixelSize: 12
                            wrapMode: Text.Wrap
                        }
                        LinkText {
                            text: qsTr("Report this problem to the author ↗")
                            font.pixelSize: 12
                            onActivated: Qt.openUrlExternally(backend.issueUrl())
                        }
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
                    // System dependencies declared in the app's PKGBUILD.
                    Rectangle {
                        id: depsBox
                        objectName: "depsBox"
                        readonly property var deps: backend.deps.deps || []
                        readonly property var toInstall: backend.deps.toInstall || []
                        Layout.fillWidth: true
                        Layout.preferredHeight: depsContent.implicitHeight + 32
                        visible: deps.length > 0
                        radius: 8
                        color: theme.surface
                        border.color: theme.selection

                        function statusText(d) {
                            switch (d.status) {
                            case "installed": return "✓"
                            case "available": return qsTr("missing")
                            case "unavailable": return qsTr("not in pacman (AUR?)")
                            }
                            return ""
                        }

                        ColumnLayout {
                            id: depsContent
                            anchors.fill: parent
                            anchors.margins: 16
                            spacing: 6

                            Label {
                                text: qsTr("System dependencies")
                                color: theme.muted
                                font.pixelSize: 12
                            }
                            Repeater {
                                model: depsBox.deps
                                RowLayout {
                                    required property var modelData
                                    Layout.fillWidth: true
                                    spacing: 8
                                    Text {
                                        Layout.fillWidth: true
                                        text: modelData.name + (modelData.optional ? " " + qsTr("(optional)") : "")
                                        color: theme.foreground
                                        elide: Text.ElideRight
                                        ToolTip.visible: depHover.hovered && !!modelData.reason
                                        ToolTip.text: modelData.reason || ""
                                        HoverHandler { id: depHover }
                                    }
                                    Text {
                                        text: depsBox.statusText(modelData)
                                        color: modelData.status === "installed" ? theme.muted
                                             : modelData.status === "unavailable" ? theme.warning : theme.accent
                                        font.pixelSize: 12
                                        // The check mark alone says nothing to a screen reader.
                                        Accessible.name: modelData.status === "installed" ? qsTr("installed") : text
                                    }
                                }
                            }
                            Button {
                                objectName: "installDepsButton"
                                Layout.fillWidth: true
                                Layout.topMargin: 6
                                visible: depsBox.toInstall.length > 0 && !page.busy
                                enabled: backend.connected
                                text: depsBox.toInstall.length === 1 ? qsTr("Install 1 dependency")
                                      : qsTr("Install %1 dependencies").arg(depsBox.toInstall.length)
                                onClicked: backend.installDeps(page.repo)
                            }
                            Text {
                                Layout.fillWidth: true
                                visible: backend.deps.pacman === false
                                text: qsTr("Install them with your system's package manager.")
                                color: theme.muted
                                font.pixelSize: 11
                                wrapMode: Text.Wrap
                            }
                        }
                    }
                    Text {
                        visible: page.unverified && !page.installed
                        text: qsTr("⚠ no checksum")
                        color: theme.warning
                        font.pixelSize: 11
                    }
                    Text {
                        objectName: "notInstallableHint"
                        Layout.fillWidth: true
                        visible: !!page.app.repo && !page.app.installable && !page.installed
                        text: qsTr("The latest release has no Linux binary for this computer.")
                        color: theme.muted
                        font.pixelSize: 12
                        wrapMode: Text.Wrap
                    }
                    LinkText {
                        objectName: "authorCheckLink"
                        Layout.fillWidth: true
                        visible: !!page.app.repo
                        text: page.app.installable ? qsTr("Is this your app? Check how it looks to the store →")
                                                   : qsTr("Is this your app? See what is missing →")
                        font.pixelSize: 12
                        wrapMode: Text.Wrap
                        onActivated: page.publishRequested(page.app.repo)
                    }
                }
            }

            // Release notes of the latest release, rendered like the README
            // (no remote images; links open only on click).
            ColumnLayout {
                objectName: "releaseNotes"
                Layout.fillWidth: true
                Layout.maximumWidth: 900
                Layout.alignment: Qt.AlignHCenter
                visible: !!page.app.releaseNotes
                spacing: 18
                Text {
                    text: page.installed && page.app.updateAvailable
                          ? qsTr("What's new in %1 (you have %2)").arg(page.app.latestVersion).arg(page.app.install.version)
                          : qsTr("What's new in %1").arg(page.app.latestVersion)
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
                    Layout.fillWidth: true
                    text: page.app.releaseNotes ? backend.readmeForDisplay(page.app.releaseNotes) : ""
                    textFormat: Text.MarkdownText
                    wrapMode: Text.Wrap
                    font.pixelSize: 15
                    color: theme.foreground
                    linkColor: theme.accent
                    onLinkActivated: (link) => {
                        if (link.startsWith("https://") || link.startsWith("http://"))
                            Qt.openUrlExternally(link)
                    }
                    HoverHandler { cursorShape: parent.hoveredLink ? Qt.PointingHandCursor : Qt.ArrowCursor }
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
                            id: simCard
                            required property var modelData
                            width: 220
                            height: 64
                            radius: 6
                            color: simArea.containsMouse || activeFocus ? theme.hover : theme.surface
                            border.color: theme.focus
                            border.width: activeFocus ? 2 : 0
                            Behavior on color { ColorAnimation { duration: theme.durationShort } }
                            activeFocusOnTab: true
                            Accessible.role: Accessible.Button
                            Accessible.name: qsTr("%1, %2").arg(modelData.name).arg(modelData.category)
                            Accessible.onPressAction: page.appActivated(modelData.repo)
                            Keys.onReturnPressed: page.appActivated(modelData.repo)
                            Keys.onEnterPressed: page.appActivated(modelData.repo)
                            Keys.onSpacePressed: page.appActivated(modelData.repo)
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
