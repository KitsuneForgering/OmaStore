#include "theme.h"

#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QRegularExpression>

namespace {
// Palette used without Omarchy (and for keys missing from the theme).
const QHash<QString, QColor> &fallback()
{
    static const QHash<QString, QColor> colors{
        {QStringLiteral("accent"), QColor(0x7a, 0xa2, 0xf7)},
        {QStringLiteral("background"), QColor(0x1a, 0x1b, 0x26)},
        {QStringLiteral("lighter_background"), QColor(0x24, 0x28, 0x3b)},
        {QStringLiteral("selection"), QColor(0x2e, 0x34, 0x4f)},
        {QStringLiteral("foreground"), QColor(0xc0, 0xca, 0xf5)},
        {QStringLiteral("muted"), QColor(0x73, 0x7a, 0xa2)},
        {QStringLiteral("red"), QColor(0xf7, 0x76, 0x8e)},
        {QStringLiteral("green"), QColor(0x9e, 0xce, 0x6a)},
        {QStringLiteral("yellow"), QColor(0xe0, 0xaf, 0x68)},
    };
    return colors;
}
} // namespace

Theme::Theme(QStringList dirs, QObject *parent)
    : QObject(parent), m_dirs(dirs.isEmpty() ? defaultDirs() : std::move(dirs))
{
    m_debounce.setSingleShot(true);
    m_debounce.setInterval(200);
    connect(&m_debounce, &QTimer::timeout, this, &Theme::reload);
    auto schedule = [this] { m_debounce.start(); };
    connect(&m_watcher, &QFileSystemWatcher::fileChanged, this, schedule);
    connect(&m_watcher, &QFileSystemWatcher::directoryChanged, this, schedule);
    reload();
}

QStringList Theme::defaultDirs()
{
    const QString home = QDir::homePath();
    const QString state = qEnvironmentVariable("XDG_STATE_HOME", home + QStringLiteral("/.local/state"));
    const QString config = qEnvironmentVariable("XDG_CONFIG_HOME", home + QStringLiteral("/.config"));
    return {state + QStringLiteral("/omarchy/current/theme"), config + QStringLiteral("/omarchy/current/theme")};
}

QHash<QString, QString> Theme::parseColors(const QByteArray &toml)
{
    static const QRegularExpression line(
        QStringLiteral(R"(^\s*([A-Za-z0-9_\-]+)\s*=\s*["']([^"']*)["']\s*(#.*)?$)"));
    QHash<QString, QString> out;
    for (const QByteArray &raw : toml.split('\n')) {
        const auto m = line.match(QString::fromUtf8(raw));
        if (m.hasMatch())
            out.insert(m.captured(1), m.captured(2).trimmed());
    }
    return out;
}

QColor Theme::color(const char *key) const
{
    const QString k = QLatin1String(key);
    const QColor c = m_colors.value(k);
    return c.isValid() ? c : fallback().value(k);
}

void Theme::reload()
{
    QHash<QString, QColor> colors;
    bool dark = true;
    QString name;
    for (const QString &dir : std::as_const(m_dirs)) {
        QFile f(dir + QStringLiteral("/colors.toml"));
        if (!f.open(QIODevice::ReadOnly))
            continue;
        const auto values = parseColors(f.read(64 * 1024));
        for (auto it = values.constBegin(); it != values.constEnd(); ++it) {
            const QColor c = QColor::fromString(it.value());
            if (c.isValid())
                colors.insert(it.key(), c);
        }
        dark = values.value(QStringLiteral("mode"), QStringLiteral("dark")) != QLatin1String("light");
        QFile nameFile(QFileInfo(dir).dir().filePath(QStringLiteral("theme.name")));
        if (nameFile.open(QIODevice::ReadOnly))
            name = QString::fromUtf8(nameFile.readAll()).trimmed();
        break;
    }
    const bool differs = colors != m_colors || dark != m_dark || name != m_name;
    m_colors = colors;
    m_dark = dark;
    m_name = name;
    watch();
    if (differs)
        emit changed();
}

// Omarchy switches themes by replacing the "current/theme" directory; so we
// watch the parent directory, the theme itself and colors.toml.
void Theme::watch()
{
    if (!m_watcher.files().isEmpty())
        m_watcher.removePaths(m_watcher.files());
    if (!m_watcher.directories().isEmpty())
        m_watcher.removePaths(m_watcher.directories());
    QStringList paths;
    for (const QString &dir : std::as_const(m_dirs)) {
        const QFileInfo fi(dir);
        for (const QString &p : {fi.dir().absolutePath(), dir, dir + QStringLiteral("/colors.toml")}) {
            if (QFileInfo::exists(p))
                paths << p;
        }
    }
    if (!paths.isEmpty())
        m_watcher.addPaths(paths);
}
