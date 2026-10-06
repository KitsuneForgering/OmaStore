#include "singleinstance.h"

#include <QSignalSpy>
#include <QTemporaryDir>
#include <QtTest>

#include <future>

class TestSingleInstance : public QObject {
    Q_OBJECT

private slots:
    void links_data()
    {
        QTest::addColumn<QString>("link");
        QTest::addColumn<QString>("repo");
        QTest::newRow("full") << "omastore://demo/app" << "demo/app";
        QTest::newRow("trailing slash") << "omastore://demo/app/" << "demo/app";
        QTest::newRow("no slashes") << "omastore:demo/app" << "demo/app";
        QTest::newRow("dots in repo") << "omastore://Demo-1/app.qt_6" << "Demo-1/app.qt_6";
        QTest::newRow("other scheme") << "https://github.com/demo/app" << "";
        QTest::newRow("no repo") << "omastore://demo" << "";
        QTest::newRow("extra path") << "omastore://demo/app/releases" << "";
        QTest::newRow("traversal") << "omastore://demo/.." << "";
        QTest::newRow("query") << "omastore://demo/app?x=1" << "";
        QTest::newRow("bad owner") << "omastore://-demo/app" << "";
    }
    void links()
    {
        QFETCH(QString, link);
        QFETCH(QString, repo);
        QCOMPARE(repoFromLink(link), repo);
    }

    // A second launch hands its request to the first one.
    void forwardsToTheRunningInstance()
    {
        QTemporaryDir dir;
        const QString path = dir.filePath(QStringLiteral("gui.sock"));
        SingleInstance first(path);
        QVERIFY(!first.forward({}));
        QVERIFY(first.listen());
        QSignalSpy requested(&first, &SingleInstance::requested);

        // forward() blocks, so the second instance runs on another thread
        // while this one's event loop serves the socket.
        SingleInstance second(path);
        auto done = std::async(std::launch::async, [&second] {
            return second.forward({QStringLiteral("demo/app"), {}, {}});
        });
        QTRY_VERIFY(done.wait_for(std::chrono::seconds(0)) == std::future_status::ready);
        QVERIFY(done.get());
        QCOMPARE(requested.count(), 1);
        QCOMPARE(requested.at(0).at(0).value<StartRequest>().open, QStringLiteral("demo/app"));
    }

    // A socket left behind by a crashed instance does not block the next one.
    void replacesAStaleSocket()
    {
        QTemporaryDir dir;
        const QString path = dir.filePath(QStringLiteral("gui.sock"));
        QFile stale(path);
        QVERIFY(stale.open(QIODevice::WriteOnly));
        stale.close();
        SingleInstance instance(path);
        QVERIFY(!instance.forward({}));
        QVERIFY(instance.listen());
    }
};

QTEST_MAIN(TestSingleInstance)
#include "tst_singleinstance.moc"
