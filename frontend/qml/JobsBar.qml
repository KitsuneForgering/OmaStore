import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

// Bottom strip with the running jobs.
Rectangle {
    id: bar
    implicitHeight: backend.jobs.runningCount > 0 ? list.contentHeight + theme.spaceS * 2 : 0
    visible: implicitHeight > 0
    color: theme.surface
    clip: true
    Behavior on implicitHeight { NumberAnimation { duration: theme.durationShort; easing.type: Easing.OutCubic } }

    Rectangle {
        anchors.top: parent.top
        width: parent.width
        height: 1
        color: theme.outline
    }

    ListView {
        id: list
        anchors.fill: parent
        anchors.margins: theme.spaceS
        anchors.leftMargin: theme.spaceXl
        anchors.rightMargin: theme.spaceXl
        interactive: false
        model: backend.jobs
        delegate: RowLayout {
            required property string jobId
            required property string kind
            required property string repo
            required property string jobState
            required property string stage
            required property real progress
            required property string message
            width: ListView.view.width
            height: jobState === "running" ? implicitHeight : 0
            visible: jobState === "running"
            spacing: theme.spaceM

            Text {
                Layout.preferredWidth: 300
                elide: Text.ElideRight
                font.weight: Font.Medium
                color: theme.foreground
                text: kind === "index" ? qsTr("Indexing catalog") + (message ? " — " + message : "")
                      : kind === "self" ? qsTr("Updating OmaStore")
                      : (kind === "update" ? qsTr("Updating %1")
                         : kind === "deps" ? qsTr("Installing dependencies of %1")
                         : qsTr("Installing %1")).arg(repo)
            }
            ProgressBar {
                Layout.fillWidth: true
                indeterminate: progress < 0
                value: progress >= 0 ? progress : 0
            }
            Text {
                objectName: "jobStage"
                color: theme.muted
                font.pixelSize: theme.fontCaption
                text: message && stage === "service" ? message : backend.stageText(kind, stage)
            }
            ActionButton {
                kind: "quiet"
                text: qsTr("Cancel")
                Accessible.name: qsTr("Cancel %1").arg(repo || qsTr("catalog refresh"))
                onClicked: backend.cancelJob(jobId)
            }
        }
    }
}
