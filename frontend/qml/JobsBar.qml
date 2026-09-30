import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

// Bottom strip with the running jobs.
Rectangle {
    id: bar
    implicitHeight: backend.jobs.runningCount > 0 ? list.contentHeight + 16 : 0
    visible: implicitHeight > 0
    color: theme.surface
    clip: true
    Behavior on implicitHeight { NumberAnimation { duration: 120 } }

    ListView {
        id: list
        anchors.fill: parent
        anchors.margins: 8
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
            height: jobState === "running" ? 32 : 0
            visible: jobState === "running"
            spacing: 12

            Text {
                Layout.preferredWidth: 280
                elide: Text.ElideRight
                color: theme.foreground
                text: kind === "index" ? qsTr("Indexing catalog") + (message ? " — " + message : "")
                      : (kind === "update" ? qsTr("Updating %1") : qsTr("Installing %1")).arg(repo)
            }
            ProgressBar {
                Layout.fillWidth: true
                indeterminate: progress < 0
                value: progress >= 0 ? progress : 0
            }
            Text {
                color: theme.muted
                text: kind !== "index" ? stage
                      : stage === "discover" ? qsTr("searching")
                      : stage === "state" ? qsTr("checking") : ""
            }
            Button {
                text: qsTr("Cancel")
                flat: true
                onClicked: backend.cancelJob(jobId)
            }
        }
    }
}
