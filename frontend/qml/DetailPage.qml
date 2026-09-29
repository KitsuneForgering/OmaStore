import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

Page {
    id: page
    signal backRequested()
    signal appActivated(string repo)

    readonly property var app: backend.detail
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
        case "download": return qsTr("Baixando")
        case "verify": return qsTr("Verificando")
        case "extract": return qsTr("Extraindo")
        case "integrate": return qsTr("Integrando ao sistema")
        case "done": return qsTr("Concluído")
        }
        return qsTr("Preparando")
    }

    Dialog {
        id: confirmUnverified
        anchors.centerIn: parent
        modal: true
        title: qsTr("Instalar sem verificação?")
        standardButtons: Dialog.Yes | Dialog.No
        Label {
            width: 360
            wrapMode: Text.Wrap
            text: qsTr("Esta release não publica checksum, então o OmaStore não consegue confirmar que o arquivo baixado é o que o autor publicou.")
        }
        onAccepted: backend.install(page.app.repo)
    }

    Dialog {
        id: confirmRemove
        anchors.centerIn: parent
        modal: true
        title: qsTr("Remover %1?").arg(page.app.name || "")
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
            x: 32
            y: 24
            width: flick.width - 64
            spacing: 20

            Button {
                text: qsTr("← Voltar")
                flat: true
                onClicked: page.backRequested()
            }

            BusyIndicator {
                visible: backend.detailLoading && !page.app.repo
                running: visible
            }

            RowLayout {
                visible: !!page.app.repo
                spacing: 20
                AppIcon {
                    Layout.preferredWidth: 96
                    Layout.preferredHeight: 96
                    url: page.app.iconUrl || ""
                    name: page.app.name || ""
                }
                ColumnLayout {
                    Layout.fillWidth: true
                    spacing: 4
                    Text {
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
                        color: theme.muted
                        font.pixelSize: 12
                        text: [
                            "★ " + (page.app.stars || 0),
                            page.app.category,
                            page.app.license,
                            page.app.latestVersion ? qsTr("versão %1").arg(page.app.latestVersion) : "",
                            page.installed ? qsTr("instalada: %1").arg(page.app.install.version) : "",
                        ].filter(s => !!s).join("  ·  ")
                    }
                }

                ColumnLayout {
                    Layout.alignment: Qt.AlignTop
                    spacing: 8

                    PrimaryButton {
                        visible: !page.installed && !page.busy
                        enabled: backend.connected && !!page.app.installable
                        text: page.app.installable ? qsTr("Instalar") : qsTr("Sem binário para Linux")
                        onClicked: page.unverified ? confirmUnverified.open() : backend.install(page.app.repo)
                    }
                    PrimaryButton {
                        visible: page.installed && !!page.app.updateAvailable && !page.busy
                        text: qsTr("Atualizar para %1").arg(page.app.latestVersion)
                        onClicked: backend.update(page.app.repo)
                    }
                    Button {
                        visible: page.installed && !page.busy
                        text: qsTr("Remover")
                        onClicked: confirmRemove.open()
                    }
                    ColumnLayout {
                        visible: page.busy
                        Layout.preferredWidth: 200
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
                            text: qsTr("Cancelar")
                            flat: true
                            onClicked: backend.cancelJob(page.job.id)
                        }
                    }
                    Text {
                        visible: page.unverified && !page.installed
                        text: qsTr("⚠ sem checksum")
                        color: theme.warning
                        font.pixelSize: 11
                    }
                }
            }

            // Screenshots
            ListView {
                Layout.fillWidth: true
                Layout.preferredHeight: 260
                visible: page.app.screenshots && page.app.screenshots.length > 0
                orientation: ListView.Horizontal
                spacing: 12
                clip: true
                model: page.app.screenshots || []
                delegate: Rectangle {
                    required property string modelData
                    width: 420
                    height: 260
                    radius: 6
                    color: theme.surface
                    Image {
                        anchors.fill: parent
                        anchors.margins: 4
                        asynchronous: true
                        fillMode: Image.PreserveAspectFit
                        sourceSize: Qt.size(840, 520)
                        source: "image://omastore/" + encodeURIComponent(modelData)
                        BusyIndicator { anchors.centerIn: parent; running: parent.status === Image.Loading }
                    }
                }
                ScrollBar.horizontal: ScrollBar {}
            }

            // Apps parecidos (recomendação local do daemon).
            ColumnLayout {
                Layout.fillWidth: true
                visible: backend.similar.length > 0
                spacing: 8
                Text {
                    text: qsTr("Apps parecidos")
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

            Text {
                Layout.fillWidth: true
                visible: !!page.app.readme
                text: page.app.readme ? backend.readmeForDisplay(page.app.readme) : ""
                textFormat: Text.MarkdownText
                wrapMode: Text.Wrap
                color: theme.foreground
                linkColor: theme.accent
                onLinkActivated: (link) => {
                    // Só abre links web no navegador; nada de esquemas locais.
                    if (link.startsWith("https://") || link.startsWith("http://"))
                        Qt.openUrlExternally(link)
                }
                HoverHandler { cursorShape: parent.hoveredLink ? Qt.PointingHandCursor : Qt.ArrowCursor }
            }
        }
    }
}
