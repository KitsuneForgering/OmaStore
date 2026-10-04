import QtQuick
import QtQuick.Controls
import QtQuick.Layouts
import "categories.js" as Categories

Rectangle {
    id: root
    color: theme.surface

    // The edge against the content, for themes where surface and background
    // are close.
    Rectangle {
        anchors.right: parent.right
        width: 1
        height: parent.height
        color: theme.outline
    }

    property string section: "discover"
    property bool detailActive: false
    signal sectionSelected(string section)
    signal categorySelected(string category)

    component NavItem: ItemDelegate {
        id: item
        property bool selected: false
        property string badge: ""
        Layout.fillWidth: true
        highlighted: selected
        leftPadding: theme.spaceM
        rightPadding: theme.spaceS
        topPadding: theme.spaceS
        bottomPadding: theme.spaceS
        Accessible.name: badge !== "" ? text + ", " + badge : text
        contentItem: RowLayout {
            spacing: theme.spaceS
            Text {
                Layout.fillWidth: true
                text: item.text
                color: item.selected ? theme.accent : theme.foreground
                font.weight: item.selected ? Font.DemiBold : Font.Normal
                elide: Text.ElideRight
            }
            Text {
                visible: item.badge !== ""
                text: item.badge
                color: theme.muted
                font.pixelSize: theme.fontCaption
            }
        }
        background: Rectangle {
            color: item.selected ? theme.selection : (item.hovered ? theme.hover : "transparent")
            radius: theme.radiusS
            border.color: theme.focus
            border.width: item.visualFocus ? 2 : 0
            Behavior on color { ColorAnimation { duration: theme.durationShort } }
            // Selected: a bar on the left, so the state does not rely on color alone.
            Rectangle {
                visible: item.selected
                anchors.left: parent.left
                anchors.verticalCenter: parent.verticalCenter
                width: 3
                height: parent.height - theme.spaceS * 2
                radius: 2
                color: theme.focus
            }
        }
    }

    ColumnLayout {
        anchors.fill: parent
        anchors.margins: theme.spaceM
        anchors.topMargin: theme.spaceXl
        spacing: 2

        Text {
            Layout.leftMargin: theme.spaceM
            text: "OmaStore"
            color: theme.accent
            font.pixelSize: theme.fontTitle
            font.weight: Font.Bold
            Layout.bottomMargin: theme.spaceL
            Accessible.role: Accessible.Heading
        }

        NavItem {
            text: qsTr("Discover")
            selected: !root.detailActive && root.section === "discover" && backend.catalog.category === ""
            onClicked: { backend.catalog.category = ""; root.sectionSelected("discover") }
        }
        NavItem {
            text: qsTr("Installed")
            selected: !root.detailActive && root.section === "installed"
            badge: backend.updatesAvailable > 0 ? qsTr("%n update(s)", "", backend.updatesAvailable) : ""
            onClicked: root.sectionSelected("installed")
        }

        Text {
            Layout.leftMargin: theme.spaceM
            text: qsTr("Categories").toUpperCase()
            color: theme.muted
            font.pixelSize: theme.fontCaption
            font.weight: Font.DemiBold
            font.letterSpacing: 0.8
            Layout.topMargin: theme.spaceXl
            Layout.bottomMargin: theme.spaceXs
            Accessible.name: qsTr("Categories")
            Accessible.role: Accessible.Heading
        }

        ListView {
            Layout.fillWidth: true
            Layout.fillHeight: true
            clip: true
            model: backend.categories
            delegate: NavItem {
                width: ListView.view.width
                text: Categories.display(modelData.name)
                badge: modelData.count
                selected: !root.detailActive && root.section === "discover" && backend.catalog.category === modelData.name
                onClicked: root.categorySelected(modelData.name)
            }
        }

        Rectangle {
            Layout.fillWidth: true
            Layout.preferredHeight: 1
            Layout.topMargin: theme.spaceS
            Layout.bottomMargin: theme.spaceS
            color: theme.outline
        }
        NavItem {
            objectName: "publishNav"
            text: qsTr("Publish your app")
            selected: !root.detailActive && root.section === "publish"
            onClicked: root.sectionSelected("publish")
            ToolTip.visible: hovered
            ToolTip.text: qsTr("For developers: get your app into OmaStore")
        }

        // OmaStore's own update: offered here, finished with a restart.
        Rectangle {
            id: selfCard
            objectName: "selfUpdateCard"
            readonly property var st: backend.selfStatus
            readonly property var job: { backend.jobs.revision; return backend.jobs.selfJob() }
            readonly property bool restart: backend.selfInstalled !== ""
            visible: restart || !!st.updateAvailable || !!job.id
            onVisibleChanged: if (visible) selfFade.restart()
            NumberAnimation on opacity { id: selfFade; from: 0; to: 1; duration: theme.durationMedium; running: false }
            Layout.fillWidth: true
            Layout.topMargin: theme.spaceS
            implicitHeight: selfColumn.implicitHeight + theme.spaceM * 2
            radius: theme.radiusM
            color: theme.background
            border.color: theme.accent
            border.width: 1

            ColumnLayout {
                id: selfColumn
                anchors.fill: parent
                anchors.margins: theme.spaceM
                spacing: theme.spaceS
                Text {
                    Layout.fillWidth: true
                    wrapMode: Text.Wrap
                    color: theme.foreground
                    text: selfCard.restart ? qsTr("OmaStore %1 is installed.").arg(backend.selfInstalled)
                          : selfCard.job.id ? qsTr("Updating OmaStore…")
                          : qsTr("OmaStore %1 is available (you have %2).").arg(selfCard.st.latest).arg(selfCard.st.version)
                }
                ProgressBar {
                    Layout.fillWidth: true
                    visible: !!selfCard.job.id
                    indeterminate: !(selfCard.job.total > 0)
                    value: selfCard.job.total > 0 ? selfCard.job.done / selfCard.job.total : 0
                    Accessible.name: qsTr("OmaStore update progress")
                }
                PrimaryButton {
                    objectName: "selfUpdateButton"
                    Layout.fillWidth: true
                    visible: !selfCard.job.id
                    enabled: backend.connected
                    text: selfCard.restart ? qsTr("Restart OmaStore") : qsTr("Update OmaStore")
                    onClicked: selfCard.restart ? backend.restartSelf() : backend.updateSelf()
                    ToolTip.visible: hovered && !selfCard.restart && !!selfCard.st.notes
                    ToolTip.delay: 400
                    ToolTip.text: qsTr("What's new:\n%1").arg(String(selfCard.st.notes).slice(0, 600))
                }
            }
        }

        ActionButton {
            Layout.fillWidth: true
            Layout.topMargin: theme.spaceS
            visible: !root.detailActive
            readonly property var job: { backend.jobs.revision; return backend.jobs.indexJob() }
            enabled: backend.connected && !job.id
            text: job.id ? qsTr("Refreshing catalog…") : qsTr("Refresh catalog")
            onClicked: backend.refreshIndex(false)
            ToolTip.visible: hovered
            ToolTip.text: qsTr("Looks for new apps and versions on GitHub (Ctrl+R)")
        }

        RowLayout {
            Layout.topMargin: theme.spaceS
            Layout.leftMargin: theme.spaceXs
            spacing: theme.spaceS
            Rectangle {
                Layout.preferredWidth: 8
                Layout.preferredHeight: 8
                radius: 4
                color: backend.connected ? theme.success : theme.danger
            }
            Text {
                text: backend.connected ? qsTr("connected") : qsTr("connecting to omastored…")
                color: theme.muted
                font.pixelSize: theme.fontCaption
            }
        }
    }
}
