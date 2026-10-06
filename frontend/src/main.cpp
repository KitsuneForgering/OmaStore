#include "backend.h"
#include "imageprovider.h"
#include "offlinenam.h"
#include "rpcclient.h"
#include "singleinstance.h"
#include "theme.h"

#include <QCommandLineParser>
#include <QFont>
#include <QGuiApplication>
#include <QLibraryInfo>
#include <QLocale>
#include <QPalette>
#include <QProcess>
#include <QQuickWindow>
#include <QTimer>
#include <QTranslator>
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

    // The interface in the user's language (pt_BR, ...), falling back to
    // English. Qt's own strings (the Yes/No of dialogs) come from qt6-translations.
    QTranslator qtStrings, ourStrings;
    if (qtStrings.load(QLocale(), QStringLiteral("qt"), QStringLiteral("_"),
                       QLibraryInfo::path(QLibraryInfo::TranslationsPath)))
        app.installTranslator(&qtStrings);
    if (ourStrings.load(QLocale(), QStringLiteral("omastore"), QStringLiteral("_"), QStringLiteral(":/i18n")))
        app.installTranslator(&ourStrings);

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
    args.addPositionalArgument(QStringLiteral("link"), QStringLiteral("An omastore://owner/repo link to open."),
                               QStringLiteral("[link]"));
    args.process(app);

    StartRequest start{args.value(openOpt), args.value(checkOpt), args.value(pageOpt)};
    QString badLink;
    if (!args.positionalArguments().isEmpty()) {
        const QString link = args.positionalArguments().constFirst();
        start.open = repoFromLink(link);
        if (start.open.isEmpty())
            badLink = link;
    }
    // A window is already open: it shows the request and this launch ends.
    SingleInstance instance;
    if (!args.isSet(shotOpt) && badLink.isEmpty()) {
        if (instance.forward(start))
            return 0;
        if (!instance.listen())
            qWarning("could not create the single-instance socket; links open new windows");
    }

    RpcClient rpc;
    Backend backend(&rpc);
    Theme theme;
    // Every Text and control starts from the user's UI family (fontconfig's
    // sans-serif, which Omarchy points at its chosen font) at the theme's
    // body size; QML only picks steps of the scale.
    QFont uiFont = app.font();
    uiFont.setFamily(theme.fontFamily());
    uiFont.setPixelSize(theme.fontBody());
    app.setFont(uiFont);
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
    engine.rootContext()->setContextProperty(QStringLiteral("startupRepo"), start.open);
    engine.rootContext()->setContextProperty(QStringLiteral("startupCheck"), start.check);
    engine.rootContext()->setContextProperty(QStringLiteral("startupPage"), start.page);

    QObject::connect(&engine, &QQmlApplicationEngine::objectCreationFailed, &app,
                     [] { QCoreApplication::exit(1); }, Qt::QueuedConnection);
    engine.loadFromModule("OmaStore", "Main");
    rpc.start();
    if (!badLink.isEmpty())
        QTimer::singleShot(0, &backend, [&backend, badLink] {
            emit backend.errorOccurred(QCoreApplication::translate("main", "Not an OmaStore link: %1").arg(badLink));
        });

    QObject::connect(&instance, &SingleInstance::requested, &app, [&engine](const StartRequest &req) {
        const auto roots = engine.rootObjects();
        auto *win = roots.isEmpty() ? nullptr : qobject_cast<QQuickWindow *>(roots.first());
        if (!win)
            return;
        QMetaObject::invokeMethod(win, "handleRequest", Q_ARG(QVariant, req.toMap()));
        win->show();
        win->raise();
        win->requestActivate();
    });

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
