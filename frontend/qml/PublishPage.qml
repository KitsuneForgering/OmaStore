import QtQuick
import QtQuick.Controls
import QtQuick.Layouts

// For app authors: how to get into the store and a checker that shows what
// the store understands of a repository (author.check), with a starter
// omastore.toml to copy.
Page {
    id: page
    readonly property var report: backend.authorCheck
    readonly property bool hasReport: !!report.repo
    readonly property string guideUrl: "https://github.com/KitsuneSemCalda/OmaStore/blob/master/docs/authors.md"
    readonly property string skillsUrl: "https://github.com/KitsuneSemCalda/OmaStore/tree/master/skills"
    readonly property string template: 'kind = "app"\n' +
        'name = "MyApp"\n' +
        'summary = "What it does, in one sentence"\n' +
        'icon = "assets/icon.svg"\n\n' +
        '[linux.x86_64]\n' +
        'asset = "myapp-{version}-x86_64-linux.tar.gz"\n'

    // Opens the page already checking a repository (e.g. from an app's page).
    function start(repo) {
        repoField.text = repo
        manifestArea.text = ""
        page.runCheck()
    }
    function runCheck() {
        backend.checkRepo(repoField.text, manifestArea.text, testManifest.checked)
    }
    function statusColor(s) {
        return s === "ok" ? theme.success : s === "warning" ? theme.warning : theme.danger
    }
    function statusGlyph(s) {
        return s === "ok" ? "✓" : s === "warning" ? "!" : "✕"
    }
    function count(status) {
        let n = 0
        for (const c of (report.checks || [])) if (c.status === status) n++
        return n
    }

    background: Rectangle { color: theme.background }

    component Card: Rectangle {
        default property alias content: inner.data
        property int padding: 20
        Layout.fillWidth: true
        implicitHeight: inner.implicitHeight + padding * 2
        radius: 8
        color: theme.surface
        border.color: theme.selection
        ColumnLayout {
            id: inner
            anchors.fill: parent
            anchors.margins: parent.padding
            spacing: 12
        }
    }

    component Heading: Text {
        Layout.fillWidth: true
        color: theme.foreground
        font.pixelSize: 17
        font.bold: true
        wrapMode: Text.Wrap
    }

    component Body: Text {
        Layout.fillWidth: true
        color: theme.foreground
        wrapMode: Text.Wrap
        lineHeight: 1.3
    }

    component Code: Rectangle {
        id: code
        property string text: ""
        property bool copyable: true
        Layout.fillWidth: true
        implicitHeight: codeColumn.implicitHeight + 20
        radius: 6
        color: theme.background
        border.color: theme.selection
        ColumnLayout {
            id: codeColumn
            anchors.fill: parent
            anchors.margins: 10
            spacing: 4
            Text {
                Layout.fillWidth: true
                text: code.text.replace(/\n+$/, "")
                color: theme.foreground
                font.family: "monospace"
                font.pixelSize: 13
                wrapMode: Text.Wrap
            }
            Button {
                Layout.alignment: Qt.AlignRight
                visible: code.copyable
                text: qsTr("Copy")
                flat: true
                onClicked: backend.copyText(code.text)
            }
        }
    }

    Flickable {
        id: flick
        anchors.fill: parent
        contentHeight: column.implicitHeight + 48
        clip: true
        ScrollBar.vertical: ScrollBar {}

        ColumnLayout {
            id: column
            objectName: "publishContent"
            x: Math.max(24, (flick.width - width) / 2)
            y: 24
            width: Math.min(flick.width - 48, 880)
            spacing: 24

            ColumnLayout {
                Layout.fillWidth: true
                spacing: 8
                Text {
                    text: qsTr("Publish your app")
                    color: theme.foreground
                    font.pixelSize: 28
                    font.bold: true
                }
                Body {
                    text: qsTr("OmaStore lists standalone Omarchy apps straight from GitHub. There is no sign-up: an omastore.toml at the root of your repository is the opt-in, and the store installs the Linux binary of your latest release.")
                }
            }

            // Checker.
            Card {
                objectName: "checkCard"
                Heading { text: qsTr("Check your repository") }
                Body {
                    text: qsTr("See what the store understands of your app and what is missing. Nothing is installed or added to your catalog.")
                }
                RowLayout {
                    Layout.fillWidth: true
                    spacing: 10
                    TextField {
                        id: repoField
                        objectName: "checkRepoField"
                        Layout.fillWidth: true
                        placeholderText: qsTr("owner/repo or https://github.com/owner/repo")
                        onAccepted: page.runCheck()
                    }
                    PrimaryButton {
                        objectName: "checkButton"
                        text: backend.authorCheckBusy ? qsTr("Checking…") : qsTr("Check")
                        enabled: backend.connected && !backend.authorCheckBusy && repoField.text.trim() !== ""
                        onClicked: page.runCheck()
                    }
                }
                CheckBox {
                    id: testManifest
                    text: qsTr("Test an omastore.toml before pushing it")
                    palette.windowText: theme.foreground
                    palette.mid: theme.muted
                    palette.base: theme.background
                }
                TextArea {
                    id: manifestArea
                    Layout.fillWidth: true
                    Layout.preferredHeight: 140
                    visible: testManifest.checked
                    placeholderText: qsTr("Paste your local omastore.toml here")
                    font.family: "monospace"
                    wrapMode: TextEdit.WrapAnywhere
                    color: theme.foreground
                    background: Rectangle {
                        color: theme.background
                        border.color: manifestArea.activeFocus ? theme.focus : theme.border
                        border.width: manifestArea.activeFocus ? 2 : 1
                        radius: 6
                    }
                }
                BusyIndicator {
                    Layout.alignment: Qt.AlignHCenter
                    visible: backend.authorCheckBusy
                    running: visible
                }
                Text {
                    Layout.fillWidth: true
                    visible: backend.authorCheckError !== ""
                    text: backend.authorCheckError
                    color: theme.danger
                    wrapMode: Text.Wrap
                }
            }

            // Report.
            Card {
                objectName: "checkReport"
                visible: page.hasReport && !backend.authorCheckBusy

                RowLayout {
                    Layout.fillWidth: true
                    spacing: 12
                    Rectangle {
                        Layout.preferredWidth: 36
                        Layout.preferredHeight: 36
                        radius: 18
                        color: page.report.compatible ? theme.success : theme.danger
                        Text {
                            anchors.centerIn: parent
                            text: page.report.compatible ? "✓" : "✕"
                            color: theme.background
                            font.pixelSize: 18
                            font.bold: true
                        }
                    }
                    ColumnLayout {
                        Layout.fillWidth: true
                        spacing: 2
                        Heading {
                            objectName: "checkVerdict"
                            text: !page.report.compatible ? qsTr("%1 is not in the store yet").arg(page.report.repo)
                                : page.report.localManifest ? qsTr("%1 works with this omastore.toml").arg(page.report.repo)
                                : qsTr("%1 is ready for OmaStore").arg(page.report.repo)
                        }
                        Text {
                            Layout.fillWidth: true
                            color: theme.muted
                            wrapMode: Text.Wrap
                            text: {
                                const fails = page.count("fail"), warns = page.count("warning")
                                const w = warns === 1 ? qsTr("1 warning") : qsTr("%1 warnings").arg(warns)
                                if (page.report.compatible && page.report.localManifest)
                                    return qsTr("Tested with your local file, not the published one: push it to the root of the default branch, then check again without it (%1 to polish).").arg(w)
                                if (page.report.compatible)
                                    return qsTr("%1 to polish.").arg(w)
                                const f = fails === 1 ? qsTr("1 problem to fix") : qsTr("%1 problems to fix").arg(fails)
                                return f + ", " + w + "."
                            }
                        }
                    }
                }

                // How the app would look in the catalog.
                Rectangle {
                    Layout.fillWidth: true
                    visible: !!page.report.name
                    implicitHeight: preview.implicitHeight + 24
                    radius: 6
                    color: theme.background
                    RowLayout {
                        id: preview
                        anchors.fill: parent
                        anchors.margins: 12
                        spacing: 12
                        AppIcon {
                            Layout.preferredWidth: 48
                            Layout.preferredHeight: 48
                            url: page.report.iconUrl || ""
                            name: page.report.name || ""
                        }
                        ColumnLayout {
                            Layout.fillWidth: true
                            spacing: 2
                            Text {
                                Layout.fillWidth: true
                                text: page.report.name || ""
                                color: theme.foreground
                                font.bold: true
                                font.pixelSize: 16
                                elide: Text.ElideRight
                            }
                            Text {
                                Layout.fillWidth: true
                                text: page.report.summary || ""
                                color: theme.foreground
                                wrapMode: Text.Wrap
                                maximumLineCount: 2
                                elide: Text.ElideRight
                            }
                            Text {
                                text: [page.report.category, page.report.tag].filter(s => !!s).join("  ·  ")
                                color: theme.muted
                                font.pixelSize: 12
                            }
                        }
                    }
                }

                Repeater {
                    model: page.report.checks || []
                    delegate: RowLayout {
                        required property var modelData
                        Layout.fillWidth: true
                        spacing: 12
                        Text {
                            Layout.alignment: Qt.AlignTop
                            Layout.preferredWidth: 18
                            horizontalAlignment: Text.AlignHCenter
                            text: page.statusGlyph(modelData.status)
                            color: page.statusColor(modelData.status)
                            font.bold: true
                        }
                        ColumnLayout {
                            Layout.fillWidth: true
                            spacing: 2
                            Text {
                                Layout.fillWidth: true
                                text: modelData.item
                                color: theme.foreground
                                font.bold: true
                                wrapMode: Text.Wrap
                            }
                            Text {
                                Layout.fillWidth: true
                                text: modelData.detail
                                color: theme.muted
                                wrapMode: Text.WrapAnywhere
                                maximumLineCount: 4
                                elide: Text.ElideRight
                            }
                            Text {
                                Layout.fillWidth: true
                                visible: modelData.status !== "ok" && !!modelData.fix
                                text: "→ " + modelData.fix
                                color: page.statusColor(modelData.status)
                                wrapMode: Text.Wrap
                            }
                        }
                    }
                }

                // What "ready" does and does not promise.
                ColumnLayout {
                    objectName: "readyNotes"
                    Layout.fillWidth: true
                    visible: !!page.report.compatible && !page.report.localManifest
                    spacing: 8
                    Heading { text: qsTr("When it shows up") }
                    Body {
                        text: qsTr("It is listed once a catalog refresh finds it. Users without a GitHub token only find repositories with the omarchy topic, and a repository checked before it had an omastore.toml can wait up to 7 days to be looked at again. To see it in your own catalog now:")
                    }
                    Code { text: "omastore index " + page.report.repo }
                    Body {
                        text: qsTr("This check reads your release; it does not download, install or run the app. Install it once to make sure the executable, its libraries and the menu entry work:")
                    }
                    Code { text: "omastore install " + page.report.repo }
                }

                ColumnLayout {
                    Layout.fillWidth: true
                    visible: !!page.report.suggestedManifest
                    spacing: 8
                    Heading { text: qsTr("Suggested omastore.toml") }
                    Body {
                        text: qsTr("Built from your release and repository. Commit it at the root of the default branch, then check again.")
                    }
                    Code {
                        objectName: "suggestedManifest"
                        text: page.report.suggestedManifest || ""
                    }
                }
            }

            // How it works.
            Heading { text: qsTr("Three steps to the store") }
            GridLayout {
                Layout.fillWidth: true
                columns: width >= 1040 ? 3 : 1
                columnSpacing: 16
                rowSpacing: 16

                Card {
                    Layout.alignment: Qt.AlignTop
                    Layout.preferredWidth: 1
                    Heading { text: qsTr("1. Add omastore.toml") }
                    Body {
                        text: qsTr("At the root of the default branch. It may be empty; every field is optional and fixes what the store would guess wrong.")
                    }
                    Code { text: page.template }
                }
                Card {
                    Layout.alignment: Qt.AlignTop
                    Layout.preferredWidth: 1
                    Heading { text: qsTr("2. Release a Linux binary") }
                    Body {
                        text: qsTr("A stable GitHub release with a portable tarball per architecture. GitHub's own sha256 digest is checked on install.")
                    }
                    Code { text: "myapp-1.2.0-x86_64-linux.tar.gz\nmyapp-1.2.0-aarch64-linux.tar.gz"; copyable: false }
                }
                Card {
                    Layout.alignment: Qt.AlignTop
                    Layout.preferredWidth: 1
                    Heading { text: qsTr("3. Check and share") }
                    Body {
                        text: qsTr("Use the checker above or the command line, then install it once. Catalogs pick it up on their next refresh; the omarchy topic makes it visible to users without a GitHub token too.")
                    }
                    Code { text: "omastore check owner/repo" }
                }
            }

            Body {
                objectName: "agentsHint"
                text: qsTr("Using a coding agent (Claude Code, Codex, OpenCode, Copilot, Gemini, Cursor, Pi, Hermes…)? OmaStore's install.sh adds its author skills to every installed agent; with the pacman package, copy them from /usr/share/omastore/skills. Then open your app's project and ask “get this app ready for OmaStore”.")
            }
            Flow {
                Layout.fillWidth: true
                spacing: 24
                LinkText { text: qsTr("Full authors guide ↗"); url: page.guideUrl }
                LinkText { text: qsTr("Agent skills for authors ↗"); url: page.skillsUrl }
            }
        }
    }
}
