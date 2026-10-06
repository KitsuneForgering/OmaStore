import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "categories.js" as Categories

Page {
    id: page
    signal backRequested()
    signal appActivated(string repo)
    signal publishRequested(string repo)

    readonly property var app: backend.detail
    readonly property string repo: app.repo || ""
    readonly property var job: { backend.jobs.revision; return app.repo ? backend.jobs.forRepo(app.repo) : ({}) }
    readonly property bool installed: !!app.install
    readonly property bool broken: installed && !!app.install.broken
    readonly property bool busy: !!job.id || backend.detailRemoving
    // The file an install would download has nothing to check it with.
    readonly property bool unverified: !!app.selectedAsset && app.selectedAsset.checksum === ""
    // Verified build provenance of that file (null when there is none).
    readonly property var provenance: app.selectedAsset ? (app.selectedAsset.provenance || null) : null
    // The user requires provenance and this file has none.
    readonly property bool blockedByProvenance: backend.requireProvenance && !!app.selectedAsset && !provenance

    background: Rectangle { color: theme.background }

    function flickToTop() { flick.contentY = 0 }

    // A label above its value, for the facts panel.
    component Fact: ColumnLayout {
        property string label: ""
        property string value: ""
        property bool mono: false
        Layout.fillWidth: true
        spacing: 2
        Text {
            text: parent.label.toUpperCase()
            color: theme.muted
            font.pixelSize: theme.fontCaption
            font.weight: Font.DemiBold
            font.letterSpacing: 0.8
            Accessible.ignored: true
        }
        Text {
            Layout.fillWidth: true
            text: parent.value
            color: theme.foreground
            font.family: parent.mono ? theme.monoFamily : theme.fontFamily
            wrapMode: Text.Wrap
            Accessible.name: parent.label + ": " + parent.value
        }
    }

    function installEventText(event) {
        if (event.action === "install")
            return qsTr("Installed %1").arg(event.toVersion)
        if (event.action === "rollback")
            return qsTr("Rolled back from %1 to %2").arg(event.fromVersion).arg(event.toVersion)
        return qsTr("Updated from %1 to %2").arg(event.fromVersion).arg(event.toVersion)
    }

    // A section title with its rule, shared by the long-text sections.
    component SectionTitle: ColumnLayout {
        property string text: ""
        Layout.fillWidth: true
        spacing: theme.spaceM
        Text {
            text: parent.text
            color: theme.foreground
            font.pixelSize: theme.fontTitle
            font.weight: Font.DemiBold
            Accessible.role: Accessible.Heading
        }
        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 1
            color: theme.outline
        }
    }

    // One theme-aware renderer for all repository prose: README, changelog and
    // release notes share typography, wrapping and safe external links.
    component MarkdownBody: Text {
        property string markdown: ""
        Layout.fillWidth: true
        text: markdown ? backend.readmeForDisplay(markdown) : ""
        textFormat: Text.MarkdownText
        wrapMode: Text.Wrap
        font.family: theme.fontFamily
        font.pixelSize: theme.fontReading
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

    Dialog {
        id: confirmUnverified
        objectName: "confirmUnverified"
        property bool forUpdate: false
        // Fixed when it opens: the page reloads under it once the job starts,
        // and a text changing while it closes makes the dialog resize in a loop.
        property string fileName: ""
        anchors.centerIn: parent
        // Fixed: a wrapping label would otherwise size it from its unwrapped text.
        contentWidth: 380
        modal: true
        title: forUpdate ? qsTr("Update without a checksum?") : qsTr("Install without a checksum?")
        standardButtons: Dialog.Yes | Dialog.No
        Label {
            width: 380
            wrapMode: Text.Wrap
            color: theme.foreground
            text: qsTr("This release publishes no checksum for %1, so OmaStore cannot tell whether the download arrived intact.")
                  .arg(confirmUnverified.fileName)
        }
        onAccepted: forUpdate ? backend.update(page.app.repo, true) : backend.install(page.app.repo, true)
    }
    function askOrRun(forUpdate) {
        if (page.unverified) {
            confirmUnverified.forUpdate = forUpdate
            confirmUnverified.fileName = page.app.selectedAsset.name
            confirmUnverified.open()
        } else if (forUpdate) {
            backend.update(page.app.repo)
        } else {
            backend.install(page.app.repo)
        }
    }

    // Keyboard: i installs (or repairs), u updates. When the key cannot act,
    // the toast says why instead of doing nothing.
    function keyInstall() {
        if (page.busy)
            backend.notice(qsTr("%1 is busy; wait for it to finish.").arg(page.app.name))
        else if (page.installed && !page.broken)
            backend.notice(qsTr("%1 is already installed.").arg(page.app.name))
        else if (!page.app.installable)
            backend.notice(qsTr("The latest release has no Linux binary for this computer."))
        else if (page.blockedByProvenance)
            backend.notice(qsTr("This file has no build provenance, and Settings allow only files that have it."))
        else if (!backend.connected)
            backend.notice(qsTr("Not connected to omastored."))
        else
            page.askOrRun(false)
    }
    function keyUpdate() {
        if (page.busy)
            backend.notice(qsTr("%1 is busy; wait for it to finish.").arg(page.app.name))
        else if (!page.installed)
            backend.notice(qsTr("%1 is not installed; press i to install it.").arg(page.app.name))
        else if (!page.app.updateAvailable)
            backend.notice(qsTr("%1 is up to date.").arg(page.app.name))
        else if (page.blockedByProvenance)
            backend.notice(qsTr("This file has no build provenance, and Settings allow only files that have it."))
        else
            page.askOrRun(true)
    }
    Shortcut { sequence: "I"; enabled: page.visible; onActivated: page.keyInstall() }
    Shortcut { sequence: "U"; enabled: page.visible; onActivated: page.keyUpdate() }

    // Removing was refused: the app is open.
    Dialog {
        id: removeInUse
        objectName: "removeInUse"
        property string processes: ""
        anchors.centerIn: parent
        // Fixed: a wrapping label would otherwise size it from its unwrapped text.
        contentWidth: 380
        modal: true
        title: qsTr("%1 is open").arg(page.app.name || "")
        Label {
            width: 380
            wrapMode: Text.Wrap
            color: theme.foreground
            text: qsTr("Close it first: removing its files while it runs can make it crash or lose unsaved work.\n\nRunning: %1").arg(removeInUse.processes)
        }
        footer: DialogButtonBox {
            ActionButton {
                text: qsTr("Remove anyway")
                DialogButtonBox.buttonRole: DialogButtonBox.DestructiveRole
            }
            ActionButton {
                kind: "primary"
                text: qsTr("Keep it")
                DialogButtonBox.buttonRole: DialogButtonBox.RejectRole
            }
            onClicked: (button) => {
                if (button.DialogButtonBox.buttonRole === DialogButtonBox.DestructiveRole)
                    backend.uninstall(page.app.repo, true)
                removeInUse.close()
            }
        }
    }
    Connections {
        target: backend
        function onRemoveRefusedInUse(repo, processes) {
            if (repo.toLowerCase() !== page.repo.toLowerCase())
                return
            removeInUse.processes = processes
            removeInUse.open()
        }
    }

    Dialog {
        id: confirmRemove
        objectName: "confirmRemove"
        anchors.centerIn: parent
        // Fixed: a wrapping label would otherwise size it from its unwrapped text.
        contentWidth: 380
        modal: true
        title: qsTr("Remove %1?").arg(page.app.name || "")
        standardButtons: Dialog.Yes | Dialog.No
        Label {
            width: 380
            wrapMode: Text.Wrap
            color: theme.foreground
            text: qsTr("The app, its command and its menu entry are removed. Your own files and the app's settings in your home folder are kept.")
        }
        onAccepted: backend.uninstall(page.app.repo)
    }

    Flickable {
        id: flick
        anchors.fill: parent
        contentHeight: column.implicitHeight + theme.spaceXxl * 2
        clip: true
        ScrollBar.vertical: ScrollBar {}

        ColumnLayout {
            id: column
            objectName: "detailContent"
            // One content column, centered on wide windows, with the same
            // gutter as the catalog.
            width: Math.min(flick.width - theme.spaceXl * 2, 1200)
            x: Math.max(theme.spaceXl, (flick.width - width) / 2)
            y: theme.spaceXl
            spacing: theme.spaceXxl

            ActionButton {
                kind: "quiet"
                Layout.leftMargin: -theme.spaceS
                Layout.bottomMargin: -theme.spaceL
                text: qsTr("← Back")
                onClicked: page.backRequested()
            }

            BusyIndicator {
                visible: backend.detailLoading && !page.app.repo
                running: visible
            }

            // Header: icon, name, repository, summary, and the star.
            RowLayout {
                visible: !!page.app.repo
                Layout.fillWidth: true
                spacing: theme.spaceXl
                AppIcon {
                    Layout.alignment: Qt.AlignTop
                    Layout.preferredWidth: 96
                    Layout.preferredHeight: 96
                    url: page.app.iconUrl || ""
                    name: page.app.name || ""
                }
                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: theme.spaceS
                    // Name and repository shrink with the window instead of
                    // widening the page: a long name wraps, the link elides.
                    Text {
                        objectName: "detailName"
                        Layout.fillWidth: true
                        text: page.app.name || ""
                        color: theme.foreground
                        font.pixelSize: theme.fontHeadline
                        font.weight: Font.Bold
                        wrapMode: Text.Wrap
                        Accessible.role: Accessible.Heading
                    }
                    LinkText {
                        Layout.fillWidth: true
                        Layout.maximumWidth: implicitWidth
                        elide: Text.ElideMiddle
                        text: page.app.repo || ""
                        url: page.app.htmlUrl || ""
                        font.family: theme.monoFamily
                        font.pixelSize: theme.fontCaption
                        Accessible.description: qsTr("Opens the repository on GitHub")
                    }
                    Text {
                        Layout.fillWidth: true
                        Layout.topMargin: theme.spaceXs
                        text: page.app.summary || ""
                        color: theme.foreground
                        font.pixelSize: theme.fontSubtitle
                        wrapMode: Text.Wrap
                        lineHeight: 1.25
                    }
                    RowLayout {
                        spacing: theme.spaceS
                        Badge {
                            objectName: "categoryBadge"
                            visible: !!page.app.category
                            text: Categories.display(page.app.category)
                        }
                        Badge {
                            visible: page.installed
                            text: page.app.updateAvailable ? qsTr("update available")
                                  : qsTr("installed %1").arg(page.installed ? page.app.install.version : "")
                            tone: page.app.updateAvailable ? "warning" : "success"
                        }
                    }
                    // The repository's topics: what the app is made of and who
                    // it is for (the search weighs them). GitHub keeps them
                    // lowercase; they are words, not sentences.
                    Flow {
                        objectName: "topicBadges"
                        Layout.fillWidth: true
                        visible: topicRepeater.count > 0
                        spacing: theme.spaceS
                        Repeater {
                            id: topicRepeater
                            model: page.app.topics || []
                            delegate: Badge {
                                required property string modelData
                                objectName: "topicBadge"
                                text: modelData
                                Accessible.name: qsTr("Topic: %1").arg(modelData)
                            }
                        }
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
                                : starButton.starred ? qsTr("You starred this app on GitHub. Click to remove your star.")
                                : qsTr("Star this app on GitHub")
                    ActionButton {
                        id: starButton
                        objectName: "starButton"
                        anchors.fill: parent
                        readonly property bool starred: backend.starState === 1
                        // On: the accent fill, so it reads at a glance.
                        selected: starred
                        enabled: backend.connected && backend.starState !== -1 && !backend.starBusy
                        text: backend.starBusy ? (starred ? qsTr("Removing star…") : qsTr("Starring…"))
                              : (starred ? "★ " + qsTr("Starred") : "☆ " + qsTr("Star")) + "  ·  " + (page.app.stars || 0)
                        Accessible.role: Accessible.CheckBox
                        Accessible.checkable: true
                        Accessible.checked: starred
                        Accessible.name: qsTr("Star on GitHub, %n star(s)", "", page.app.stars || 0)
                        Accessible.description: backend.starHint
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
                columnSpacing: theme.spaceXl
                rowSpacing: theme.spaceXl

                ColumnLayout {
                    id: gallery
                    Layout.fillWidth: true
                    Layout.preferredWidth: hero.columns === 2 ? hero.width * 0.62 - theme.spaceXl / 2 : hero.width
                    Layout.alignment: Qt.AlignTop
                    spacing: theme.spaceM
                    visible: !!page.app.screenshots && page.app.screenshots.length > 0

                    // Pointer over the gallery: the slideshow waits.
                    HoverHandler { id: galleryHover }

                    SwipeView {
                        id: previewCarousel
                        objectName: "previewCarousel"
                        // Slideshow: advances every autoplayInterval and wraps
                        // around. It waits while the pointer or the keyboard
                        // focus is on the gallery, while the window is in the
                        // background and after the user pauses it; with
                        // reduced motion it starts paused (WCAG 2.2.2).
                        property bool autoplay: true
                        property int autoplayInterval: 5000
                        property bool userPaused: theme.reducedMotion
                        readonly property bool playing: autoplay && !userPaused && count > 1 && page.visible
                                                        && !galleryHover.hovered && !activeFocus
                                                        && Qt.application.state === Qt.ApplicationActive
                        Layout.fillWidth: true
                        Layout.preferredHeight: hero.columns === 2 ? 360 : Math.min(320, hero.width * 0.62)
                        clip: true
                        activeFocusOnTab: true
                        Keys.onLeftPressed: decrementCurrentIndex()
                        Keys.onRightPressed: incrementCurrentIndex()
                        onCurrentIndexChanged: if (autoplayTimer.running) autoplayTimer.restart()

                        Timer {
                            id: autoplayTimer
                            interval: previewCarousel.autoplayInterval
                            repeat: true
                            running: previewCarousel.playing
                            onTriggered: previewCarousel.setCurrentIndex((previewCarousel.currentIndex + 1) % previewCarousel.count)
                        }

                        Repeater {
                            model: page.app.screenshots || []
                            Rectangle {
                                required property string modelData
                                required property int index
                                radius: theme.radiusM
                                color: theme.surface
                                border.color: theme.outline
                                Image {
                                    Accessible.role: Accessible.Graphic
                                    Accessible.name: qsTr("Screenshot %1 of %2 of %3").arg(parent.index + 1)
                                                         .arg(previewCarousel.count).arg(page.app.name || "")
                                    anchors.fill: parent
                                    anchors.margins: theme.spaceS
                                    asynchronous: true
                                    fillMode: Image.PreserveAspectFit
                                    sourceSize: Qt.size(1280, 800)
                                    source: "image://omastore/" + encodeURIComponent(modelData)
                                    BusyIndicator { anchors.centerIn: parent; running: parent.status === Image.Loading }
                                }
                            }
                        }
                    }

                    // The buttons shrink (and elide) rather than widen the
                    // gallery past a narrow window or under a large font.
                    RowLayout {
                        objectName: "previewControls"
                        Layout.fillWidth: true
                        visible: previewCarousel.count > 1
                        spacing: theme.spaceS
                        ActionButton {
                            objectName: "previewPrevious"
                            Layout.fillWidth: true
                            Layout.maximumWidth: implicitWidth
                            Layout.minimumWidth: leftPadding + rightPadding + theme.fontBody * 2
                            text: qsTr("‹ Previous")
                            enabled: previewCarousel.currentIndex > 0
                            onClicked: previewCarousel.decrementCurrentIndex()
                        }
                        Item { Layout.fillWidth: true }
                        Text {
                            objectName: "previewPosition"
                            text: qsTr("%1 / %2").arg(previewCarousel.currentIndex + 1).arg(previewCarousel.count)
                            color: theme.muted
                            font.pixelSize: theme.fontCaption
                        }
                        ActionButton {
                            objectName: "previewPlayPause"
                            Layout.fillWidth: true
                            Layout.maximumWidth: implicitWidth
                            Layout.minimumWidth: leftPadding + rightPadding + theme.fontBody * 2
                            kind: "quiet"
                            visible: previewCarousel.count > 1
                            text: previewCarousel.userPaused ? qsTr("▶ Play") : qsTr("❚❚ Pause")
                            Accessible.name: previewCarousel.userPaused ? qsTr("Play slideshow") : qsTr("Pause slideshow")
                            onClicked: previewCarousel.userPaused = !previewCarousel.userPaused
                        }
                        Item { Layout.fillWidth: true }
                        ActionButton {
                            objectName: "previewNext"
                            Layout.fillWidth: true
                            Layout.maximumWidth: implicitWidth
                            Layout.minimumWidth: leftPadding + rightPadding + theme.fontBody * 2
                            text: qsTr("Next ›")
                            enabled: previewCarousel.currentIndex < previewCarousel.count - 1
                            onClicked: previewCarousel.incrementCurrentIndex()
                        }
                    }

                    ListView {
                        id: previewStrip
                        objectName: "previewStrip"
                        Layout.fillWidth: true
                        Layout.preferredHeight: 76
                        visible: count > 1
                        orientation: ListView.Horizontal
                        spacing: theme.spaceS
                        clip: true
                        model: page.app.screenshots || []
                        delegate: Rectangle {
                            required property string modelData
                            required property int index
                            readonly property bool current: index === previewCarousel.currentIndex
                            width: 120
                            height: 72
                            radius: theme.radiusS
                            color: theme.surface
                            border.width: current ? 3 : 1
                            border.color: current ? theme.focus : theme.outline
                            Image {
                                anchors.fill: parent
                                anchors.margins: 4
                                source: "image://omastore/" + encodeURIComponent(modelData)
                                fillMode: Image.PreserveAspectFit
                                asynchronous: true
                                sourceSize: Qt.size(240, 144)
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
                    Layout.preferredWidth: hero.columns === 2 ? hero.width * 0.38 - theme.spaceXl / 2 : hero.width
                    Layout.alignment: Qt.AlignTop
                    spacing: theme.spaceM

                    // Actions first: they are why people open the page.
                    PrimaryButton {
                        objectName: "installButton"
                        Layout.fillWidth: true
                        visible: !page.installed && !page.busy
                        enabled: backend.connected && !!page.app.installable && !page.blockedByProvenance
                        text: page.app.installable ? qsTr("Install") : qsTr("No Linux binary")
                        onClicked: page.askOrRun(false)
                    }
                    // Next to the button it is about.
                    Text {
                        objectName: "uncheckedHint"
                        Layout.fillWidth: true
                        visible: page.unverified && !page.busy && !!page.app.installable && (!page.installed || !!page.app.updateAvailable)
                        text: qsTr("⚠ This release publishes no checksum for this file; you will be asked first.")
                        color: theme.warning
                        font.pixelSize: theme.fontCaption
                        wrapMode: Text.Wrap
                    }
                    // Which code built the file: a verified GitHub attestation
                    // from a workflow of the repository itself.
                    Text {
                        objectName: "provenanceHint"
                        Layout.fillWidth: true
                        visible: !!page.app.installable && !page.unverified && !page.busy
                                 && (!page.installed || !!page.app.updateAvailable || !!page.provenance)
                        text: page.provenance
                              ? qsTr("✓ Built by this repository's GitHub Actions (%1, %2)")
                                    .arg(page.provenance.workflow.split("/").pop())
                                    .arg(page.provenance.ref.replace(/^refs\/(tags|heads)\//, ""))
                              : page.blockedByProvenance
                                ? qsTr("⚠ No build provenance, and Settings allow only files that have it.")
                                : qsTr("No build provenance: nothing shows which code built this file.")
                        color: page.provenance ? theme.success : page.blockedByProvenance ? theme.warning : theme.muted
                        font.pixelSize: theme.fontCaption
                        wrapMode: Text.Wrap
                    }
                    Text {
                        objectName: "notInstallableHint"
                        Layout.fillWidth: true
                        visible: !!page.app.repo && !page.app.installable && !page.installed
                        text: qsTr("The latest release has no Linux binary for this computer.")
                        color: theme.muted
                        font.pixelSize: theme.fontCaption
                        wrapMode: Text.Wrap
                    }
                    PrimaryButton {
                        Layout.fillWidth: true
                        visible: page.installed && !!page.app.updateAvailable && !page.busy
                        enabled: !page.blockedByProvenance
                        text: qsTr("Update to %1").arg(page.app.latestVersion)
                        onClicked: page.askOrRun(true)
                    }
                    // Its files were removed outside OmaStore: say so, offer the fix.
                    Text {
                        objectName: "brokenHint"
                        Layout.fillWidth: true
                        visible: page.broken && !page.busy
                        text: qsTr("⚠ The app's files are gone (removed outside OmaStore), so it cannot open.")
                        color: theme.warning
                        font.pixelSize: theme.fontCaption
                        wrapMode: Text.Wrap
                    }
                    PrimaryButton {
                        objectName: "repairButton"
                        Layout.fillWidth: true
                        visible: page.broken && !page.busy
                        enabled: backend.connected && !!page.app.installable
                        text: qsTr("Repair")
                        onClicked: page.askOrRun(false)
                    }
                    RowLayout {
                        Layout.fillWidth: true
                        visible: page.installed && !page.busy
                        spacing: theme.spaceS
                        ActionButton {
                            objectName: "openButton"
                            Layout.fillWidth: true
                            visible: !page.broken
                            text: qsTr("Open")
                            onClicked: backend.launch()
                        }
                        ActionButton {
                            objectName: "removeButton"
                            Layout.fillWidth: true
                            text: qsTr("Remove")
                            onClicked: confirmRemove.open()
                        }
                    }
                    // The version the last update replaced is still on disk.
                    ActionButton {
                        objectName: "rollbackButton"
                        Layout.fillWidth: true
                        kind: "quiet"
                        visible: page.installed && !page.busy && !!page.app.install.previousVersion
                        text: qsTr("Go back to %1").arg(page.installed ? page.app.install.previousVersion : "")
                        ToolTip.visible: hovered
                        ToolTip.delay: 400
                        ToolTip.text: qsTr("Use the version you had before the last update. Nothing is downloaded.")
                        onClicked: backend.rollback(page.app.repo)
                    }
                    // A running job, or a removal: what is happening, with a
                    // way out when it can be canceled.
                    ColumnLayout {
                        objectName: "busyBox"
                        visible: page.busy
                        Layout.fillWidth: true
                        spacing: theme.spaceS
                        Text {
                            objectName: "busyText"
                            Layout.fillWidth: true
                            text: backend.detailRemoving ? qsTr("Removing…") : backend.stageText(page.job.kind || "", page.job.stage || "")
                            color: theme.foreground
                            font.weight: Font.DemiBold
                            wrapMode: Text.Wrap
                        }
                        ProgressBar {
                            Layout.fillWidth: true
                            indeterminate: backend.detailRemoving || page.job.progress === undefined || page.job.progress < 0
                            value: page.job.progress > 0 ? page.job.progress : 0
                            Accessible.name: qsTr("Progress")
                        }
                        ActionButton {
                            kind: "quiet"
                            Layout.leftMargin: -theme.spaceS
                            visible: !!page.job.id
                            text: qsTr("Cancel")
                            onClicked: backend.cancelJob(page.job.id)
                        }
                    }
                    // The last install/update of this app failed: say why and
                    // offer a prefilled report to its author.
                    ColumnLayout {
                        objectName: "failureBox"
                        Layout.fillWidth: true
                        visible: backend.detailFailure !== "" && !page.busy
                        spacing: theme.spaceXs
                        Text {
                            Layout.fillWidth: true
                            text: qsTr("The last attempt failed: %1").arg(backend.detailFailure)
                            color: theme.danger
                            font.pixelSize: theme.fontCaption
                            wrapMode: Text.Wrap
                        }
                        LinkText {
                            text: qsTr("Report this problem to the author ↗")
                            font.pixelSize: theme.fontCaption
                            onActivated: Qt.openUrlExternally(backend.issueUrl())
                        }
                    }

                    // Facts.
                    Rectangle {
                        Layout.fillWidth: true
                        Layout.topMargin: theme.spaceS
                        Layout.preferredHeight: facts.implicitHeight + theme.spaceL * 2
                        radius: theme.radiusM
                        color: theme.surface
                        border.color: theme.outline

                        ColumnLayout {
                            id: facts
                            anchors.fill: parent
                            anchors.margins: theme.spaceL
                            spacing: theme.spaceM

                            GridLayout {
                                Layout.fillWidth: true
                                columns: 2
                                columnSpacing: theme.spaceL
                                rowSpacing: theme.spaceM
                                Fact { label: qsTr("Version"); value: page.app.latestVersion || qsTr("Unavailable"); mono: !!page.app.latestVersion }
                                Fact { label: qsTr("License"); value: page.app.license || qsTr("Not specified") }
                                Fact { label: qsTr("Stars"); value: String(page.app.stars || 0) }
                                Fact {
                                    visible: page.installed
                                    label: qsTr("Installed")
                                    value: page.installed ? page.app.install.version : ""
                                    mono: true
                                }
                            }
                            Rectangle {
                                Layout.fillWidth: true
                                Layout.preferredHeight: 1
                                color: theme.outline
                            }
                            ColumnLayout {
                                objectName: "installHistory"
                                Layout.fillWidth: true
                                spacing: theme.spaceS
                                visible: page.installed && (page.app.install.history || []).length > 0
                                Text {
                                    text: qsTr("Version history")
                                    color: theme.muted
                                    font.pixelSize: theme.fontCaption
                                    font.weight: Font.DemiBold
                                }
                                Repeater {
                                    objectName: "installHistoryRepeater"
                                    model: page.installed ? page.app.install.history : []
                                    delegate: Text {
                                        Layout.fillWidth: true
                                        text: page.installEventText(modelData) + " · " + (modelData.at || "").slice(0, 10)
                                        color: theme.foreground
                                        font.pixelSize: theme.fontCaption
                                        wrapMode: Text.Wrap
                                    }
                                }
                            }
                            ColumnLayout {
                                Layout.fillWidth: true
                                spacing: theme.spaceS
                                visible: page.installed && (page.app.install.services || []).length > 0
                                Text { text: qsTr("Managed user services"); color: theme.muted; font.pixelSize: theme.fontCaption; font.weight: Font.DemiBold }
                                Repeater {
                                    model: page.installed ? page.app.install.services : []
                                    delegate: Text {
                                        Layout.fillWidth: true
                                        text: modelData.unit + (modelData.enabledByStore ? qsTr(" · enabled") : "") + (modelData.startedByStore ? qsTr(" · started") : "")
                                        color: theme.foreground
                                        font.pixelSize: theme.fontCaption
                                        wrapMode: Text.Wrap
                                    }
                                }
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
                                text: qsTr("Report a problem ↗")
                                onActivated: Qt.openUrlExternally(backend.issueUrl())
                            }
                        }
                    }

                    // System dependencies declared in the app's PKGBUILD.
                    Rectangle {
                        id: depsBox
                        objectName: "depsBox"
                        readonly property var deps: backend.deps.deps || []
                        readonly property var libraries: backend.deps.libraries || []
                        readonly property string wrongArch: backend.deps.wrongArch || ""
                        readonly property var toInstall: backend.deps.toInstall || []
                        readonly property bool libraryLookupUnknown: {
                            for (const l of libraries) if (l.status === "unknown") return true
                            return false
                        }
                        Layout.fillWidth: true
                        Layout.preferredHeight: depsContent.implicitHeight + theme.spaceL * 2
                        visible: deps.length > 0 || libraries.length > 0 || wrongArch !== ""
                        radius: theme.radiusM
                        color: theme.surface
                        border.color: theme.outline

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
                            anchors.margins: theme.spaceL
                            spacing: theme.spaceS

                            Text {
                                text: qsTr("System dependencies").toUpperCase()
                                color: theme.muted
                                font.pixelSize: theme.fontCaption
                                font.weight: Font.DemiBold
                                font.letterSpacing: 0.8
                                Accessible.name: qsTr("System dependencies")
                                Accessible.role: Accessible.Heading
                            }
                            Repeater {
                                model: depsBox.deps
                                RowLayout {
                                    required property var modelData
                                    Layout.fillWidth: true
                                    spacing: theme.spaceS
                                    Text {
                                        Layout.fillWidth: true
                                        text: modelData.name + (modelData.optional ? " " + qsTr("(optional)") : "")
                                        color: theme.foreground
                                        font.family: theme.monoFamily
                                        elide: Text.ElideRight
                                        ToolTip.visible: depHover.hovered && !!modelData.reason
                                        ToolTip.text: modelData.reason || ""
                                        HoverHandler { id: depHover }
                                    }
                                    Text {
                                        text: depsBox.statusText(modelData)
                                        color: modelData.status === "installed" ? theme.success
                                             : modelData.status === "unavailable" ? theme.warning : theme.accent
                                        font.pixelSize: theme.fontCaption
                                        // The check mark alone says nothing to a screen reader.
                                        Accessible.name: modelData.status === "installed" ? qsTr("installed") : text
                                    }
                                }
                            }
                            // Read from the installed executable (never run).
                            Text {
                                objectName: "wrongArchWarning"
                                Layout.fillWidth: true
                                visible: depsBox.wrongArch !== ""
                                text: qsTr("⚠ The installed file is built for %1 and cannot run on this computer. Report it to the author.").arg(depsBox.wrongArch)
                                color: theme.danger
                                wrapMode: Text.Wrap
                            }
                            Repeater {
                                model: depsBox.libraries
                                RowLayout {
                                    required property var modelData
                                    Layout.fillWidth: true
                                    spacing: theme.spaceS
                                    Text {
                                        Layout.fillWidth: true
                                        text: modelData.name
                                        color: theme.foreground
                                        font.family: theme.monoFamily
                                        elide: Text.ElideMiddle
                                        ToolTip.visible: libHover.hovered
                                        ToolTip.text: qsTr("A library the app needs to start; this system does not have it.")
                                        HoverHandler { id: libHover }
                                    }
                                    Text {
                                        text: modelData.status === "available" ? qsTr("missing · %1").arg(modelData.package)
                                              : modelData.status === "unavailable" ? qsTr("not in pacman")
                                              : qsTr("missing library")
                                        color: modelData.status === "available" ? theme.accent : theme.warning
                                        font.pixelSize: theme.fontCaption
                                    }
                                }
                            }
                            Text {
                                Layout.fillWidth: true
                                visible: depsBox.libraryLookupUnknown
                                text: qsTr("To find which package ships a library, enable pacman's file database: sudo pacman -Fy")
                                color: theme.muted
                                font.pixelSize: theme.fontCaption
                                wrapMode: Text.Wrap
                            }
                            ActionButton {
                                objectName: "installDepsButton"
                                Layout.fillWidth: true
                                Layout.topMargin: theme.spaceXs
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
                                font.pixelSize: theme.fontCaption
                                wrapMode: Text.Wrap
                            }
                        }
                    }

                    LinkText {
                        objectName: "authorCheckLink"
                        Layout.fillWidth: true
                        visible: !!page.app.repo
                        text: page.app.installable ? qsTr("Is this your app? Check how it looks to the store →")
                                                   : qsTr("Is this your app? See what is missing →")
                        font.pixelSize: theme.fontCaption
                        wrapMode: Text.Wrap
                        onActivated: page.publishRequested(page.app.repo)
                    }
                }
            }

            // Release notes of the latest release, rendered like the README
            // (no remote images; links open only on click). Long text keeps a
            // readable line length and the column's left edge.
            ColumnLayout {
                objectName: "releaseNotes"
                Layout.fillWidth: true
                Layout.maximumWidth: 860
                visible: !!page.app.releaseNotes
                spacing: theme.spaceL
                SectionTitle {
                    text: page.installed && page.app.updateAvailable
                          ? qsTr("What's new in %1 (you have %2)").arg(page.app.latestVersion).arg(page.app.install.version)
                          : qsTr("What's new in %1").arg(page.app.latestVersion)
                }
                MarkdownBody {
                    Layout.fillWidth: true
                    markdown: page.app.releaseNotes || ""
                }
            }

            ColumnLayout {
                Layout.fillWidth: true
                Layout.maximumWidth: 860
                visible: !!page.app.readme
                spacing: theme.spaceL
                SectionTitle { text: qsTr("About") }
                MarkdownBody {
                    objectName: "detailReadme"
                    Layout.fillWidth: true
                    markdown: page.app.readme || ""
                }
            }

            ColumnLayout {
                objectName: "changelogSection"
                Layout.fillWidth: true
                Layout.maximumWidth: 860
                visible: !!page.app.changelog
                spacing: theme.spaceL
                SectionTitle { text: qsTr("Changelog") }
                MarkdownBody {
                    objectName: "detailChangelog"
                    markdown: page.app.changelog || ""
                }
            }

            // Similar apps (local recommendation from the daemon).
            ColumnLayout {
                Layout.fillWidth: true
                visible: backend.similar.length > 0
                spacing: theme.spaceL
                SectionTitle { text: qsTr("Similar apps") }
                Flow {
                    Layout.fillWidth: true
                    spacing: theme.spaceM
                    Repeater {
                        model: backend.similar
                        delegate: Rectangle {
                            id: simCard
                            required property var modelData
                            width: 260
                            implicitHeight: simRow.implicitHeight + theme.spaceM * 2
                            height: implicitHeight
                            radius: theme.radiusM
                            color: simArea.containsMouse || activeFocus ? theme.hover : theme.surface
                            border.color: activeFocus ? theme.focus : theme.outline
                            border.width: activeFocus ? 2 : 1
                            Behavior on color { ColorAnimation { duration: theme.durationShort } }
                            activeFocusOnTab: true
                            Accessible.role: Accessible.Button
                            Accessible.name: qsTr("%1, %2").arg(modelData.name).arg(Categories.display(modelData.category))
                            Accessible.onPressAction: page.appActivated(modelData.repo)
                            Keys.onReturnPressed: page.appActivated(modelData.repo)
                            Keys.onEnterPressed: page.appActivated(modelData.repo)
                            Keys.onSpacePressed: page.appActivated(modelData.repo)
                            RowLayout {
                                id: simRow
                                anchors.left: parent.left
                                anchors.right: parent.right
                                anchors.verticalCenter: parent.verticalCenter
                                anchors.margins: theme.spaceM
                                spacing: theme.spaceM
                                AppIcon {
                                    Layout.preferredWidth: 40
                                    Layout.preferredHeight: 40
                                    url: modelData.iconUrl
                                    name: modelData.name
                                }
                                ColumnLayout {
                                    Layout.fillWidth: true
                                    spacing: 2
                                    Text {
                                        Layout.fillWidth: true
                                        text: modelData.name
                                        color: theme.foreground
                                        font.weight: Font.DemiBold
                                        elide: Text.ElideRight
                                    }
                                    Text {
                                        Layout.fillWidth: true
                                        text: Categories.display(modelData.category)
                                        color: theme.muted
                                        font.pixelSize: theme.fontCaption
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
