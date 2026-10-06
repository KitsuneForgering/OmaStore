#include "backend.h"
#include "fakedaemon.h"
#include "rpcclient.h"
#include "theme.h"

#include <QFile>
#include <QJsonArray>
#include <QJsonObject>
#include <QQmlComponent>
#include <QQmlContext>
#include <QQmlEngine>
#include <QQuickImageProvider>
#include <QQuickItem>
#include <QQuickWindow>
#include <QSignalSpy>
#include <QTemporaryDir>
#include <QtTest>

#include <algorithm>
#include <cmath>
#include <functional>
#include <memory>

namespace {
class StubImages : public QQuickImageProvider {
public:
    StubImages() : QQuickImageProvider(Image) {}
    QImage requestImage(const QString &, QSize *size, const QSize &) override
    {
        QImage image(2, 2, QImage::Format_RGB32);
        image.fill(Qt::black);
        if (size)
            *size = image.size();
        return image;
    }
};

double luminance(QColor color)
{
    auto channel = [](double value) {
        value /= 255.0;
        return value <= 0.04045 ? value / 12.92 : std::pow((value + 0.055) / 1.055, 2.4);
    };
    return 0.2126 * channel(color.red()) + 0.7152 * channel(color.green()) +
           0.0722 * channel(color.blue());
}

double contrast(QColor a, QColor b)
{
    const double l1 = luminance(a), l2 = luminance(b);
    return (std::max(l1, l2) + 0.05) / (std::min(l1, l2) + 0.05);
}

QPointF leftTop(QQuickItem *item, QQuickItem *root)
{
    return item->mapToItem(root, QPointF{});
}

void click(QQuickWindow &window, QQuickItem *item)
{
    const QPointF center = item->mapToScene(QPointF(item->width() / 2, item->height() / 2));
    QTest::mouseClick(&window, Qt::LeftButton, {}, center.toPoint());
}

QJsonObject appDetail(const QString &repo, int screenshots, int stars = 4)
{
    QJsonArray shots;
    for (int i = 0; i < screenshots; ++i)
        shots.append(QStringLiteral("https://example.com/%1.png").arg(i));
    return QJsonObject{{"repo", repo}, {"name", "Demo App"}, {"summary", "A sample app"},
                       {"latestVersion", "v1.2.3"}, {"license", "MIT"}, {"readme", "# Demo App\nDetails"},
                       {"changelog", "# Changes\n\n- Improved the interface."},
                       {"category", "Utility"}, {"installable", true}, {"screenshots", shots},
                       {"stars", stars}, {"assets", QJsonArray{}}};
}

// DetailPage.qml in a window, talking to a fake daemon.
struct DetailHarness {
    explicit DetailHarness(std::function<QJsonObject(const QString &, const QJsonObject &)> handler)
        : theme({QStringLiteral("/nonexistent")})
    {
        daemon.handler = std::move(handler);
        listening = daemon.listen();
        rpc = std::make_unique<RpcClient>(daemon.path());
        rpc->setAutoStart(false);
        backend = std::make_unique<Backend>(rpc.get());
        engine.addImageProvider("omastore", new StubImages);
        engine.rootContext()->setContextProperty("backend", backend.get());
        engine.rootContext()->setContextProperty("theme", &theme);
        QQmlComponent component(&engine, QUrl::fromLocalFile(QStringLiteral(OMASTORE_QML_DIR "/DetailPage.qml")));
        error = component.errorString();
        object.reset(component.create());
        page = qobject_cast<QQuickItem *>(object.get());
        if (!page)
            return;
        window.resize(1000, 760);
        page->setParentItem(window.contentItem());
        page->setSize(window.size());
        window.show();
    }
    // Opens repo once the page is up, then connects.
    bool open(const QString &repo)
    {
        if (!page || !listening || !QTest::qWaitForWindowExposed(&window))
            return false;
        backend->openDetail(repo);
        rpc->start();
        return QTest::qWaitFor([&] { return backend->detail().value("repo").toString() == repo; });
    }
    QQuickItem *find(const char *name) const { return page ? page->findChild<QQuickItem *>(name) : nullptr; }

