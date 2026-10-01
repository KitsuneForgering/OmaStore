#include "backend.h"
#include "imageprovider.h"
#include "offlinenam.h"
#include "rpcclient.h"
#include "theme.h"

#include <QCommandLineParser>
#include <QGuiApplication>
#include <QPalette>
#include <QProcess>
#include <QQuickWindow>
#include <QTimer>
#include <QQmlApplicationEngine>
#include <QQmlContext>
#include <QQuickStyle>

int main(int argc, char *argv[])
{
    QGuiApplication app(argc, argv);
    app.setApplicationName(QStringLiteral("OmaStore"));
    app.setOrganizationName(QStringLiteral("OmaStore"));
    app.setApplicationVersion(QStringLiteral(OMASTORE_VERSION));
    app.setDesktopFileName(QStringLiteral("omastore"));
    QQuickStyle::setStyle(QStringLiteral("Basic"));

    QCommandLineParser args;
    args.setApplicationDescription(QStringLiteral("App store for Omarchy"));
    args.addHelpOption();
    const QCommandLineOption openOpt(QStringLiteral("open"), QStringLiteral("Opens the app owner/repo."),
                                     QStringLiteral("repo"));
    const QCommandLineOption shotOpt(QStringLiteral("screenshot"),
                                     QStringLiteral("Saves the window to <file> and exits (development)."),
                                     QStringLiteral("file"));
    const QCommandLineOption delayOpt(QStringLiteral("screenshot-delay"),
                                      QStringLiteral("Delay before the screenshot, in ms."), QStringLiteral("ms"),
                                      QStringLiteral("3000"));
    const QCommandLineOption checkOpt(QStringLiteral("check"),
                                      QStringLiteral("Opens the page for app authors and checks owner/repo."),
                                      QStringLiteral("repo"));
    const QCommandLineOption pageOpt(QStringLiteral("page"),
                                     QStringLiteral("Opens a page: discover, installed or publish."),
                                     QStringLiteral("page"));
    args.addOptions({openOpt, checkOpt, pageOpt, shotOpt, delayOpt});
    args.process(app);

    RpcClient rpc;
    Backend backend(&rpc);
    Theme theme;
    // Text.MarkdownText uses the global palette for links, not QML's.
    auto applyLinkColor = [&app, &theme] {
        QPalette p = app.palette();
        p.setColor(QPalette::Link, theme.accent());
        p.setColor(QPalette::LinkVisited, theme.accent());
        app.setPalette(p);
    };
    applyLinkColor();
    QObject::connect(&theme, &Theme::changed, &app, applyLinkColor);

    // After a self-update: the new interface takes over (it starts the new daemon).
    QObject::connect(&backend, &Backend::restartReady, &app, [](const QString &gui) {
        if (!QProcess::startDetached(gui, {}))
            qWarning("could not start %s", qPrintable(gui));
        QCoreApplication::quit();
    });

    QQmlApplicationEngine engine;
    OfflineNamFactory nam;
    engine.setNetworkAccessManagerFactory(&nam);
    engine.addImageProvider(QStringLiteral("omastore"), new DaemonImageProvider(&rpc));
    engine.rootContext()->setContextProperty(QStringLiteral("backend"), &backend);
    engine.rootContext()->setContextProperty(QStringLiteral("theme"), &theme);
    engine.rootContext()->setContextProperty(QStringLiteral("startupRepo"), args.value(openOpt));
    engine.rootContext()->setContextProperty(QStringLiteral("startupCheck"), args.value(checkOpt));
    engine.rootContext()->setContextProperty(QStringLiteral("startupPage"), args.value(pageOpt));

    QObject::connect(&engine, &QQmlApplicationEngine::objectCreationFailed, &app,
                     [] { QCoreApplication::exit(1); }, Qt::QueuedConnection);
    engine.loadFromModule("OmaStore", "Main");
    rpc.start();

    if (args.isSet(shotOpt)) {
        const QString file = args.value(shotOpt);
        QTimer::singleShot(args.value(delayOpt).toInt(), &app, [&engine, file] {
            const auto roots = engine.rootObjects();
            auto *win = roots.isEmpty() ? nullptr : qobject_cast<QQuickWindow *>(roots.first());
            const bool ok = win && win->grabWindow().save(file);
            if (!ok)
                qWarning("could not save the screenshot to %s", qPrintable(file));
            QCoreApplication::exit(ok ? 0 : 1);
        });
    }
    return app.exec();
}
