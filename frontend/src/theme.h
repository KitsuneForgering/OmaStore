#pragma once

#include <QColor>
#include <QFileSystemWatcher>
#include <QHash>
#include <QObject>
#include <QTimer>

#include <algorithm>
#include <cmath>

// Colors of the active Omarchy theme (colors.toml), reloaded automatically
// when the user switches themes. Without Omarchy, uses a default palette that
// follows the system's light/dark preference.
//
// Every color meant for text is adjusted to WCAG AAA against the backgrounds
// it is drawn on (7:1: background, surface, selection, hover), keeping its hue
// and only moving its lightness; borders and the focus ring get at least 3:1
// (WCAG 1.4.11). Fills that carry text come with their text color (onAccent,
// onDanger), also at 7:1.
class Theme : public QObject {
    Q_OBJECT
    Q_PROPERTY(bool dark READ dark NOTIFY changed)
    Q_PROPERTY(QString name READ name NOTIFY changed)
    Q_PROPERTY(QColor accent READ accent NOTIFY changed)
    Q_PROPERTY(QColor background READ background NOTIFY changed)
    Q_PROPERTY(QColor surface READ surface NOTIFY changed)
    Q_PROPERTY(QColor surfaceAlt READ surfaceAlt NOTIFY changed)
    Q_PROPERTY(QColor foreground READ foreground NOTIFY changed)
    Q_PROPERTY(QColor muted READ muted NOTIFY changed)
    Q_PROPERTY(QColor selection READ selection NOTIFY changed)
    Q_PROPERTY(QColor danger READ danger NOTIFY changed)
    Q_PROPERTY(QColor success READ success NOTIFY changed)
    Q_PROPERTY(QColor warning READ warning NOTIFY changed)
    // Background of a hovered item (text on it keeps 7:1).
    Q_PROPERTY(QColor hover READ hover NOTIFY changed)
    // Boundary of inputs, buttons and anything that must be seen (3:1).
    Q_PROPERTY(QColor border READ border NOTIFY changed)
    // Quiet edge of cards and dividers: enough to separate a surface from the
    // background on themes where the two are close (1.6:1), never louder.
    Q_PROPERTY(QColor outline READ outline NOTIFY changed)
    // Keyboard focus ring and selected-text highlight (3:1 against every
    // background), and the text on it (7:1).
    Q_PROPERTY(QColor focus READ focus NOTIFY changed)
    Q_PROPERTY(QColor onFocus READ onFocus NOTIFY changed)
    // Primary button / highlight fill and the text on it (7:1).
    Q_PROPERTY(QColor accentFill READ accentFill NOTIFY changed)
    Q_PROPERTY(QColor onAccent READ onAccent NOTIFY changed)
    // Error message fill and the text on it (7:1).
    Q_PROPERTY(QColor dangerFill READ dangerFill NOTIFY changed)
    Q_PROPERTY(QColor onDanger READ onDanger NOTIFY changed)
    // Animation durations in ms; 0 when the user asked for reduced motion
    // (gtk-enable-animations=false in the GTK settings, or
    // OMASTORE_REDUCE_MOTION=1), so every transition becomes instant (WCAG 2.3.3).
    Q_PROPERTY(bool reducedMotion READ reducedMotion NOTIFY changed)
    Q_PROPERTY(int durationShort READ durationShort NOTIFY changed)
    Q_PROPERTY(int durationMedium READ durationMedium NOTIFY changed)

    // Typography: the families go through fontconfig, so they follow the
    // fonts Omarchy (or the user) configured; the sizes, in pixels, are a scale
    // built on the system's UI font, so a larger system font grows every step.
    Q_PROPERTY(QString fontFamily READ fontFamily CONSTANT)
    Q_PROPERTY(QString monoFamily READ monoFamily CONSTANT)
    Q_PROPERTY(int fontCaption READ fontCaption CONSTANT)   // labels, metadata
    Q_PROPERTY(int fontBody READ fontBody CONSTANT)         // UI text
    Q_PROPERTY(int fontReading READ fontReading CONSTANT)   // README, long text
    Q_PROPERTY(int fontSubtitle READ fontSubtitle CONSTANT) // card titles
    Q_PROPERTY(int fontTitle READ fontTitle CONSTANT)       // page and section titles
    Q_PROPERTY(int fontHeadline READ fontHeadline CONSTANT) // the app's name
    // Spacing on a 4 px grid and corner radii.
    Q_PROPERTY(int spaceXs READ spaceXs CONSTANT)
    Q_PROPERTY(int spaceS READ spaceS CONSTANT)
    Q_PROPERTY(int spaceM READ spaceM CONSTANT)
    Q_PROPERTY(int spaceL READ spaceL CONSTANT)
    Q_PROPERTY(int spaceXl READ spaceXl CONSTANT)
    Q_PROPERTY(int spaceXxl READ spaceXxl CONSTANT)
    Q_PROPERTY(int radiusS READ radiusS CONSTANT)
    Q_PROPERTY(int radiusM READ radiusM CONSTANT)

public:
    // dirs: theme directories in order of preference. Empty = Omarchy's
    // default (~/.local/state/omarchy/current/theme and the old path
    // ~/.config/omarchy/current/theme).
    explicit Theme(QStringList dirs = {}, QObject *parent = nullptr);