    Theme theme;
    FakeDaemon daemon;
    bool listening = false;
    std::unique_ptr<RpcClient> rpc;
    std::unique_ptr<Backend> backend;
    QQmlEngine engine;
    QString error;
    std::unique_ptr<QObject> object;
    QQuickItem *page = nullptr;
    QQuickWindow window;
};
} // namespace

class TestUi : public QObject {
    Q_OBJECT

private slots:
    void detailAcrossThemesAndWidths_data()
    {
        QTest::addColumn<QByteArray>("colors");
        QTest::addColumn<int>("width");
        QTest::newRow("dark-narrow") << QByteArray(
            "mode = \"dark\"\nbackground = \"#1a1b26\"\nforeground = \"#ffffff\"\n"
            "lighter_background = \"#24283b\"\naccent = \"#7aa2f7\"\n") << 500;
        QTest::newRow("light-wide") << QByteArray(
            "mode = \"light\"\nbackground = \"#ffffff\"\nforeground = \"#202020\"\n"
            "lighter_background = \"#f2f2f2\"\naccent = \"#3366cc\"\n") << 960;
    }

    void detailAcrossThemesAndWidths()
    {
        QFETCH(QByteArray, colors);
        QFETCH(int, width);

        QTemporaryDir themeDir;
        QVERIFY(themeDir.isValid());
        QFile colorsFile(themeDir.filePath("colors.toml"));
        QVERIFY(colorsFile.open(QIODevice::WriteOnly));
        QCOMPARE(colorsFile.write(colors), colors.size());
        colorsFile.close();
        Theme theme({themeDir.path()});

        FakeDaemon daemon;
        daemon.handler = [](const QString &method, const QJsonObject &params) -> QJsonObject {
            if (method == "catalog.get") {
                const QString repo = params.value("repo").toString();
                QJsonArray shots{"https://example.com/one.png", "https://example.com/two.png"};
                if (repo == "demo/other")
                    shots.append("https://example.com/three.png");
                return {{"result", QJsonObject{
                    {"repo", repo}, {"name", "Demo App"}, {"summary", "A sample app"},
                    {"latestVersion", "v1.2.3"}, {"license", "MIT"}, {"readme", "# Demo App\nDetails"},
                    {"changelog", "# Changes\n\n- Improved the interface."},
                    {"category", "Utility"}, {"installable", true}, {"screenshots", shots},
                    {"assets", QJsonArray{}}}}};
            }
            if (method == "catalog.list")
                return {{"result", QJsonArray{QJsonObject{{"repo", "demo/app"}}}}};
            return {{"result", QJsonArray{}}};
        };
        QVERIFY(daemon.listen());
        RpcClient rpc(daemon.path());
        rpc.setAutoStart(false);
        Backend backend(&rpc);

        QQmlEngine engine;
        engine.addImageProvider("omastore", new StubImages);
        engine.rootContext()->setContextProperty("backend", &backend);
        engine.rootContext()->setContextProperty("theme", &theme);
        QQmlComponent component(&engine, QUrl::fromLocalFile(
            QStringLiteral(OMASTORE_QML_DIR "/DetailPage.qml")));
        QVERIFY2(component.isReady(), qPrintable(component.errorString()));
        std::unique_ptr<QObject> object(component.create());
        QVERIFY2(object != nullptr, qPrintable(component.errorString()));
        auto *page = qobject_cast<QQuickItem *>(object.get());
        QVERIFY(page);

        QQuickWindow window;
        window.resize(width, 720);
        page->setParentItem(window.contentItem());
        page->setSize(window.size());
        window.show();
        QVERIFY(QTest::qWaitForWindowExposed(&window));
        backend.openDetail("demo/app");
        rpc.start();
        QTRY_COMPARE(backend.detail().value("repo").toString(), QStringLiteral("demo/app"));

        auto *content = page->findChild<QQuickItem *>("detailContent");
        auto *controls = page->findChild<QQuickItem *>("previewControls");
        auto *carousel = page->findChild<QQuickItem *>("previewCarousel");
        auto *hero = page->findChild<QQuickItem *>("detailHero");
        auto *panel = page->findChild<QQuickItem *>("detailInfoPanel");
        auto *strip = page->findChild<QQuickItem *>("previewStrip");
        auto *readme = page->findChild<QQuickItem *>("detailReadme");
        auto *changelog = page->findChild<QQuickItem *>("detailChangelog");
        auto *previous = page->findChild<QQuickItem *>("previewPrevious");
        auto *next = page->findChild<QQuickItem *>("previewNext");
        auto *position = page->findChild<QQuickItem *>("previewPosition");
        auto *install = page->findChild<QQuickItem *>("installButton");
        auto *name = page->findChild<QQuickItem *>("detailName");
        QVERIFY(content && controls && carousel && hero && panel && strip && readme && changelog &&
                previous && next && position && install && name);
        // The slideshow would move the gallery under the checks below.
        carousel->setProperty("autoplay", false);
        QTRY_COMPARE(position->property("text").toString(), QStringLiteral("1 / 2"));
        // The layout settles over several passes after the screenshots arrive
        // (columns, then widths): the geometry is checked until it holds, and
        // a failure says which condition did not.
        const auto layoutProblem = [&]() -> QString {
            const QPointF contentPos = leftTop(content, page);
            const QPointF controlsPos = leftTop(controls, page);
            const QPointF carouselPos = leftTop(carousel, page);
            const QPointF panelPos = leftTop(panel, page);
            const QPointF stripPos = leftTop(strip, page);
            const QPointF readmePos = leftTop(readme, page);
            const QPointF prevPos = leftTop(previous, page);
            const QPointF nextPos = leftTop(next, page);
            const QList<std::pair<bool, const char *>> checks{
                {carousel->height() > 0 && strip->height() > 0, "carousel and strip laid out"},
                {contentPos.x() >= 24, "content left margin"},
                {contentPos.x() + content->width() <= width - 24, "content right margin"},
                {controlsPos.x() >= contentPos.x(), "controls inside the content"},
                {carouselPos.x() >= contentPos.x(), "carousel left edge inside the content"},
                {carouselPos.x() + carousel->width() <= contentPos.x() + content->width(),
                 "carousel right edge inside the content"},
                {prevPos.x() + previous->width() <= nextPos.x(), "Previous before Next"},
                {nextPos.x() + next->width() <= width - 24, "Next inside the window"},
                {stripPos.y() > carouselPos.y() + carousel->height(), "strip below the carousel"},
                {readmePos.y() > std::max(carouselPos.y() + carousel->height(), panelPos.y() + panel->height()),
                 "README below the carousel and the panel"},
                {width >= 720 ? hero->property("columns").toInt() == 2 : hero->property("columns").toInt() == 1,
                 "hero columns for the width"},
                {width < 720 || (carousel->width() >= hero->width() * 0.55 && carousel->width() <= hero->width() * 0.65),
                 "carousel takes 55-65% of a wide hero"},
                {width < 720 || panelPos.x() >= carouselPos.x() + carousel->width(), "panel beside the carousel"},
                {width >= 720 || panelPos.y() > stripPos.y(), "panel below the strip when narrow"},
            };
            for (const auto &[ok, what] : checks)
                if (!ok)
                    return QString::fromLatin1(what);
            return {};
        };
        QTRY_VERIFY2_WITH_TIMEOUT(layoutProblem().isEmpty(), qPrintable(layoutProblem()), 5000);
        QVERIFY(readme->width() <= 900);
        QVERIFY(changelog->isVisible());
        QVERIFY(leftTop(changelog, page).y() > leftTop(readme, page).y());
        QVERIFY(readme->property("font").value<QFont>().pixelSize() >= 17);
        QVERIFY(readme->property("lineHeight").toReal() >= 1.5);
        QVERIFY(contrast(readme->property("color").value<QColor>(), theme.background()) >= 7);

        auto *buttonText = install->property("contentItem").value<QQuickItem *>();
        auto *buttonBackground = install->property("background").value<QQuickItem *>();
        QVERIFY(buttonText && buttonBackground);
        // QTRY: the button's fill eases between colors (theme.durationShort).
        QTRY_COMPARE(name->property("color").value<QColor>(), theme.foreground());
        QTRY_COMPARE(buttonText->property("color").value<QColor>(), theme.onAccent());
        QTRY_COMPARE(buttonBackground->property("color").value<QColor>(), theme.accentFill());
        // WCAG AAA (7:1) for text.
        QVERIFY(contrast(name->property("color").value<QColor>(), theme.background()) >= 7);
        QVERIFY(contrast(buttonText->property("color").value<QColor>(),
                         buttonBackground->property("color").value<QColor>()) >= 7);

        carousel->setProperty("currentIndex", 1);
        QTRY_COMPARE(position->property("text").toString(), QStringLiteral("2 / 2"));
        QVERIFY(!next->property("enabled").toBool());
        carousel->forceActiveFocus();
        QTest::keyClick(&window, Qt::Key_Left);
        QTRY_COMPARE(carousel->property("currentIndex").toInt(), 0);
        QVERIFY(!previous->property("enabled").toBool());

        // The buttons and thumbnails drive the carousel as a user would.
        auto click = [&window](QQuickItem *item) {
            const QPointF center = item->mapToScene(QPointF(item->width() / 2, item->height() / 2));
            QTest::mouseClick(&window, Qt::LeftButton, {}, center.toPoint());
        };
        click(next);
        QTRY_COMPARE(position->property("text").toString(), QStringLiteral("2 / 2"));
        QQuickItem *firstThumb = nullptr;
        QVERIFY(QMetaObject::invokeMethod(strip, "itemAtIndex", Q_RETURN_ARG(QQuickItem *, firstThumb),
                                          Q_ARG(int, 0)));
        QVERIFY(firstThumb);
        click(firstThumb);
        QTRY_COMPARE(position->property("text").toString(), QStringLiteral("1 / 2"));

        // Opening another app starts its gallery from the first screenshot.
        click(next);
        QTRY_COMPARE(carousel->property("currentIndex").toInt(), 1);
        backend.openDetail("demo/other");
        QTRY_COMPARE(position->property("text").toString(), QStringLiteral("1 / 3"));
        QCOMPARE(carousel->property("currentIndex").toInt(), 0);
    }

