#include "markdown.h"
#include "offlinenam.h"

#include <QNetworkAccessManager>
#include <QNetworkReply>
#include <QSignalSpy>
#include <QtTest>

class TestMarkdown : public QObject {
    Q_OBJECT

private slots:
    void stripImages_data()
    {
        QTest::addColumn<QString>("in");
        QTest::addColumn<QString>("out");
        QTest::newRow("md") << "a ![logo](https://x/l.png) b" << "a logo b";
        QTest::newRow("link com imagem") << "[![CI](https://x/b.svg)](https://x)" << "[CI](https://x)";
        QTest::newRow("ref") << "![shot][1]\n\n[1]: https://x/s.png" << "shot\n\n[1]: https://x/s.png";
        QTest::newRow("html alt") << "<p><img src=\"https://x/a.png\" alt=\"Tela\" width=10></p>" << "<p>Tela</p>";
        QTest::newRow("html sem alt") << "x<IMG SRC='https://x/a.png'/>y" << "xy";
        QTest::newRow("picture") << "<picture><source srcset=\"https://x/d.png\"><img src=\"https://x/l.png\"></picture>" << "";
        QTest::newRow("link normal fica") << "[site](https://omarchy.org)" << "[site](https://omarchy.org)";
    }

    void stripImages()
    {
        QFETCH(QString, in);
        QFETCH(QString, out);
        QCOMPARE(Markdown::stripImages(in), out);
    }

    void offlineNamBlocksRemote()
    {
        QVERIFY(isLocalUrl(QUrl("file:///tmp/a.png")));
        QVERIFY(isLocalUrl(QUrl("qrc:/x.qml")));
        QVERIFY(!isLocalUrl(QUrl("https://example.com/a.png")));
        QVERIFY(!isLocalUrl(QUrl("http://example.com/a.png")));

        OfflineNamFactory f;
        std::unique_ptr<QNetworkAccessManager> nam(f.create(nullptr));
        QTest::ignoreMessage(QtWarningMsg, QRegularExpression("acesso de rede bloqueado"));
        QNetworkReply *r = nam->get(QNetworkRequest(QUrl("https://example.com/a.png")));
        QSignalSpy done(r, &QNetworkReply::finished);
        QTRY_COMPARE(done.count(), 1);
        QVERIFY(r->error() != QNetworkReply::NoError);
        QVERIFY(r->readAll().isEmpty());
        r->deleteLater();
    }
};

QTEST_GUILESS_MAIN(TestMarkdown)
#include "tst_markdown.moc"