    static QStringList defaultDirs();
    // Reads key = "value" pairs from a colors.toml (the subset of TOML used
    // by Omarchy themes).
    static QHash<QString, QString> parseColors(const QByteArray &toml);

    // WCAG 2 relative luminance and contrast ratio (1 to 21).
    static double luminance(const QColor &c);
    static double contrast(const QColor &a, const QColor &b);
    // c with its lightness moved (toward white on dark backgrounds, toward
    // black on light ones) until its contrast with every color in bgs is at
    // least target; unchanged if it already is.
    static QColor ensureContrast(const QColor &c, const QList<QColor> &bgs, double target);

    // Minimum contrast for text (WCAG AAA, 1.4.6) and for UI boundaries (1.4.11).
    static constexpr double TextContrast = 7.0;
    static constexpr double UiContrast = 3.0;
    // Decorative edges (outline): visible, not a boundary WCAG requires.
    static constexpr double OutlineContrast = 1.6;

    bool dark() const { return m_dark; }
    QString name() const { return m_name; }
    QColor accent() const { return m_ui.value(QStringLiteral("accent")); }
    QColor background() const { return m_ui.value(QStringLiteral("background")); }
    QColor surface() const { return m_ui.value(QStringLiteral("surface")); }
    QColor surfaceAlt() const { return selection(); }
    QColor foreground() const { return m_ui.value(QStringLiteral("foreground")); }
    QColor muted() const { return m_ui.value(QStringLiteral("muted")); }
    QColor selection() const { return m_ui.value(QStringLiteral("selection")); }
    QColor danger() const { return m_ui.value(QStringLiteral("danger")); }
    QColor success() const { return m_ui.value(QStringLiteral("success")); }
    QColor warning() const { return m_ui.value(QStringLiteral("warning")); }
    QColor hover() const { return m_ui.value(QStringLiteral("hover")); }
    QColor border() const { return m_ui.value(QStringLiteral("border")); }
    QColor outline() const { return m_ui.value(QStringLiteral("outline")); }
    QColor focus() const { return m_ui.value(QStringLiteral("focus")); }
    QColor onFocus() const { return m_ui.value(QStringLiteral("onFocus")); }
    QColor accentFill() const { return m_ui.value(QStringLiteral("accentFill")); }
    QColor onAccent() const { return m_ui.value(QStringLiteral("onAccent")); }
    QColor dangerFill() const { return m_ui.value(QStringLiteral("dangerFill")); }
    QColor onDanger() const { return m_ui.value(QStringLiteral("onDanger")); }
    bool reducedMotion() const { return m_reducedMotion; }
    int durationShort() const { return m_reducedMotion ? 0 : 120; }
    int durationMedium() const { return m_reducedMotion ? 0 : 220; }
    QString fontFamily() const { return QStringLiteral("sans-serif"); }
    QString monoFamily() const { return QStringLiteral("monospace"); }
    int fontCaption() const { return std::max(12, step(0.86)); }
    int fontBody() const { return m_basePx; }
    int fontReading() const { return std::max(17, step(1.15)); }
    int fontSubtitle() const { return step(1.15); }
    int fontTitle() const { return step(1.43); }
    int fontHeadline() const { return step(1.86); }
    int spaceXs() const { return 4; }
    int spaceS() const { return 8; }
    int spaceM() const { return 12; }
    int spaceL() const { return 16; }
    int spaceXl() const { return 24; }
    int spaceXxl() const { return 32; }
    int radiusS() const { return 6; }
    int radiusM() const { return 10; }
    // Body size in pixels from the application font (14 when unknown),
    // kept between 13 and 24.
    static int basePixelSize();
    // Whether the environment asks for reduced motion (see reducedMotion).
    static bool prefersReducedMotion();

    Q_INVOKABLE void reload();

signals:
    void changed();

private:
    QColor color(const char *key) const;
    int step(double ratio) const { return int(std::lround(m_basePx * ratio)); }
    void watch();
    void derive();

    QStringList m_dirs;
    QHash<QString, QColor> m_colors; // as read from colors.toml
    QHash<QString, QColor> m_ui;     // derived, accessible colors
    bool m_dark = true;
    bool m_reducedMotion = false;
    int m_basePx = 14;
    QString m_name;
    QFileSystemWatcher m_watcher;
    QTimer m_debounce;
};