    // The category is written the way the user reads it (AudioVideo is the
    // freedesktop name, kept in the manifest and the .desktop).
    void categoryBadgeShowsTheDisplayName_data()
    {
        QTest::addColumn<QString>("category");
        QTest::addColumn<QString>("shown");
        QTest::newRow("audio") << QStringLiteral("AudioVideo") << QStringLiteral("Audio/Video");
        QTest::newRow("other") << QStringLiteral("Utility") << QStringLiteral("Utility");
    }

    void categoryBadgeShowsTheDisplayName()
    {
        QFETCH(QString, category);
        QFETCH(QString, shown);
        DetailHarness h([category](const QString &method, const QJsonObject &) -> QJsonObject {
            if (method == "catalog.get") {
                QJsonObject detail = appDetail(QStringLiteral("demo/app"), 0);
                detail.insert("category", category);
                return {{"result", detail}};
            }
            return {{"result", QJsonArray{}}};
        });
        QVERIFY2(h.page, qPrintable(h.error));
        QVERIFY(h.open("demo/app"));
        auto *badge = h.find("categoryBadge");
        QVERIFY(badge);
        QTRY_COMPARE(badge->property("text").toString(), shown);
    }

    // Starring shows: the button takes the accent fill (not only another
    // glyph), the count moves and a notice confirms it.
    void starredStateIsVisible()
    {
        DetailHarness h([](const QString &method, const QJsonObject &params) -> QJsonObject {
            if (method == "catalog.get")
                return {{"result", appDetail(params.value("repo").toString(), 0)}};
            if (method == "star.get")
                return {{"result", QJsonObject{{"starred", false}}}};
            if (method == "star.set")
                return {{"result", QJsonObject{{"starred", true}, {"stars", 5}}}};
            return {{"result", QJsonArray{}}};
        });
        QVERIFY2(h.page, qPrintable(h.error));
        QVERIFY(h.open("demo/app"));
        auto *star = h.find("starButton");
        QVERIFY(star);
        auto background = [star] { return star->property("background").value<QQuickItem *>()->property("color").value<QColor>(); };
        auto label = [star] { return star->property("contentItem").value<QQuickItem *>(); };

        QTRY_VERIFY(star->property("enabled").toBool());
        QTRY_COMPARE(background(), h.theme.surface());
        QVERIFY(star->property("text").toString().contains("Star"));
        QVERIFY(!star->property("text").toString().contains("Starred"));

        QSignalSpy notices(h.backend.get(), &Backend::notice);
        click(h.window, star);
        QTRY_COMPARE(h.backend->starState(), int(Backend::StarYes));
        QTRY_COMPARE(background(), h.theme.accentFill());
        QTRY_COMPARE(label()->property("color").value<QColor>(), h.theme.onAccent());
        QVERIFY(contrast(label()->property("color").value<QColor>(), background()) >= 7);
        QVERIFY(star->property("text").toString().contains("Starred"));
        QVERIFY(star->property("text").toString().contains("5"));
        QCOMPARE(notices.count(), 1);
        QVERIFY(notices.at(0).at(0).toString().contains("starred"));
    }

