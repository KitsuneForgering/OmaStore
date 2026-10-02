#include "theme.h"

#include <QDir>
#include <QFile>
#include <QFileInfo>
#include <QFontInfo>
#include <QGuiApplication>
#include <QRegularExpression>
#include <QStyleHints>

#include <algorithm>
#include <cmath>
#include <tuple>

namespace {
// Palettes used without Omarchy (and for keys missing from the theme).
const QHash<QString, QColor> &fallbackDark()
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

const QHash<QString, QColor> &fallbackLight()
{
    static const QHash<QString, QColor> colors{
        {QStringLiteral("accent"), QColor(0x2e, 0x5c, 0xb8)},
        {QStringLiteral("background"), QColor(0xf7, 0xf7, 0xf8)},
        {QStringLiteral("lighter_background"), QColor(0xec, 0xed, 0xf0)},
        {QStringLiteral("selection"), QColor(0xd8, 0xe2, 0xf5)},
        {QStringLiteral("foreground"), QColor(0x1f, 0x23, 0x2b)},
        {QStringLiteral("muted"), QColor(0x55, 0x5b, 0x66)},
        {QStringLiteral("red"), QColor(0xb4, 0x23, 0x3a)},
        {QStringLiteral("green"), QColor(0x2f, 0x6f, 0x2a)},
        {QStringLiteral("yellow"), QColor(0x8a, 0x5a, 0x00)},
    };
    return colors;
}

bool systemPrefersLight()
{
    return qobject_cast<QGuiApplication *>(QCoreApplication::instance())
        && QGuiApplication::styleHints()->colorScheme() == Qt::ColorScheme::Light;
}

double channel(double c)
{
    return c <= 0.04045 ? c / 12.92 : std::pow((c + 0.055) / 1.055, 2.4);
}

QColor mix(const QColor &a, const QColor &b, double t)
{
    return QColor::fromRgbF(float(a.redF() + (b.redF() - a.redF()) * t), float(a.greenF() + (b.greenF() - a.greenF()) * t),
                            float(a.blueF() + (b.blueF() - a.blueF()) * t));
}

double minContrast(const QColor &c, const QList<QColor> &bgs)
{
    double m = 21;
    for (const QColor &bg : bgs)
        m = std::min(m, Theme::contrast(c, bg));
    return m;
}

// A fill and the text on it: black or white, whichever reads better; when
// neither reaches TextContrast, the fill moves away from the text color.
std::pair<QColor, QColor> fillWithText(const QColor &fill)
{
    const QColor black(Qt::black), white(Qt::white);
    const QColor text = Theme::contrast(fill, black) >= Theme::contrast(fill, white) ? black : white;
    QColor f = fill;
    for (int i = 1; i <= 50 && Theme::contrast(f, text) < Theme::TextContrast; ++i)
        f = mix(fill, text == black ? white : black, i / 50.0);
    return {f, text};
}
} // namespace

double Theme::luminance(const QColor &c)
{
    return 0.2126 * channel(c.redF()) + 0.7152 * channel(c.greenF()) + 0.0722 * channel(c.blueF());
}

double Theme::contrast(const QColor &a, const QColor &b)
{
    const double la = luminance(a), lb = luminance(b);
    return (std::max(la, lb) + 0.05) / (std::min(la, lb) + 0.05);
}

QColor Theme::ensureContrast(const QColor &c, const QList<QColor> &bgs, double target)
{
    if (bgs.isEmpty() || minContrast(c, bgs) >= target)
        return c;
    // Toward the extreme that contrasts more with the backgrounds.
    double darkest = 1, lightest = 0;
    for (const QColor &bg : bgs) {
        darkest = std::min(darkest, luminance(bg));
        lightest = std::max(lightest, luminance(bg));
    }
    const QColor white(Qt::white), black(Qt::black);
    // Worst case of each: white against the lightest background, black against the darkest.
    const QColor extreme = 1.05 / (lightest + 0.05) >= (darkest + 0.05) / 0.05 ? white : black;
    QColor best = c;
    for (int i = 1; i <= 100; ++i) {
        best = mix(c, extreme, i / 100.0);
        if (minContrast(best, bgs) >= target)
            break;
    }
    return best;
}

int Theme::basePixelSize()
{
    if (!qobject_cast<QGuiApplication *>(QCoreApplication::instance()))
        return 14;
    const int px = QFontInfo(QGuiApplication::font()).pixelSize();
    return px > 0 ? std::clamp(px, 13, 24) : 14;
}

Theme::Theme(QStringList dirs, QObject *parent)
    : QObject(parent), m_dirs(dirs.isEmpty() ? defaultDirs() : std::move(dirs)), m_basePx(basePixelSize())
{
    m_debounce.setSingleShot(true);
    m_debounce.setInterval(200);
    connect(&m_debounce, &QTimer::timeout, this, &Theme::reload);
    auto schedule = [this] { m_debounce.start(); };
    connect(&m_watcher, &QFileSystemWatcher::fileChanged, this, schedule);
    connect(&m_watcher, &QFileSystemWatcher::directoryChanged, this, schedule);
    // Without Omarchy the palette follows the system's light/dark preference.
    if (qobject_cast<QGuiApplication *>(QCoreApplication::instance()))
        connect(QGuiApplication::styleHints(), &QStyleHints::colorSchemeChanged, this, schedule);
    reload();
}

