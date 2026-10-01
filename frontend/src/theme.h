#pragma once

#include <QColor>
#include <QFileSystemWatcher>
#include <QHash>
#include <QObject>
#include <QTimer>

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
    // Boundary of inputs, cards and dividers that must be seen (3:1).
    Q_PROPERTY(QColor border READ border NOTIFY changed)
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
    QColor focus() const { return m_ui.value(QStringLiteral("focus")); }
    QColor onFocus() const { return m_ui.value(QStringLiteral("onFocus")); }
    QColor accentFill() const { return m_ui.value(QStringLiteral("accentFill")); }
    QColor onAccent() const { return m_ui.value(QStringLiteral("onAccent")); }
    QColor dangerFill() const { return m_ui.value(QStringLiteral("dangerFill")); }
    QColor onDanger() const { return m_ui.value(QStringLiteral("onDanger")); }

    Q_INVOKABLE void reload();

signals:
    void changed();

private:
    QColor color(const char *key) const;
    void watch();
    void derive();

    QStringList m_dirs;
    QHash<QString, QColor> m_colors; // as read from colors.toml
    QHash<QString, QColor> m_ui;     // derived, accessible colors
    bool m_dark = true;
    QString m_name;
    QFileSystemWatcher m_watcher;
    QTimer m_debounce;
};