    // A star that cannot be checked says why instead of leaving a dead button.
    void starFailureExplainsItself()
    {
        DetailHarness h([](const QString &method, const QJsonObject &params) -> QJsonObject {
            if (method == "catalog.get")
                return {{"result", appDetail(params.value("repo").toString(), 0)}};
            if (method == "star.get")
                return {{"error", QJsonObject{{"code", -32601}, {"message", "method not found"}}}};
            return {{"result", QJsonArray{}}};
        });
        QVERIFY2(h.page, qPrintable(h.error));
        QVERIFY(h.open("demo/app"));
        auto *star = h.find("starButton");
        QVERIFY(star);
        QTRY_VERIFY(!h.backend->starHint().isEmpty());
        QVERIFY(h.backend->starHint().contains("older"));
        QVERIFY(!star->property("enabled").toBool());
    }

    // A file with no checksum: the install asks first, and only the
    // confirmed request carries allowUnverified (the daemon refuses others).
    void unverifiedInstallAsksFirst()
    {
        DetailHarness h([](const QString &method, const QJsonObject &params) -> QJsonObject {
            if (method == "catalog.get") {
                QJsonObject d = appDetail(params.value("repo").toString(), 0);
                d.insert("selectedAsset", QJsonObject{{"name", "demo-1-x86_64-linux.tar.gz"}, {"checksum", ""}});
                return {{"result", d}};
            }
            if (method == "install.start")
                return {{"result", QJsonObject{{"id", "j1"}, {"state", "running"}}}};
            return {{"result", QJsonArray{}}};
        });
        QVERIFY2(h.page, qPrintable(h.error));
        QVERIFY(h.open("demo/app"));
        auto *install = h.find("installButton");
        auto *hint = h.find("uncheckedHint");
        QVERIFY(install && hint);
        QTRY_VERIFY(hint->isVisible());
        QTRY_VERIFY(install->property("enabled").toBool());
        click(h.window, install);
        auto *dialog = h.page->findChild<QObject *>("confirmUnverified");
        QVERIFY(dialog);
        QTRY_VERIFY(dialog->property("visible").toBool());
        QTest::qWait(50);
        QCOMPARE(h.daemon.count("install.start"), 0);

        QMetaObject::invokeMethod(dialog, "accept");
        QTRY_COMPARE(h.daemon.count("install.start"), 1);
        QJsonObject params;
        for (const QJsonObject &r : h.daemon.received)
            if (r.value("method").toString() == "install.start")
                params = r.value("params").toObject();
        QCOMPARE(params.value("allowUnverified").toBool(), true);
    }

