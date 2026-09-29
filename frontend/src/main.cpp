#include "backend.h"
#include "imageprovider.h"
#include "offlinenam.h"
#include "rpcclient.h"
#include "theme.h"

#include <QCommandLineParser>
#include <QGuiApplication>
#include <QPalette>
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
    app.setDesktopFileName(QStringLiteral("omastore"));
    QQuickStyle::setStyle(QStringLiteral("Basic"));

    QCommandLineParser args;
    args.setApplicationDescription(QStringLiteral("Loja de apps do Omarchy"));
    args.addHelpOption();
    const QCommandLineOption openOpt(QStringLiteral("open"), QStringLiteral("Abre o app owner/repo."),
                                     QStringLiteral("repo"));
    const QCommandLineOption shotOpt(QStringLiteral("screenshot"),
                                     QStringLiteral("Salva a janela em <arquivo> e sai (desenvolvimento)."),
                                     QStringLiteral("arquivo"));
    const QCommandLineOption delayOpt(QStringLiteral("screenshot-delay"),
                                      QStringLiteral("Espera antes do screenshot, em ms."), QStringLiteral("ms"),
                                      QStringLiteral("3000"));
    args.addOptions({openOpt, shotOpt, delayOpt});
    args.process(app);

    RpcClient rpc;
    Backend backend(&rpc);
    Theme theme;
    // Text.MarkdownText usa a paleta global para links, não a do QML.
    auto applyLinkColor = [&app, &theme] {
        QPalette p = app.palette();
        p.setColor(QPalette::Link, theme.accent());
        p.setColor(QPalette::LinkVisited, theme.accent());
        app.setPalette(p);
    };
    applyLinkColor();
    QObject::connect(&theme, &Theme::changed, &app, applyLinkColor);

    QQmlApplicationEngine engine;
    OfflineNamFactory nam;
    engine.setNetworkAccessManagerFactory(&nam);
    engine.addImageProvider(QStringLiteral("omastore"), new DaemonImageProvider(&rpc));
    engine.rootContext()->setContextProperty(QStringLiteral("backend"), &backend);
    engine.rootContext()->setContextProperty(QStringLiteral("theme"), &theme);
    engine.rootContext()->setContextProperty(QStringLiteral("startupRepo"), args.value(openOpt));

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
                qWarning("não foi possível salvar o screenshot em %s", qPrintable(file));
            QCoreApplication::exit(ok ? 0 : 1);
        });
    }
    return app.exec();
}
