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
#include <QTemporaryDir>
#include <QtTest>

#include <algorithm>
#include <cmath>
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
        auto *previous = page->findChild<QQuickItem *>("previewPrevious");
        auto *next = page->findChild<QQuickItem *>("previewNext");
        auto *position = page->findChild<QQuickItem *>("previewPosition");
        auto *install = page->findChild<QQuickItem *>("installButton");
        auto *name = page->findChild<QQuickItem *>("detailName");
        QVERIFY(content && controls && carousel && hero && panel && strip && readme &&
                previous && next && position && install && name);
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
