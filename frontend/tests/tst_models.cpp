#include "backend.h"
#include "catalogmodel.h"
#include "fakedaemon.h"
#include "jobsmodel.h"
#include "rpcclient.h"

#include <QJsonArray>
#include <QSignalSpy>
#include <QUrl>
#include <QUrlQuery>
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
        if (m == "author.check")
            return {{"result", QJsonObject{{"repo", p.value("repo")}, {"compatible", p.contains("manifest")},
                                           {"checks", QJsonArray{}}, {"suggestedManifest", ""}}}};
        if (m == "star.get") {
            if (p.value("repo").toString() == "a/anon")
                return {{"error", QJsonObject{{"code", -32011}, {"message", "token required"}}}};
            return {{"result", QJsonObject{{"starred", false}}}};
        }
        if (m == "star.set")
            return {{"result", QJsonObject{{"starred", p.value("starred")}, {"stars", 4}}}};
        if (m == "deps.check")
            return {{"result", QJsonObject{{"repo", p.value("repo")}, {"source", "PKGBUILD"}, {"pacman", true},
                                           {"deps", QJsonArray{QJsonObject{{"name", "ffmpeg"}, {"status", "available"},
                                                                           {"package", "extra/ffmpeg"}}}},
                                           {"missing", 1}, {"toInstall", QJsonArray{"extra/ffmpeg"}}}}};
        if (m == "index.start" || m == "install.start" || m == "deps.install")
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

    void normalizeRepo()
    {
        QCOMPARE(Backend::normalizeRepo(" pch/rawmakase "), QStringLiteral("pch/rawmakase"));
        QCOMPARE(Backend::normalizeRepo("https://github.com/pch/rawmakase"), QStringLiteral("pch/rawmakase"));
        QCOMPARE(Backend::normalizeRepo("github.com/pch/rawmakase.git"), QStringLiteral("pch/rawmakase"));
        QCOMPARE(Backend::normalizeRepo("https://github.com/pch/rawmakase/tree/main/src"), QStringLiteral("pch/rawmakase"));
        QCOMPARE(Backend::normalizeRepo("rawmakase"), QString());
        QCOMPARE(Backend::normalizeRepo("pch/.."), QString());
        QCOMPARE(Backend::normalizeRepo("bad owner/x"), QString());
    }

    void authorCheckAndIndexError()
    {
        FakeDaemon d;
        QJsonArray all{app("a/photo", "Graphics")};
        serveCatalog(d, &all);
        QVERIFY(d.listen());
        RpcClient rpc(d.path());
        rpc.setAutoStart(false);
        Backend b(&rpc);
        // Asked before connecting (omastore-gui --check): sent on connect.
        b.checkRepo("acme/early");
        QVERIFY(b.authorCheckBusy());
        rpc.start();
        QTRY_VERIFY(b.connected());
        QTRY_COMPARE(b.authorCheck().value("repo").toString(), QStringLiteral("acme/early"));

        b.checkRepo("nope");
        QVERIFY(!b.authorCheckError().isEmpty());
        QCOMPARE(d.count("author.check"), 1); // only acme/early

        b.checkRepo("https://github.com/acme/photo");
        QVERIFY(b.authorCheckBusy());
        QTRY_VERIFY(!b.authorCheckBusy());
        QCOMPARE(b.authorCheck().value("repo").toString(), QStringLiteral("acme/photo"));
        QVERIFY(!b.authorCheck().value("compatible").toBool()); // no manifest sent
        QVERIFY(b.authorCheckError().isEmpty());

        b.checkRepo("acme/photo", "kind = \"app\"", true);
        QTRY_VERIFY(b.authorCheck().value("compatible").toBool());
        // An empty local manifest is sent as such, not as "use the published one".
        b.checkRepo("acme/photo", "  ", true);
        QTRY_VERIFY(!b.authorCheckBusy());
        QCOMPARE(d.received.last().value("params").toObject().value("manifest"), QJsonValue("  "));
        b.checkRepo("acme/photo", "ignored", false);
        QTRY_VERIFY(!b.authorCheckBusy());
        QVERIFY(!d.received.last().value("params").toObject().contains("manifest"));
        b.clearAuthorCheck();
        QVERIFY(b.authorCheck().isEmpty());

        // A failed index is remembered (friendly text) until one succeeds.
        const QJsonObject failed{{"id", "j9"}, {"kind", "index"}, {"state", "failed"},
                                 {"error", QJsonObject{{"code", -32005}, {"message", "limit"}}}};
        d.notify("job.failed", failed);
        QTRY_VERIFY(b.indexError().contains("gh auth login"));
        d.notify("job.done", QJsonObject{{"id", "j10"}, {"kind", "index"}, {"state", "done"}});
        QTRY_VERIFY(b.indexError().isEmpty());
    }

    void starAndDeps()
    {
        FakeDaemon d;
        QJsonArray all{app("a/photo", "Graphics"), app("a/anon", "Graphics")};
        serveCatalog(d, &all);
        QVERIFY(d.listen());
        RpcClient rpc(d.path());
        rpc.setAutoStart(false);
        Backend b(&rpc);
        rpc.start();
        QTRY_VERIFY(b.connected());

        b.openDetail("a/photo");
        QTRY_COMPARE(b.starState(), int(Backend::StarNo));
        QTRY_COMPARE(b.deps().value("toInstall").toStringList(), QStringList{"extra/ffmpeg"});
        b.toggleStar();
        QVERIFY(b.starBusy());
        QTRY_COMPARE(b.starState(), int(Backend::StarYes));
        QVERIFY(!b.starBusy());
        QCOMPARE(b.detail().value("stars").toInt(), 4);
        QCOMPARE(d.received.last().value("params").toObject().value("starred").toBool(), true);

        // Without a token the star stays unknown and explains why.
        b.openDetail("a/anon");
        QCOMPARE(b.starState(), int(Backend::StarUnknown));
        QTRY_VERIFY(b.starHint().contains("gh auth login"));
        const int sets = d.count("star.set");
        b.toggleStar();
        QCOMPARE(d.count("star.set"), sets);

        // A finished install with missing dependencies offers to install them.
        QSignalSpy suggested(&b, &Backend::depsSuggested);
        d.notify("job.done", QJsonObject{{"id", "j1"}, {"kind", "install"}, {"repo", "a/photo"}, {"state", "done"}});
        QTRY_COMPARE(suggested.count(), 1);
        QCOMPARE(suggested.first().at(0).toString(), QStringLiteral("a/photo"));
        QCOMPARE(suggested.first().at(1).toStringList(), QStringList{"extra/ffmpeg"});
        b.installDeps("a/photo");
        QTRY_COMPARE(d.count("deps.install"), 1);

        // A failed install of the open app is kept until one succeeds.
        QVERIFY(b.detailFailure().isEmpty());
        d.notify("job.failed", QJsonObject{{"id", "j2"}, {"kind", "install"}, {"repo", "A/Anon"}, {"state", "failed"},
                                           {"error", QJsonObject{{"code", -32603}, {"message", "checksum mismatch"}}}});
        QTRY_COMPARE(b.detailFailure(), QStringLiteral("checksum mismatch"));
        QVERIFY(QUrlQuery(QUrl(b.issueUrl())).queryItemValue("body", QUrl::FullyDecoded).contains("checksum mismatch"));
        b.openDetail("a/photo");
        QVERIFY(b.detailFailure().isEmpty());
        b.openDetail("a/anon");
        QCOMPARE(b.detailFailure(), QStringLiteral("checksum mismatch"));
        d.notify("job.done", QJsonObject{{"id", "j3"}, {"kind", "install"}, {"repo", "a/anon"}, {"state", "done"}});
        QTRY_VERIFY(b.detailFailure().isEmpty());
    }

    void issueUrl()
    {
        const QVariantMap detail{
            {"repo", "acme/app"}, {"latestVersion", "v2"},
            {"install", QVariantMap{{"version", "v1"}, {"execPath", "/home/u/.local/bin/app"}}},
            {"assets", QVariantList{QVariantMap{{"name", "app-x86_64.tar.gz"}, {"arch", "amd64"}},
                                    QVariantMap{{"name", "app-aarch64.tar.gz"}, {"arch", "arm64"}},
                                    QVariantMap{{"name", "app.AppImage"}, {"arch", ""}}}}};
        const QUrl url(Backend::buildIssueUrl(detail, "open /home/u/.cache/x: `denied`\nnext", "x86_64", "0.1.0", "/home/u"));
        QCOMPARE(url.scheme() + "://" + url.host() + url.path(), QStringLiteral("https://github.com/acme/app/issues/new"));
        const QUrlQuery q(url);
        QCOMPARE(q.queryItemValue("title", QUrl::FullyDecoded), QStringLiteral("Installing through OmaStore fails"));
        const QString body = q.queryItemValue("body", QUrl::FullyDecoded);
        for (const char *want : {"App version: v1 (latest release: v2)", "Architecture: x86_64",
                                 "Release files for it: app-x86_64.tar.gz, app.AppImage", "OmaStore: 0.1.0",
                                 "Error: `open ~/.cache/x: 'denied' next`"})
            QVERIFY2(body.contains(QLatin1String(want)), qPrintable(body));
        // Only allow-listed fields: never the local paths of the installation.
        QVERIFY(!body.contains("/home/u"));
        QVERIFY(!body.contains("aarch64"));

        const QUrlQuery plain(QUrl(Backend::buildIssueUrl(QVariantMap{{"repo", "acme/app"}}, {}, "arm64", {}, "/home/u")));
        QVERIFY(!plain.hasQueryItem("title"));
        QVERIFY(plain.queryItemValue("body", QUrl::FullyDecoded).contains("not installed"));
        QVERIFY(Backend::buildIssueUrl(QVariantMap{{"repo", "../x"}}, {}, "x86_64", {}, {}).isEmpty());
    }

    void friendlyErrors()
    {
        QVERIFY(Backend::friendlyError(-32012, "x").contains("password"));
        QVERIFY(Backend::friendlyError(-32010, "x").contains("retry"));
        QVERIFY(Backend::friendlyError(-32005, "x").contains("gh auth login"));
        QVERIFY(Backend::friendlyError(-32008, "x").contains("checksum"));
        QCOMPARE(Backend::friendlyError(-32603, "failed"), QStringLiteral("failed"));
    }
};

QTEST_GUILESS_MAIN(TestModels)
#include "tst_models.moc"
