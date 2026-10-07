#pragma once

#include <QAbstractListModel>
#include <QJsonArray>
#include <QJsonObject>
#include <QPointer>
#include <QTimer>

class RpcClient;

// App list coming from catalog.list. Reloads on its own when the filters
// change, when the connection comes back and when the daemon sends catalog.changed.
// With a page size, it loads that many apps and the next page when a view
// scrolls to the end (fetchMore).
class CatalogModel : public QAbstractListModel {
    Q_OBJECT
    Q_PROPERTY(QString category READ category WRITE setCategory NOTIFY categoryChanged)
    Q_PROPERTY(QString query READ query WRITE setQuery NOTIFY queryChanged)
    Q_PROPERTY(bool installedOnly READ installedOnly WRITE setInstalledOnly NOTIFY installedOnlyChanged)
    Q_PROPERTY(int count READ rowCount NOTIFY countChanged)
    Q_PROPERTY(bool loading READ loading NOTIFY loadingChanged)
    // More pages are left on the daemon (the count is a lower bound).
    Q_PROPERTY(bool hasMore READ hasMore NOTIFY countChanged)
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
        BlockedRole,
    };
    Q_ENUM(Role)

    explicit CatalogModel(RpcClient *rpc, QObject *parent = nullptr);

    int rowCount(const QModelIndex &parent = {}) const override;
    QVariant data(const QModelIndex &index, int role) const override;
    QHash<int, QByteArray> roleNames() const override;
    bool canFetchMore(const QModelIndex &parent) const override;
    void fetchMore(const QModelIndex &parent) override;

    QString category() const { return m_category; }
    QString query() const { return m_query; }
    bool installedOnly() const { return m_installedOnly; }
    bool loading() const { return m_loading; }
    bool hasMore() const { return !m_complete; }
    QString error() const { return m_error; }

    void setCategory(const QString &c);
    void setQuery(const QString &q);
    void setInstalledOnly(bool on);

    // Wait before reloading after a filter change.
    void setDebounce(int ms) { m_debounce.setInterval(ms); }
    // Apps per request; 0 (the default) loads everything at once.
    void setPageSize(int n) { m_pageSize = n; }

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
    void load(bool keepCount);
    QJsonObject filterParams() const;
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
    int m_pageSize = 0;
    bool m_complete = true;   // the last response had everything left
    bool m_keepCount = false; // the pending reload keeps the loaded pages
};
