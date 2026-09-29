#include "backend.h"
#include "catalogmodel.h"
#include "fakedaemon.h"
#include "jobsmodel.h"
#include "rpcclient.h"

#include <QJsonArray>
#include <QSignalSpy>
#include <QtTest>

namespace {
QJsonObject app(const QString &repo, const QString &category, bool update = false, const QString &installed = {})
{
    return {{"repo", repo}, {"name", repo.section('/', 1)}, {"summary", "s"}, {"iconUrl", ""},
            {"category", category}, {"stars", 3}, {"installable", true}, {"latestVersion", "v2"},
            {"installedVersion", installed}, {"updateAvailable", update}, {"screenshots", QJsonArray{"https://x/a.png"}}};
}

// Fake daemon with a filterable catalog.
void serveCatalog(FakeDaemon &d, QJsonArray *all)
{
    d.handler = [all](const QString &m, const QJsonObject &p) -> QJsonObject {
        if (m == "catalog.list") {
            QJsonArray out;
            for (const QJsonValue &v : *all) {
                const QJsonObject o = v.toObject();
                if (p.contains("category") && o.value("category") != p.value("category"))
                    continue;
                if (p.value("installed").toBool() && o.value("installedVersion").toString().isEmpty())
                    continue;
                if (p.contains("query") && !o.value("repo").toString().contains(p.value("query").toString()))
                    continue;
                out.append(o);
            }
            if (p.value("limit").toInt() > 0)
                while (out.size() > p.value("limit").toInt())
                    out.removeLast();
            return {{"result", out}};
        }
        if (m == "catalog.categories")
            return {{"result", QJsonArray{QJsonObject{{"name", "Graphics"}, {"count", 1}}}}};
        if (m == "catalog.similar")
            return {{"result", QJsonArray{app("a/draw", "Graphics")}}};
        if (m == "catalog.get")
            return {{"result", app(p.value("repo").toString(), "Graphics")}};
        if (m == "jobs.list")
            return {{"result", QJsonArray{}}};
        if (m == "index.start" || m == "install.start")
            return {{"result", QJsonObject{{"id", "job-1"}, {"state", "running"}}}};
        return {{"error", QJsonObject{{"code", -32601}, {"message", "?"}}}};
    };
}
} // namespace

class TestModels : public QObject {
    Q_OBJECT

private slots:
    void catalogLoadsAndFilters()
    {
        FakeDaemon d;
        QJsonArray all{app("a/photo", "Graphics", true, "v1"), app("a/vm", "System")};
        serveCatalog(d, &all);
        QVERIFY(d.listen());
        RpcClient rpc(d.path());
        rpc.setAutoStart(false);
        CatalogModel m(&rpc);
        m.setDebounce(0);
        rpc.start();

        QTRY_COMPARE(m.rowCount(), 2);
        QCOMPARE(m.data(m.index(0), CatalogModel::RepoRole).toString(), QStringLiteral("a/photo"));
        QCOMPARE(m.data(m.index(0), CatalogModel::UpdateAvailableRole).toBool(), true);
        QCOMPARE(m.data(m.index(0), CatalogModel::ScreenshotsRole).toList().size(), 1);
        QCOMPARE(m.roleNames().value(CatalogModel::IconUrlRole), QByteArray("iconUrl"));
        QCOMPARE(m.get(1).value("repo").toString(), QStringLiteral("a/vm"));
        QCOMPARE(m.indexOf("a/vm"), 1);
        QCOMPARE(m.indexOf("x/y"), -1);
        QVERIFY(m.get(99).isEmpty());

        m.setCategory("System");
        QTRY_COMPARE(m.rowCount(), 1);
        QCOMPARE(m.data(m.index(0), CatalogModel::RepoRole).toString(), QStringLiteral("a/vm"));

        m.setCategory({});
        m.setQuery("  photo ");
        QTRY_COMPARE(d.received.last().value("params").toObject().value("query").toString(), QStringLiteral("photo"));
        QTRY_COMPARE(m.data(m.index(0), CatalogModel::RepoRole).toString(), QStringLiteral("a/photo"));
        QCOMPARE(m.rowCount(), 1);

        // catalog.changed reloads.
        m.setQuery({});
        QTRY_COMPARE(m.rowCount(), 2);
        all.append(app("a/new", "Office"));
        QTRY_VERIFY(d.hasClient());
        d.notify("catalog.changed", QJsonObject{});
        QTRY_COMPARE(m.rowCount(), 3);
    }

    void staleResponsesAreIgnored()
    {
        FakeDaemon d;
        QVERIFY(d.listen());
        // The first request never answers in time; the second one does.
        int n = 0;
        d.handler = [&n](const QString &m, const QJsonObject &p) -> QJsonObject {
            if (m != "catalog.list")
                return {};
            ++n;
            if (p.value("category").toString() == "Slow")
                return {};
            return {{"result", QJsonArray{app("a/fast", "Fast")}}};
        };
        RpcClient rpc(d.path());
        rpc.setAutoStart(false);
        CatalogModel m(&rpc);
        m.setDebounce(0);
        rpc.start();
        QTRY_VERIFY(rpc.isConnected());
        m.setCategory("Slow");
        QTRY_VERIFY(m.loading());
        m.setCategory("Fast");
        QTRY_COMPARE(m.rowCount(), 1);
        QVERIFY(!m.loading());
    }

