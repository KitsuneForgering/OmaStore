import QtQuick

// Ícone de app carregado pelo daemon; mostra a inicial enquanto não há imagem.
Item {
    id: root
    property string url: ""
    property string name: ""
    implicitWidth: 64
    implicitHeight: 64

    Rectangle {
        anchors.fill: parent
        radius: width * 0.2
        color: theme.selection
        visible: img.status !== Image.Ready
        Text {
            anchors.centerIn: parent
            text: root.name.length > 0 ? root.name[0].toUpperCase() : "?"
            color: theme.accent
            font.pixelSize: parent.height * 0.5
            font.bold: true
        }
    }

    Image {
        id: img
        anchors.fill: parent
        asynchronous: true
        fillMode: Image.PreserveAspectFit
        sourceSize: Qt.size(width * 2, height * 2)
        source: root.url !== "" ? "image://omastore/" + encodeURIComponent(root.url) : ""
    }
}