    // The file's build provenance shows next to Install; when Settings
    // require it and the file has none, Install is off and the hint says why.
    void provenanceShowsAndCanBlock_data()
    {
        QTest::addColumn<bool>("attested");
        QTest::addColumn<bool>("required");
        QTest::newRow("attested") << true << false;
        QTest::newRow("none") << false << false;
        QTest::newRow("none, required") << false << true;
    }
    void provenanceShowsAndCanBlock()
    {
        QFETCH(bool, attested);
        QFETCH(bool, required);
        DetailHarness h([=](const QString &method, const QJsonObject &params) -> QJsonObject {
            if (method == "catalog.get") {
                QJsonObject d = appDetail(params.value("repo").toString(), 0);
                QJsonObject asset{{"name", "demo-1-x86_64-linux.tar.gz"}, {"checksum", "digest"}};
                if (attested)
                    asset.insert("provenance", QJsonObject{{"workflow", ".github/workflows/release.yml"},
                                                           {"ref", "refs/tags/v1.2.3"}, {"commit", "abc"}});
                d.insert("selectedAsset", asset);
                return {{"result", d}};
            }
            if (method == "settings.get")
                return {{"result", QJsonObject{{"autoUpdate", true}, {"requireProvenance", required}}}};
            return {{"result", QJsonArray{}}};
        });
        QVERIFY2(h.page, qPrintable(h.error));
        QVERIFY(h.open("demo/app"));
        QTRY_VERIFY(h.backend->settingsAvailable());
        auto *hint = h.find("provenanceHint");
        auto *install = h.find("installButton");
        QVERIFY(hint && install);
        QTRY_VERIFY(hint->isVisible());
        const QString text = hint->property("text").toString();
        if (attested)
            QVERIFY2(text.contains("release.yml") && text.contains("v1.2.3") && !text.contains("refs/"), qPrintable(text));
        else
            QVERIFY2(text.contains("No build provenance"), qPrintable(text));
        QTRY_COMPARE(install->property("enabled").toBool(), !required);
    }

