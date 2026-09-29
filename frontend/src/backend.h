#pragma once

#include "catalogmodel.h"
#include "jobsmodel.h"

#include <QObject>
#include <QVariantList>
#include <QVariantMap>

class RpcClient;

// Facade exposed to QML: models, detail of the selected app, categories and
// actions (install, update, remove, index). All the logic lives in the
// daemon; here there are only RPC calls and presentation state.
class Backend : public QObject {
    Q_OBJECT
    Q_PROPERTY(bool connected READ connected NOTIFY connectedChanged)
    Q_PROPERTY(CatalogModel *catalog READ catalog CONSTANT)
    Q_PROPERTY(CatalogModel *installed READ installed CONSTANT)
    Q_PROPERTY(JobsModel *jobs READ jobs CONSTANT)
    Q_PROPERTY(QVariantList categories READ categories NOTIFY categoriesChanged)
    Q_PROPERTY(QVariantMap detail READ detail NOTIFY detailChanged)
    Q_PROPERTY(bool detailLoading READ detailLoading NOTIFY detailChanged)
    Q_PROPERTY(QVariantList similar READ similar NOTIFY similarChanged)
    Q_PROPERTY(int updatesAvailable READ updatesAvailable NOTIFY updatesAvailableChanged)

public:
    explicit Backend(RpcClient *rpc, QObject *parent = nullptr);

    bool connected() const;
    CatalogModel *catalog() const { return m_catalog; }
    CatalogModel *installed() const { return m_installed; }
    JobsModel *jobs() const { return m_jobs; }
    QVariantList categories() const { return m_categories; }
    QVariantMap detail() const { return m_detail; }
    bool detailLoading() const { return m_detailLoading; }
    QVariantList similar() const { return m_similar; }
    int updatesAvailable() const;

    Q_INVOKABLE void openDetail(const QString &repo);
    Q_INVOKABLE void closeDetail();
    Q_INVOKABLE void install(const QString &repo);
    Q_INVOKABLE void update(const QString &repo);
    Q_INVOKABLE void uninstall(const QString &repo);
    Q_INVOKABLE void updateAll();
    Q_INVOKABLE void refreshIndex(bool force = false);
    Q_INVOKABLE void cancelJob(const QString &jobId);
    // README markdown ready to display (without remote images).
    Q_INVOKABLE QString readmeForDisplay(const QString &markdown) const;

    // Friendly message for a daemon error code (docs/ipc.md).
    static QString friendlyError(int code, const QString &message);

signals:
    void connectedChanged();
    void categoriesChanged();
    void detailChanged();
    void similarChanged();
    void updatesAvailableChanged();
    // Error to show to the user.
    void errorOccurred(const QString &message);
    // Informational notice (e.g. installation finished).
    void notice(const QString &message);

private:
    void loadCategories();
    void reloadDetail();
    void loadSimilar(const QString &repo);
    void startJob(const QString &method, const QString &repo);
    void maybeIndexOnFirstRun();

    RpcClient *m_rpc;
    CatalogModel *m_catalog;
    CatalogModel *m_installed;
    JobsModel *m_jobs;
    QVariantList m_categories;
    QString m_detailRepo;
    QVariantMap m_detail;
    QVariantList m_similar;
    bool m_detailLoading = false;
    bool m_checkedEmpty = false;
};
