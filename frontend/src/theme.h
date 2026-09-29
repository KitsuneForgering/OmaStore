#pragma once

#include <QColor>
#include <QFileSystemWatcher>
#include <QHash>
#include <QObject>
#include <QTimer>

// Cores do tema ativo do Omarchy (colors.toml), com recarga automática
// quando o usuário troca de tema. Sem Omarchy, usa uma paleta escura padrão.
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

public:
    // dirs: diretórios de tema em ordem de preferência. Vazio = padrão do
    // Omarchy (~/.local/state/omarchy/current/theme e o caminho antigo
    // ~/.config/omarchy/current/theme).
    explicit Theme(QStringList dirs = {}, QObject *parent = nullptr);

    static QStringList defaultDirs();
    // Lê pares chave = "valor" de um colors.toml (subconjunto do TOML usado
    // pelos temas do Omarchy).
    static QHash<QString, QString> parseColors(const QByteArray &toml);

    bool dark() const { return m_dark; }
    QString name() const { return m_name; }
    QColor accent() const { return color("accent"); }
    QColor background() const { return color("background"); }
    QColor surface() const { return color("lighter_background"); }
    QColor surfaceAlt() const { return color("selection"); }
    QColor foreground() const { return color("foreground"); }
    QColor muted() const { return color("muted"); }
    QColor selection() const { return color("selection"); }
    QColor danger() const { return color("red"); }
    QColor success() const { return color("green"); }
    QColor warning() const { return color("yellow"); }

    Q_INVOKABLE void reload();

signals:
    void changed();

private:
    QColor color(const char *key) const;
    void watch();

    QStringList m_dirs;
    QHash<QString, QColor> m_colors;
    bool m_dark = true;
    QString m_name;
    QFileSystemWatcher m_watcher;
    QTimer m_debounce;
};