    // i installs and u updates; a key that cannot act says why.
    void keyboardInstallAndUpdate()
    {
        DetailHarness h([](const QString &method, const QJsonObject &params) -> QJsonObject {
            if (method == "catalog.get")
                return {{"result", appDetail(params.value("repo").toString(), 0)}};
            if (method == "install.start")
                return {{"result", QJsonObject{{"id", "j1"}, {"state", "running"}}}};
            return {{"result", QJsonArray{}}};
        });
        QVERIFY2(h.page, qPrintable(h.error));
        QVERIFY(h.open("demo/app"));
        QTRY_VERIFY(h.backend->connected());
        h.page->forceActiveFocus();

        QSignalSpy notices(h.backend.get(), &Backend::notice);
        QTest::keyClick(&h.window, Qt::Key_U);
        QTRY_COMPARE(notices.count(), 1);
        QVERIFY(notices.at(0).at(0).toString().contains("not installed"));
        QCOMPARE(h.daemon.count("install.start"), 0);

        QTest::keyClick(&h.window, Qt::Key_I);
        QTRY_COMPARE(h.daemon.count("install.start"), 1);
    }

    // An installed app with a previous version, files gone and a missing
    // library: Go back, Repair, the library and the in-use refusal all show.
    void installedAppStates()
    {
        DetailHarness h([](const QString &method, const QJsonObject &params) -> QJsonObject {
            if (method == "catalog.get") {
                QJsonObject d = appDetail(params.value("repo").toString(), 0);
                d.insert("install", QJsonObject{
                    {"repo", "demo/app"}, {"version", "v2"}, {"previousVersion", "v1"}, {"broken", true},
                    {"history", QJsonArray{QJsonObject{{"action", "update"}, {"fromVersion", "v1"},
                                                       {"toVersion", "v2"}, {"at", "2026-10-05T12:00:00Z"}}}}
                });
                return {{"result", d}};
            }
            if (method == "deps.check")
                return {{"result", QJsonObject{{"deps", QJsonArray{}}, {"toInstall", QJsonArray{"extra/webkit2gtk-4.1"}},
                                               {"libraries", QJsonArray{QJsonObject{{"name", "libwebkit2gtk-4.1.so.0"},
                                                   {"status", "available"}, {"package", "extra/webkit2gtk-4.1"}}}}}}};
            if (method == "install.uninstall")
                return {{"error", QJsonObject{{"code", -32015}, {"message", "demo/app: the app is running: demo (4242)"}}}};
            if (method == "install.rollback")
                return {{"result", QJsonObject{{"repo", "demo/app"}, {"version", "v1"}}}};
            return {{"result", QJsonArray{}}};
        });
        QVERIFY2(h.page, qPrintable(h.error));
        QVERIFY(h.open("demo/app"));
        auto *rollback = h.find("rollbackButton");
        auto *repair = h.find("repairButton");
        auto *open = h.find("openButton");
        auto *deps = h.find("depsBox");
        auto *history = h.find("installHistory");
        QVERIFY(rollback && repair && open && deps && history);
        QTRY_VERIFY(history->isVisible());
        auto *historyRepeater = h.find("installHistoryRepeater");
        QVERIFY(historyRepeater);
        QCOMPARE(historyRepeater->property("count").toInt(), 1);
        QTRY_VERIFY(rollback->isVisible());
        QVERIFY(rollback->property("text").toString().contains("v1"));
        QVERIFY(repair->isVisible());
        QVERIFY(!open->isVisible());
        QTRY_VERIFY(deps->isVisible());

        QSignalSpy notices(h.backend.get(), &Backend::notice);
        click(h.window, rollback);
        QTRY_COMPARE(h.daemon.count("install.rollback"), 1);
        QTRY_COMPARE(notices.count(), 1);
        QVERIFY(notices.at(0).at(0).toString().contains("v1"));

        h.backend->uninstall("demo/app");
        auto *inUse = h.page->findChild<QObject *>("removeInUse");
        QVERIFY(inUse);
        QTRY_VERIFY(inUse->property("visible").toBool());
        QVERIFY(inUse->property("processes").toString().contains("4242"));
    }

