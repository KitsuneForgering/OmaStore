import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

ApplicationWindow {
    id: window
    width: 1180
    height: 760
    minimumWidth: 720
    minimumHeight: 480
    visible: true
    title: "OmaStore"
    color: theme.background
    font.family: theme.fontFamily
    font.pixelSize: theme.fontBody

    palette {
        window: theme.background
        windowText: theme.foreground
        base: theme.surface
        text: theme.foreground
        button: theme.surface
        buttonText: theme.foreground
        highlight: theme.focus
        highlightedText: theme.onFocus
        placeholderText: theme.muted
        mid: theme.border
        link: theme.accent
        linkVisited: theme.accent
    }

    // "discover", "installed" or "publish"
    property string section: "discover"

    function openApp(repo) {
        backend.openDetail(repo)
        if (stack.depth === 1)
            stack.push(detailPage)
    }
    // Publish page for app authors, optionally checking a repository.
    function openPublish(repo) {
        window.section = "publish"
        window.back()
        if (repo)
            publishPage.start(repo)
    }
    function back() {
        if (stack.depth > 1) {
            stack.pop()
            backend.closeDetail()
        }
    }

    // What the command line (or a second launch, e.g. an omastore:// link)
    // asked to show.
    function handleRequest(req) {
        if (req.check)
            openPublish(req.check)
        else if (req.open)
            openApp(req.open)
        else if (["discover", "installed", "publish"].indexOf(req.page) >= 0) {
            window.section = req.page
            window.back()
        }
    }
    Component.onCompleted: handleRequest({open: startupRepo, check: startupCheck, page: startupPage})

    Shortcut { sequence: "/"; enabled: window.section !== "publish"; onActivated: catalogPage.focusSearch() }
    Shortcut { sequences: [StandardKey.Find]; enabled: window.section !== "publish"; onActivated: catalogPage.focusSearch() }
    Shortcut { sequence: "Esc"; onActivated: window.back() }
    Shortcut { sequences: [StandardKey.Refresh, "Ctrl+R"]; onActivated: backend.refreshIndex(false) }
    Shortcut { sequences: ["?", "Shift+?", "F1"]; onActivated: shortcutsDialog.open() }

    RowLayout {
        anchors.fill: parent
        spacing: 0

        SideBar {
            Layout.fillHeight: true
            // Wide enough for the category names at the user's font size.
            Layout.preferredWidth: Math.max(stack.depth > 1 ? 196 : 232, theme.fontBody * (stack.depth > 1 ? 13 : 16))
            detailActive: stack.depth > 1
            section: window.section
            onSectionSelected: (s) => { window.section = s; window.back() }
            onCategorySelected: (c) => { window.section = "discover"; backend.catalog.category = c; window.back() }
            onSettingsRequested: settingsDialog.open()
        }

        ColumnLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 0

            // The running daemon does not match this window (daemon.hello).
            Rectangle {
                objectName: "daemonBanner"
                Layout.fillWidth: true
                visible: backend.daemonWarning !== ""
                implicitHeight: bannerRow.implicitHeight + theme.spaceM * 2
                color: theme.surface
                Rectangle { anchors.bottom: parent.bottom; width: parent.width; height: 1; color: theme.warning }
                RowLayout {
                    id: bannerRow
                    anchors.fill: parent
                    anchors.leftMargin: theme.spaceXl
                    anchors.rightMargin: theme.spaceXl
                    spacing: theme.spaceM
                    Text {
                        Layout.fillWidth: true
                        text: "⚠ " + backend.daemonWarning + (backend.canRestartDaemon ? ""
                              : " " + qsTr("Close OmaStore and run: pkill -x omastored"))
                        color: theme.warning
                        wrapMode: Text.Wrap
                        Accessible.role: Accessible.AlertMessage
                    }
                    ActionButton {
                        visible: backend.canRestartDaemon
                        text: qsTr("Restart omastored")
                        onClicked: backend.restartDaemon()
                    }
                }
            }

            StackView {
                id: stack
                Layout.fillWidth: true
                Layout.fillHeight: true
                initialItem: home
                // Into an app's page: it slides in from the right and fades in;
                // back: the reverse. Instant with reduced motion.
                pushEnter: Transition {
                    NumberAnimation { property: "opacity"; from: 0; to: 1; duration: theme.durationMedium; easing.type: Easing.OutCubic }
                    NumberAnimation { property: "x"; from: 32; to: 0; duration: theme.durationMedium; easing.type: Easing.OutCubic }
                }
                pushExit: Transition {
                    NumberAnimation { property: "opacity"; from: 1; to: 0; duration: theme.durationMedium; easing.type: Easing.OutCubic }
                }
                popEnter: Transition {
                    NumberAnimation { property: "opacity"; from: 0; to: 1; duration: theme.durationMedium; easing.type: Easing.OutCubic }
                }
                popExit: Transition {
                    NumberAnimation { property: "opacity"; from: 1; to: 0; duration: theme.durationMedium; easing.type: Easing.OutCubic }
                    NumberAnimation { property: "x"; from: 0; to: 32; duration: theme.durationMedium; easing.type: Easing.OutCubic }
                }
            }

            JobsBar {
                Layout.fillWidth: true
            }
        }
    }

    // The stack's first page: the catalog or, for app authors, the publish page.
    Item {
        id: home
        visible: false
        CatalogPage {
            id: catalogPage
            anchors.fill: parent
            visible: window.section !== "publish"
            onVisibleChanged: if (visible) catalogFade.restart()
            NumberAnimation on opacity { id: catalogFade; from: 0; to: 1; duration: theme.durationMedium; running: false }
            model: window.section === "installed" ? backend.installed : backend.catalog
            installedView: window.section === "installed"
            onAppActivated: (repo) => window.openApp(repo)
            onPublishRequested: window.openPublish("")
            onDiscoverRequested: { window.section = "discover"; backend.catalog.category = "" }
        }
        PublishPage {
            id: publishPage
            anchors.fill: parent
            visible: window.section === "publish"
            onVisibleChanged: if (visible) publishFade.restart()
            NumberAnimation on opacity { id: publishFade; from: 0; to: 1; duration: theme.durationMedium; running: false }
        }
    }

    Component {
        id: detailPage
        DetailPage {
            onBackRequested: window.back()
            onAppActivated: (repo) => { backend.openDetail(repo); flickToTop() }
            onPublishRequested: (repo) => window.openPublish(repo)
        }
    }

    // Notices and errors coming from the backend.
    Rectangle {
        id: toast
        property bool isError: false
        property alias text: toastText.text
        anchors.horizontalCenter: parent.horizontalCenter
        anchors.bottom: parent.bottom
        anchors.bottomMargin: opacity > 0.99 ? 72 : 56
        Behavior on anchors.bottomMargin { NumberAnimation { duration: theme.durationMedium; easing.type: Easing.OutCubic } }
        width: Math.min(toastRow.implicitWidth + theme.spaceL * 2, parent.width - 64)
        height: toastRow.implicitHeight + theme.spaceM * 2
        radius: theme.radiusM
        color: isError ? theme.dangerFill : theme.surface
        border.color: isError ? theme.onDanger : theme.border
        Accessible.role: Accessible.AlertMessage
        Accessible.name: toastText.text
        opacity: 0
        visible: opacity > 0
        Behavior on opacity { NumberAnimation { duration: theme.durationMedium } }

        RowLayout {
            id: toastRow
            anchors.fill: parent
            anchors.leftMargin: theme.spaceL
            anchors.rightMargin: theme.spaceS
            anchors.topMargin: theme.spaceM
            anchors.bottomMargin: theme.spaceM
            spacing: theme.spaceM
            Text {
                id: toastText
                Layout.fillWidth: true
                Layout.maximumWidth: 560
                wrapMode: Text.Wrap
                color: toast.isError ? theme.onDanger : theme.foreground
            }
            // Errors stay until dismissed: say how, with a real button.
            Button {
                id: toastClose
                objectName: "toastClose"
                Layout.alignment: Qt.AlignTop
                text: "✕"
                flat: true
                hoverEnabled: true
                Accessible.name: qsTr("Dismiss")
                onClicked: toast.opacity = 0
                contentItem: Text {
                    text: toastClose.text
                    color: toast.isError ? theme.onDanger : theme.foreground
                    horizontalAlignment: Text.AlignHCenter
                    verticalAlignment: Text.AlignVCenter
                }
                background: Rectangle {
                    implicitWidth: 28
                    implicitHeight: 28
                    radius: theme.radiusS
                    color: "transparent"
                    border.color: toastClose.visualFocus ? theme.focus
                                : toast.isError ? theme.onDanger : theme.border
                    border.width: toastClose.visualFocus ? 2 : (toastClose.hovered ? 1 : 0)
                }
            }
        }
        // Notices go away after a while (not while the pointer is on them);
        // errors stay until dismissed, so there is time to read them.
        Timer {
            id: toastTimer
            interval: 6000
            running: toast.opacity > 0 && !toast.isError && !toastArea.hovered
            onTriggered: toast.opacity = 0
        }
        function show(msg, err) {
            text = msg
            isError = err
            opacity = 1
            toastTimer.restart()
        }
        HoverHandler { id: toastArea }
    }

    // After an install: offer the missing system dependencies (PKGBUILD
    // depends and optdepends). The daemon asks for the password via polkit.
    Dialog {
        id: depsDialog
        objectName: "depsDialog"
        property string repo: ""
        property var packages: []
        anchors.centerIn: parent
        // Fixed: a wrapping label would otherwise size it from its unwrapped text.
        contentWidth: 380
        modal: true
        title: qsTr("Install system dependencies?")
        standardButtons: Dialog.Yes | Dialog.No
        Label {
            width: 380
            wrapMode: Text.Wrap
            color: theme.foreground
            text: qsTr("%1 needs these packages to work fully:\n\n%2\n\nThey are installed with pacman, which asks for the administrator password.")
                  .arg(depsDialog.repo).arg(depsDialog.packages.join(", "))
        }
        onAccepted: backend.installDeps(repo)
    }

    Connections {
        target: backend
        function onErrorOccurred(message) { toast.show(message, true) }
        function onNotice(message) { toast.show(message, false) }
        function onDepsSuggested(repo, packages) {
            depsDialog.repo = repo
            depsDialog.packages = packages
            depsDialog.open()
        }
    }

    // "?" lists every shortcut, so they can be found without reading docs.
    Dialog {
        id: shortcutsDialog
        objectName: "shortcutsDialog"
        anchors.centerIn: parent
        modal: true
        title: qsTr("Keyboard shortcuts")
        standardButtons: Dialog.Close
        ColumnLayout {
            spacing: theme.spaceS
            TextMetrics { id: keyMetrics; font.family: theme.monoFamily; font.pixelSize: theme.fontBody; text: "h j k l / ←↓↑→" }
            Repeater {
                model: [
                    ["/", qsTr("Search")],
                    ["h j k l / ←↓↑→", qsTr("Move between apps")],
                    ["Enter", qsTr("Open the selected app")],
                    ["i", qsTr("Install the open app")],
                    ["u", qsTr("Update the open app")],
                    ["Esc", qsTr("Go back")],
                    ["Ctrl+R", qsTr("Look for new apps")],
                    ["?", qsTr("Show this list")]
                ]
                delegate: RowLayout {
                    required property var modelData
                    spacing: theme.spaceXl
                    Text {
                        Layout.preferredWidth: keyMetrics.advanceWidth
                        text: modelData[0]
                        color: theme.foreground
                        font.family: theme.monoFamily
                        font.weight: Font.DemiBold
                    }
                    Text { text: modelData[1]; color: theme.foreground }
                }
            }
        }
    }

    // Store-wide preferences, kept by the daemon (settings.*).
    Dialog {
        id: settingsDialog
        objectName: "settingsDialog"
        anchors.centerIn: parent
        width: Math.min(560, window.width - theme.spaceXxl * 2)
        modal: true
        title: qsTr("Settings")
        standardButtons: Dialog.Close
        ColumnLayout {
            width: parent.width
            spacing: theme.spaceL
            Repeater {
                model: [
                    { key: "autoUpdate", on: backend.autoUpdate,
                      title: qsTr("Update apps automatically"),
                      text: qsTr("Updates whose download can be verified install on their own after omarchy update and when OmaStore opens. You are told what changed, and Go back undoes any of them.") },
                    { key: "requireProvenance", on: backend.requireProvenance,
                      title: qsTr("Install only apps with build provenance"),
                      text: qsTr("Only files that GitHub attests were built by a workflow of the app's own repository. Most apps do not publish this yet, so many will not install.") }
                ]
                delegate: RowLayout {
                    required property var modelData
                    Layout.fillWidth: true
                    spacing: theme.spaceL
                    ColumnLayout {
                        Layout.fillWidth: true
                        spacing: theme.spaceXs
                        Text {
                            Layout.fillWidth: true
                            text: modelData.title
                            color: theme.foreground
                            font.weight: Font.DemiBold
                            wrapMode: Text.Wrap
                        }
                        Text {
                            Layout.fillWidth: true
                            text: modelData.text
                            color: theme.muted
                            font.pixelSize: theme.fontCaption
                            wrapMode: Text.Wrap
                        }
                    }
                    ActionButton {
                        objectName: "setting_" + modelData.key
                        selected: modelData.on
                        enabled: backend.connected && !backend.settingsBusy
                        text: modelData.on ? qsTr("On") : qsTr("Off")
                        Accessible.name: modelData.title
                        Accessible.role: Accessible.CheckBox
                        Accessible.checkable: true
                        Accessible.checked: modelData.on
                        onClicked: modelData.key === "autoUpdate" ? backend.setAutoUpdate(!modelData.on)
                                                                  : backend.setRequireProvenance(!modelData.on)
                    }
                }
            }
        }
    }
}
