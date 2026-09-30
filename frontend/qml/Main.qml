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
        highlight: theme.accent
        highlightedText: theme.background
        placeholderText: theme.muted
        mid: theme.selection
        link: theme.accent
        linkVisited: theme.accent
    }

    // "discover" or "installed"
    property string section: "discover"

    function openApp(repo) {
        backend.openDetail(repo)
        if (stack.depth === 1)
            stack.push(detailPage)
    }
    function back() {
        if (stack.depth > 1) {
            stack.pop()
            backend.closeDetail()
        }
    }

    Component.onCompleted: if (startupRepo !== "") openApp(startupRepo)

    Shortcut { sequence: "/"; onActivated: catalogPage.focusSearch() }
    Shortcut { sequences: [StandardKey.Find]; onActivated: catalogPage.focusSearch() }
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
                initialItem: catalogPage
            }

            JobsBar {
                Layout.fillWidth: true
            }
        }
    }

    CatalogPage {
        id: catalogPage
        visible: false
        model: window.section === "installed" ? backend.installed : backend.catalog
        installedView: window.section === "installed"
        onAppActivated: (repo) => window.openApp(repo)
    }

    Component {
        id: detailPage
        DetailPage {
            onBackRequested: window.back()
            onAppActivated: (repo) => { backend.openDetail(repo); flickToTop() }
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
        color: isError ? theme.danger : theme.surface
        border.color: theme.selection
        opacity: 0
        visible: opacity > 0
        Behavior on opacity { NumberAnimation { duration: 150 } }

        Text {
            id: toastText
            anchors.centerIn: parent
            width: parent.width - 32
            wrapMode: Text.Wrap
            horizontalAlignment: Text.AlignHCenter
            color: toast.isError ? theme.background : theme.foreground
        }
        Timer { id: toastTimer; interval: 5000; onTriggered: toast.opacity = 0 }
        function show(msg, err) {
            text = msg
            isError = err
            opacity = 1
            toastTimer.restart()
        }
        MouseArea { anchors.fill: parent; onClicked: toast.opacity = 0 }
    }

    Connections {
        target: backend
        function onErrorOccurred(message) { toast.show(message, true) }
        function onNotice(message) { toast.show(message, false) }
    }
}