    // The gallery is a slideshow: it advances on its own and wraps around,
    // and waits while paused or while the pointer is on it.
    void carouselPlaysAndPauses()
    {
        DetailHarness h([](const QString &method, const QJsonObject &params) -> QJsonObject {
            if (method == "catalog.get")
                return {{"result", appDetail(params.value("repo").toString(), 3)}};
            return {{"result", QJsonArray{}}};
        });
        QVERIFY2(h.page, qPrintable(h.error));
        auto *carousel = h.find("previewCarousel");
        auto *playPause = h.find("previewPlayPause");
        QVERIFY(carousel && playPause);
        carousel->setProperty("autoplayInterval", 40);
        QTest::mouseMove(&h.window, QPoint(2, 2)); // the pointer away from the gallery
        QVERIFY(h.open("demo/app"));
        QTRY_COMPARE(carousel->property("count").toInt(), 3);
        QTRY_COMPARE(carousel->property("currentIndex").toInt(), 2);
        QTRY_COMPARE(carousel->property("currentIndex").toInt(), 0); // wrapped

        click(h.window, playPause);
        QTRY_VERIFY(carousel->property("userPaused").toBool());
        QTest::mouseMove(&h.window, QPoint(2, 2));
        const int pausedAt = carousel->property("currentIndex").toInt();
        QTest::qWait(300);
        QCOMPARE(carousel->property("currentIndex").toInt(), pausedAt);

        // Playing again, but the pointer rests on the gallery: still waits.
        click(h.window, playPause);
        QTRY_VERIFY(!carousel->property("userPaused").toBool());
        QVERIFY(!carousel->property("playing").toBool());
        QTest::mouseMove(&h.window, QPoint(2, 2));
        QTRY_VERIFY(carousel->property("playing").toBool());
        QTRY_VERIFY(carousel->property("currentIndex").toInt() != pausedAt);
    }

    // With reduced motion the slideshow starts paused (it can still be played).
    void carouselStartsPausedWithReducedMotion()
    {
        qputenv("OMASTORE_REDUCE_MOTION", "1");
        DetailHarness h([](const QString &method, const QJsonObject &params) -> QJsonObject {
            if (method == "catalog.get")
                return {{"result", appDetail(params.value("repo").toString(), 3)}};
            return {{"result", QJsonArray{}}};
        });
        qunsetenv("OMASTORE_REDUCE_MOTION");
        QVERIFY2(h.page, qPrintable(h.error));
        auto *carousel = h.find("previewCarousel");
        QVERIFY(carousel);
        carousel->setProperty("autoplayInterval", 40);
        QVERIFY(h.open("demo/app"));
        QTRY_COMPARE(carousel->property("count").toInt(), 3);
        QVERIFY(carousel->property("userPaused").toBool());
        QTest::qWait(200);
        QCOMPARE(carousel->property("currentIndex").toInt(), 0);
    }

