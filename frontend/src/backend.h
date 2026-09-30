#pragma once

#include "catalogmodel.h"
#include "jobsmodel.h"

#include <QJsonObject>
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
    // Compatibility report of the last author.check (Publish page).
    Q_PROPERTY(QVariantMap authorCheck READ authorCheck NOTIFY authorCheckChanged)
    Q_PROPERTY(bool authorCheckBusy READ authorCheckBusy NOTIFY authorCheckChanged)
    Q_PROPERTY(QString authorCheckError READ authorCheckError NOTIFY authorCheckChanged)
    // Why the last catalog refresh failed ("" after a successful one).
    Q_PROPERTY(QString indexError READ indexError NOTIFY indexErrorChanged)

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
    QVariantMap authorCheck() const { return m_authorCheck; }
    bool authorCheckBusy() const { return m_authorCheckBusy; }
    QString authorCheckError() const { return m_authorCheckError; }
    QString indexError() const { return m_indexError; }

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
    // Asks the daemon what the store sees of a repository (owner/repo or a
    // GitHub URL). A non-empty manifest is tested instead of the published one.
    Q_INVOKABLE void checkRepo(const QString &repo, const QString &manifest = {});
    Q_INVOKABLE void clearAuthorCheck();
    Q_INVOKABLE void copyText(const QString &text);

    // "owner/repo" from what a user typed or pasted (URL, .git, spaces);
    // "" if it is not a repository name.
    static QString normalizeRepo(const QString &input);

    // Friendly message for a daemon error code (docs/ipc.md).
    static QString friendlyError(int code, const QString &message);

signals:
    void connectedChanged();
    void categoriesChanged();
    void detailChanged();
    void similarChanged();
    void updatesAvailableChanged();
    void authorCheckChanged();
    void indexErrorChanged();
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
    void sendAuthorCheck(const QJsonObject &params);

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
    QVariantMap m_authorCheck;
    bool m_authorCheckBusy = false;
    QString m_authorCheckError;
    int m_authorCheckSeq = 0;
    QJsonObject m_pendingCheck; // author.check asked for before connecting
    QString m_indexError;
};
