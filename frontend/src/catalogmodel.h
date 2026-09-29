#pragma once

#include <QAbstractListModel>
#include <QJsonArray>
#include <QPointer>
#include <QTimer>

class RpcClient;

// App list coming from catalog.list. Reloads on its own when the filters
// change, when the connection comes back and when the daemon sends catalog.changed.
class CatalogModel : public QAbstractListModel {
    Q_OBJECT
    Q_PROPERTY(QString category READ category WRITE setCategory NOTIFY categoryChanged)
    Q_PROPERTY(QString query READ query WRITE setQuery NOTIFY queryChanged)
    Q_PROPERTY(bool installedOnly READ installedOnly WRITE setInstalledOnly NOTIFY installedOnlyChanged)
    Q_PROPERTY(int count READ rowCount NOTIFY countChanged)
    Q_PROPERTY(bool loading READ loading NOTIFY loadingChanged)
    Q_PROPERTY(QString error READ error NOTIFY errorChanged)

public:
    enum Role {
        RepoRole = Qt::UserRole + 1,
        NameRole,
        SummaryRole,
        IconUrlRole,
        CategoryRole,
        StarsRole,
        InstallableRole,
        LatestVersionRole,
        InstalledVersionRole,
        UpdateAvailableRole,
        ScreenshotsRole,
    };
    Q_ENUM(Role)

    explicit CatalogModel(RpcClient *rpc, QObject *parent = nullptr);

    int rowCount(const QModelIndex &parent = {}) const override;
    QVariant data(const QModelIndex &index, int role) const override;
    QHash<int, QByteArray> roleNames() const override;

    QString category() const { return m_category; }
    QString query() const { return m_query; }
    bool installedOnly() const { return m_installedOnly; }
    bool loading() const { return m_loading; }
    QString error() const { return m_error; }

    void setCategory(const QString &c);
    void setQuery(const QString &q);
    void setInstalledOnly(bool on);

    // Wait before reloading after a filter change.
    void setDebounce(int ms) { m_debounce.setInterval(ms); }

    Q_INVOKABLE void reload();
    Q_INVOKABLE QVariantMap get(int row) const;
    Q_INVOKABLE int indexOf(const QString &repo) const;

    // Replaces the content (used when the response arrives; public for tests).
    void setItems(const QJsonArray &items);

signals:
    void categoryChanged();
    void queryChanged();
    void installedOnlyChanged();
    void countChanged();
    void loadingChanged();
    void errorChanged();

private:
    void scheduleReload();
    void setLoading(bool on);
    void setError(const QString &e);

    QPointer<RpcClient> m_rpc;
    QJsonArray m_items;
    QString m_category;
    QString m_query;
    bool m_installedOnly = false;
    bool m_loading = false;
    QString m_error;
    QTimer m_debounce;
    quint64 m_generation = 0; // discards responses to old requests
};