    // A fresh store (empty catalog) invites authors in; the publish page
    // checks a repository and shows the report with a manifest to copy.
    void emptyCatalogLeadsAuthorsToPublish()
    {
        Theme theme({QStringLiteral("/nonexistent")});
        FakeDaemon daemon;
        daemon.handler = [](const QString &method, const QJsonObject &params) -> QJsonObject {
            if (method == "author.check") {
                return {{"result", QJsonObject{
                    {"repo", params.value("repo")}, {"compatible", false}, {"name", "Demo"},
                    {"summary", "A demo"}, {"category", "Utility"}, {"iconUrl", ""}, {"tag", "v1.0.0"},
                    {"screenshots", QJsonArray{}},
                    {"checks", QJsonArray{
                        QJsonObject{{"status", "fail"}, {"item", "omastore.toml"}, {"detail", "missing"}, {"fix", "add it"}},
                        QJsonObject{{"status", "ok"}, {"item", "Release"}, {"detail", "v1.0.0"}, {"fix", ""}}}},
                    {"suggestedManifest", "kind = \"app\"\n"}}}};
            }
            if (method == "index.start")
                return {{"result", QJsonObject{{"id", "j1"}, {"state", "running"}}}};
            return {{"result", QJsonArray{}}};
        };
        QVERIFY(daemon.listen());
        RpcClient rpc(daemon.path());
        rpc.setAutoStart(false);
        Backend backend(&rpc);

        QQmlEngine engine;
        engine.addImageProvider("omastore", new StubImages);
        engine.rootContext()->setContextProperty("backend", &backend);
        engine.rootContext()->setContextProperty("theme", &theme);
        engine.rootContext()->setContextProperty("startupRepo", QString());
        engine.rootContext()->setContextProperty("startupCheck", QString());
        engine.rootContext()->setContextProperty("startupPage", QString());
        QQmlComponent component(&engine, QUrl::fromLocalFile(QStringLiteral(OMASTORE_QML_DIR "/Main.qml")));
        QVERIFY2(component.isReady(), qPrintable(component.errorString()));
        std::unique_ptr<QObject> object(component.create());
        QVERIFY2(object != nullptr, qPrintable(component.errorString()));
        auto *window = qobject_cast<QQuickWindow *>(object.get());
        QVERIFY(window);
        QVERIFY(QTest::qWaitForWindowExposed(window));
        rpc.start();
        QTRY_VERIFY(backend.connected());

        auto *invite = window->findChild<QQuickItem *>("authorInvite");
        auto *title = window->findChild<QQuickItem *>("emptyTitle");
        QVERIFY(invite && title);
        // The first run starts an index; the invite shows once no job is running.
        QTRY_COMPARE(daemon.count("index.start"), 1);
        daemon.notify("job.done", QJsonObject{{"id", "j1"}, {"kind", "index"}, {"state", "done"}});
        QTRY_VERIFY(invite->isVisible());
        QCOMPARE(title->property("text").toString(), QStringLiteral("The catalog is just getting started"));

        QVERIFY(QMetaObject::invokeMethod(object.get(), "openPublish", Q_ARG(QVariant, QVariant(QStringLiteral("https://github.com/demo/app")))));
        auto *content = window->findChild<QQuickItem *>("publishContent");
        auto *report = window->findChild<QQuickItem *>("checkReport");
        auto *verdict = window->findChild<QQuickItem *>("checkVerdict");
        auto *manifest = window->findChild<QQuickItem *>("suggestedManifest");
        QVERIFY(content && report && verdict && manifest);
        QTRY_VERIFY(content->isVisible());
        QVERIFY(!invite->isVisible());
        QTRY_VERIFY(report->isVisible());
        QCOMPARE(daemon.received.last().value("params").toObject().value("repo").toString(), QStringLiteral("demo/app"));
        QCOMPARE(verdict->property("text").toString(), QStringLiteral("demo/app is not in the store yet"));
        QVERIFY(manifest->isVisible());
        QCOMPARE(manifest->property("text").toString(), QStringLiteral("kind = \"app\"\n"));
        QVERIFY(content->width() <= 880);
        QVERIFY(content->mapToItem(window->contentItem(), QPointF{}).x() + content->width() <= window->width());
    }
};

QTEST_MAIN(TestUi)
#include "tst_ui.moc"