bool Theme::prefersReducedMotion()
{
    const QByteArray env = qgetenv("OMASTORE_REDUCE_MOTION").trimmed().toLower();
    if (!env.isEmpty())
        return env != "0" && env != "false" && env != "no";
    // GTK's switch, which GNOME's "Reduce animation" and Omarchy users set.
    const QString config = qEnvironmentVariable("XDG_CONFIG_HOME", QDir::homePath() + QStringLiteral("/.config"));
    static const QRegularExpression off(QStringLiteral(R"(^\s*gtk-enable-animations\s*=\s*(false|0)\s*$)"),
                                        QRegularExpression::CaseInsensitiveOption | QRegularExpression::MultilineOption);
    for (const char *dir : {"/gtk-4.0/settings.ini", "/gtk-3.0/settings.ini"}) {
        QFile f(config + QLatin1String(dir));
        if (f.open(QIODevice::ReadOnly) && off.match(QString::fromUtf8(f.read(64 * 1024))).hasMatch())
            return true;
    }
    return false;
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
    return c.isValid() ? c : (m_dark ? fallbackDark() : fallbackLight()).value(k);
}

void Theme::derive()
{
    QHash<QString, QColor> ui;
    const QColor bg = color("background"), surface = color("lighter_background"), sel = color("selection");
    const QColor fgRaw = color("foreground");
    // Hover: a step from the surface toward the text, so it shows on any theme.
    const QColor hover = mix(surface, fgRaw, 0.08);
    const QList<QColor> grounds{bg, surface, sel, hover};
    ui.insert(QStringLiteral("background"), bg);
    ui.insert(QStringLiteral("surface"), surface);
    ui.insert(QStringLiteral("selection"), sel);
    ui.insert(QStringLiteral("hover"), hover);
    ui.insert(QStringLiteral("foreground"), ensureContrast(fgRaw, grounds, TextContrast));
    ui.insert(QStringLiteral("muted"), ensureContrast(color("muted"), grounds, TextContrast));
    ui.insert(QStringLiteral("accent"), ensureContrast(color("accent"), grounds, TextContrast));
    ui.insert(QStringLiteral("danger"), ensureContrast(color("red"), grounds, TextContrast));
    ui.insert(QStringLiteral("success"), ensureContrast(color("green"), grounds, TextContrast));
    ui.insert(QStringLiteral("warning"), ensureContrast(color("yellow"), grounds, TextContrast));
    ui.insert(QStringLiteral("border"), ensureContrast(color("muted"), {bg, surface}, UiContrast));
    ui.insert(QStringLiteral("outline"), ensureContrast(mix(surface, fgRaw, 0.12), {bg, surface}, OutlineContrast));
    // The focus color also highlights selected text: it needs 3:1 around it and a
    // text color with 7:1 on it. Pushing it further from the backgrounds only
    // helps both, so it moves until the text fits too.
    QColor focus = ensureContrast(color("accent"), grounds, UiContrast);
    auto [focusFill, onFocus] = fillWithText(focus);
    if (minContrast(focusFill, grounds) < UiContrast)
        std::tie(focusFill, onFocus) = fillWithText(ensureContrast(focus, grounds, TextContrast));
    ui.insert(QStringLiteral("focus"), focusFill);
    ui.insert(QStringLiteral("onFocus"), onFocus);
    const auto [accentFill, onAccent] = fillWithText(color("accent"));
    ui.insert(QStringLiteral("accentFill"), accentFill);
    ui.insert(QStringLiteral("onAccent"), onAccent);
    const auto [dangerFill, onDanger] = fillWithText(color("red"));
    ui.insert(QStringLiteral("dangerFill"), dangerFill);
    ui.insert(QStringLiteral("onDanger"), onDanger);
    m_ui = ui;
}

void Theme::reload()
{
    QHash<QString, QColor> colors;
    bool dark = !systemPrefersLight(); // without Omarchy, follow the system
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
        const QString mode = values.value(QStringLiteral("mode"));
        if (mode == QLatin1String("light") || mode == QLatin1String("dark"))
            dark = mode == QLatin1String("dark");
        else if (colors.contains(QStringLiteral("background")))
            dark = luminance(colors.value(QStringLiteral("background"))) < 0.18; // no mode key: judge by the background
        else
            dark = true;
        QFile nameFile(QFileInfo(dir).dir().filePath(QStringLiteral("theme.name")));
        if (nameFile.open(QIODevice::ReadOnly))
            name = QString::fromUtf8(nameFile.readAll()).trimmed();
        break;
    }
    const bool reduced = prefersReducedMotion();
    const bool differs = colors != m_colors || dark != m_dark || name != m_name || reduced != m_reducedMotion;
    m_colors = colors;
    m_dark = dark;
    m_name = name;
    m_reducedMotion = reduced;
    derive();
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
