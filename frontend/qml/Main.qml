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

    Component.onCompleted: {
        if (startupCheck !== "")
            openPublish(startupCheck)
        else if (startupRepo !== "")
            openApp(startupRepo)
        else if (["discover", "installed", "publish"].indexOf(startupPage) >= 0)
            window.section = startupPage
    }

    Shortcut { sequence: "/"; enabled: window.section !== "publish"; onActivated: catalogPage.focusSearch() }
    Shortcut { sequences: [StandardKey.Find]; enabled: window.section !== "publish"; onActivated: catalogPage.focusSearch() }
    Shortcut { sequence: "Esc"; onActivated: window.back() }
    Shortcut { sequences: [StandardKey.Refresh, "Ctrl+R"]; onActivated: backend.refreshIndex(false) }

    RowLayout {
        anchors.fill: parent
        spacing: 0

        SideBar {
            Layout.fillHeight: true
            Layout.preferredWidth: stack.depth > 1 ? 176 : 220
            detailActive: stack.depth > 1
            section: window.section
            onSectionSelected: (s) => { window.section = s; window.back() }
            onCategorySelected: (c) => { window.section = "discover"; backend.catalog.category = c; window.back() }
        }

        ColumnLayout {
            Layout.fillWidth: true
            Layout.fillHeight: true
            spacing: 0

            StackView {
                id: stack
                Layout.fillWidth: true
                Layout.fillHeight: true
                initialItem: home
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
        anchors.bottomMargin: 72
        width: Math.min(toastText.implicitWidth + 32, parent.width - 64)
        height: toastText.implicitHeight + 20
        radius: 6
        color: isError ? theme.dangerFill : theme.surface
        border.color: theme.border
        Accessible.role: Accessible.AlertMessage
        Accessible.name: toastText.text
        opacity: 0
        visible: opacity > 0
        Behavior on opacity { NumberAnimation { duration: 150 } }

        Text {
            id: toastText
            anchors.centerIn: parent
            width: parent.width - 32
            wrapMode: Text.Wrap
            horizontalAlignment: Text.AlignHCenter
            color: toast.isError ? theme.onDanger : theme.foreground
        }
        // Notices go away after a while (not while the pointer is on them);
        // errors stay until dismissed, so there is time to read them.
        Timer {
            id: toastTimer
            interval: 6000
            running: toast.opacity > 0 && !toast.isError && !toastArea.containsMouse
            onTriggered: toast.opacity = 0
        }
        function show(msg, err) {
            text = msg
            isError = err
            opacity = 1
            toastTimer.restart()
        }
        MouseArea { id: toastArea; anchors.fill: parent; hoverEnabled: true; onClicked: toast.opacity = 0 }
    }

    // After an install: offer the missing system dependencies (PKGBUILD
    // depends and optdepends). The daemon asks for the password via polkit.
    Dialog {
        id: depsDialog
        objectName: "depsDialog"
        property string repo: ""
        property var packages: []
        anchors.centerIn: parent
        modal: true
        title: qsTr("Install system dependencies?")
        standardButtons: Dialog.Yes | Dialog.No
        Label {
            width: 400
            wrapMode: Text.Wrap
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
}
