#include "fakedaemon.h"
#include "rpcclient.h"

#include <QJsonArray>
#include <QSignalSpy>
#include <QtTest>

class TestRpcClient : public QObject {
    Q_OBJECT

private:
    static std::unique_ptr<RpcClient> connectTo(FakeDaemon &d)
    {
        auto c = std::make_unique<RpcClient>(d.path());
        c->setAutoStart(false);
        c->setReconnectInterval(50);
        c->start();
        return c;
    }

private slots:
    void callAndResponse()
    {
        FakeDaemon d;
        d.handler = [](const QString &m, const QJsonObject &p) {
            if (m == "echo")
                return QJsonObject{{"result", p}};
            return QJsonObject{{"error", QJsonObject{{"code", -32601}, {"message", "método desconhecido"}}}};
        };
        QVERIFY(d.listen());
        auto c = connectTo(d);
        QTRY_VERIFY(c->isConnected());

        QJsonValue got;
        RpcError gotErr{1, {}};
        c->call("echo", {{"x", 42}}, [&](const QJsonValue &r, const RpcError &e) { got = r; gotErr = e; });
        QTRY_VERIFY(gotErr.ok());
        QCOMPARE(got.toObject().value("x").toInt(), 42);

        RpcError err;
        c->call("nope", {}, [&](const QJsonValue &, const RpcError &e) { err = e; });
        QTRY_COMPARE(err.code, -32601);
        QCOMPARE(err.message, QStringLiteral("método desconhecido"));

        const QJsonObject sent = d.received.first();
        QCOMPARE(sent.value("jsonrpc").toString(), QStringLiteral("2.0"));
        QCOMPARE(sent.value("method").toString(), QStringLiteral("echo"));
    }

    void outOfOrderResponses()
    {
        FakeDaemon d;
        QList<QJsonObject> held;
        d.handler = [](const QString &, const QJsonObject &) { return QJsonObject{}; }; // não responde
        QVERIFY(d.listen());
        auto c = connectTo(d);
        QTRY_VERIFY(c->isConnected());

        QString first, second;
        const qint64 id1 = c->call("a", {}, [&](const QJsonValue &r, const RpcError &) { first = r.toString(); });
        const qint64 id2 = c->call("b", {}, [&](const QJsonValue &r, const RpcError &) { second = r.toString(); });
        QTRY_COMPARE(d.received.size(), 2);
        // Responde primeiro o segundo pedido, e numa escrita partida ao meio.
        const QByteArray r2 = QJsonDocument(QJsonObject{{"jsonrpc", "2.0"}, {"id", id2}, {"result", "B"}}).toJson(QJsonDocument::Compact);
        const QByteArray r1 = QJsonDocument(QJsonObject{{"jsonrpc", "2.0"}, {"id", id1}, {"result", "A"}}).toJson(QJsonDocument::Compact);
        d.writeRaw(r2.left(5));
        QTest::qWait(20);
        QVERIFY(second.isEmpty());
        d.writeRaw(r2.mid(5) + "\n" + r1 + "\n");
        QTRY_COMPARE(second, QStringLiteral("B"));
        QTRY_COMPARE(first, QStringLiteral("A"));
    }

    void notificationsAndGarbage()
    {
        FakeDaemon d;
        QVERIFY(d.listen());
        auto c = connectTo(d);
        QTRY_VERIFY(c->isConnected());
        QTRY_VERIFY(d.hasClient());
        QSignalSpy spy(c.get(), &RpcClient::notification);
        d.writeRaw("isto não é json\n\n");
        d.notify("job.progress", QJsonObject{{"id", "job-1"}});
        QTRY_COMPARE(spy.count(), 1);
        QCOMPARE(spy.at(0).at(0).toString(), QStringLiteral("job.progress"));
        QVERIFY(c->isConnected()); // lixo não derruba a conexão
    }

    void pendingFailOnDisconnectAndReconnect()
    {
        FakeDaemon d;
        d.handler = [](const QString &, const QJsonObject &) { return QJsonObject{}; };
        QVERIFY(d.listen());
        auto c = connectTo(d);
        QTRY_VERIFY(c->isConnected());

        int calls = 0;
        RpcError err;
        c->call("slow", {}, [&](const QJsonValue &, const RpcError &e) { ++calls; err = e; });
        QTRY_COMPARE(d.received.size(), 1);
        d.close();
        QTRY_COMPARE(err.code, int(RpcClient::DisconnectedCode));
        QTRY_VERIFY(!c->isConnected());

        // Chamada sem conexão falha de forma assíncrona e só uma vez.
        int offline = 0;
        c->call("x", {}, [&](const QJsonValue &, const RpcError &e) { offline += e.code == RpcClient::DisconnectedCode; });
        QCOMPARE(offline, 0);
        QTRY_COMPARE(offline, 1);

        // O daemon volta: o cliente reconecta sozinho.
        QVERIFY(d.listen());
        QTRY_VERIFY_WITH_TIMEOUT(c->isConnected(), 3000);
        QCOMPARE(calls, 1);
    }

    void findDaemonIgnoresPath()
    {
        // Um "omastored" no PATH (ex.: instalado por um app em ~/.local/bin)
        // nunca deve ser usado.
        QTemporaryDir dir;
        const QString fake = dir.path() + "/omastored";
        QFile f(fake);
        QVERIFY(f.open(QIODevice::WriteOnly));
        f.write("#!/bin/sh\n");
        f.close();
        f.setPermissions(f.permissions() | QFileDevice::ExeOwner);
        qputenv("PATH", dir.path().toUtf8());
        qunsetenv("OMASTORED");
        QVERIFY(RpcClient::findDaemon() != QFileInfo(fake).canonicalFilePath());

        qputenv("OMASTORED", fake.toUtf8());
        QCOMPARE(RpcClient::findDaemon(), QFileInfo(fake).canonicalFilePath());
        qunsetenv("OMASTORED");
    }
};

QTEST_GUILESS_MAIN(TestRpcClient)
#include "tst_rpcclient.moc"