    void errorIsExposed()
    {
        FakeDaemon d;
        d.handler = [](const QString &, const QJsonObject &) {
            return QJsonObject{{"error", QJsonObject{{"code", -32603}, {"message", "banco travado"}}}};
        };
        QVERIFY(d.listen());
        RpcClient rpc(d.path());
        rpc.setAutoStart(false);
        CatalogModel m(&rpc);
        rpc.start();
        QTRY_COMPARE(m.error(), QStringLiteral("banco travado"));
    }

    void jobsModelTracksNotifications()
    {
        FakeDaemon d;
        QVERIFY(d.listen());
        RpcClient rpc(d.path());
        rpc.setAutoStart(false);
        JobsModel jobs(&rpc);
        QSignalSpy finished(&jobs, &JobsModel::finished);
        rpc.start();
        QTRY_VERIFY(d.hasClient());

        d.notify("job.started", QJsonObject{{"id", "job-1"}, {"kind", "install"}, {"repo", "a/vm"}, {"state", "running"}});
        QTRY_COMPARE(jobs.rowCount(), 1);
        QCOMPARE(jobs.runningCount(), 1);
        QCOMPARE(jobs.forRepo("A/VM").value("id").toString(), QStringLiteral("job-1"));
        QCOMPARE(jobs.forRepo("A/VM").value("progress").toDouble(), -1.0);

        d.notify("job.progress", QJsonObject{{"id", "job-1"}, {"kind", "install"}, {"repo", "a/vm"},
                                             {"state", "running"}, {"stage", "download"}, {"done", 25}, {"total", 100}});
        QTRY_COMPARE(jobs.data(jobs.index(0), JobsModel::ProgressRole).toDouble(), 0.25);

        d.notify("job.done", QJsonObject{{"id", "job-1"}, {"kind", "install"}, {"repo", "a/vm"}, {"state", "done"}});
        QTRY_COMPARE(finished.count(), 1);
        QCOMPARE(jobs.runningCount(), 0);
        QVERIFY(jobs.forRepo("a/vm").isEmpty());

        // A late progress update does not revive a finished job.
        d.notify("job.progress", QJsonObject{{"id", "job-1"}, {"state", "running"}});
        QTest::qWait(50);
        QCOMPARE(jobs.runningCount(), 0);
        QCOMPARE(finished.count(), 1);

        d.notify("job.started", QJsonObject{{"id", "job-2"}, {"kind", "index"}, {"state", "running"}, {"total", 10}, {"done", 3}});
        QTRY_COMPARE(jobs.indexJob().value("id").toString(), QStringLiteral("job-2"));
        jobs.clearFinished();
        QCOMPARE(jobs.rowCount(), 1);
    }

    void backendFirstRunIndexesAndDetail()
    {
        FakeDaemon d;
        QJsonArray empty;
        serveCatalog(d, &empty);
        QVERIFY(d.listen());
        RpcClient rpc(d.path());
        rpc.setAutoStart(false);
        Backend b(&rpc);
        rpc.start();

        // An empty catalog on the first connection triggers index.start.
        QTRY_COMPARE(d.count("index.start"), 1);
        QTRY_COMPARE(b.categories().size(), 1);

        b.openDetail("a/photo");
        QTRY_COMPARE(b.detail().value("repo").toString(), QStringLiteral("a/photo"));
        QTRY_COMPARE(b.similar().size(), 1);
        QCOMPARE(b.similar().first().toMap().value("repo").toString(), QStringLiteral("a/draw"));
        QVERIFY(!b.detailLoading());
        // catalog.changed for the same repo reloads the detail.
        const int gets = d.count("catalog.get");
        d.notify("catalog.changed", QJsonObject{{"repo", "a/photo"}});
        QTRY_COMPARE(d.count("catalog.get"), gets + 1);
        b.closeDetail();
        QVERIFY(b.detail().isEmpty());
        QVERIFY(b.similar().isEmpty());

        QSignalSpy errors(&b, &Backend::errorOccurred);
        b.install("a/photo");
        QTRY_COMPARE(d.count("install.start"), 1);
        b.uninstall("a/photo"); // the fake answers error -32601
        QTRY_COMPARE(errors.count(), 1);
    }

    // A detail opened before connecting (e.g. omastore-gui --open) loads the
    // detail and the similar apps as soon as it connects.
    void detailOpenedBeforeConnect()
    {
        FakeDaemon d;
        QJsonArray all{app("a/photo", "Graphics")};
        serveCatalog(d, &all);
        QVERIFY(d.listen());
        RpcClient rpc(d.path());
        rpc.setAutoStart(false);
        Backend b(&rpc);
        b.openDetail("a/photo"); // still not connected
        QTest::qWait(20);
        QVERIFY(b.similar().isEmpty());
        rpc.start();
        QTRY_COMPARE(b.detail().value("repo").toString(), QStringLiteral("a/photo"));
        QTRY_COMPARE(b.similar().size(), 1);
    }

    void friendlyErrors()
    {
        QVERIFY(Backend::friendlyError(-32005, "x").contains("gh auth login"));
        QVERIFY(Backend::friendlyError(-32008, "x").contains("checksum"));
        QCOMPARE(Backend::friendlyError(-32603, "falhou"), QStringLiteral("falhou"));
    }
};

QTEST_GUILESS_MAIN(TestModels)
#include "tst_models.moc"
