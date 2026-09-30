#include "backend.h"

#include "catalogmodel.h"
#include "jobsmodel.h"
#include "markdown.h"
#include "rpcclient.h"

#include <QClipboard>
#include <QGuiApplication>
#include <QJsonArray>
#include <QJsonObject>
#include <QRegularExpression>

#include <utility>

namespace {
QString repoOf(const QVariantMap &job)
{
    return job.value(QStringLiteral("repo")).toString();
}
} // namespace

Backend::Backend(RpcClient *rpc, QObject *parent)
    : QObject(parent), m_rpc(rpc), m_catalog(new CatalogModel(rpc, this)),
      m_installed(new CatalogModel(rpc, this)), m_jobs(new JobsModel(rpc, this))
{
    m_installed->setInstalledOnly(true);
    connect(m_installed, &CatalogModel::countChanged, this, &Backend::updatesAvailableChanged);
    connect(m_installed, &QAbstractItemModel::modelReset, this, &Backend::updatesAvailableChanged);

    connect(rpc, &RpcClient::connectedChanged, this, [this] {
        emit connectedChanged();
        if (m_rpc->isConnected()) {
            loadCategories();
            reloadDetail();
            if (!m_detailRepo.isEmpty())
                loadSimilar(m_detailRepo); // detail opened before connecting (e.g. --open)
            maybeIndexOnFirstRun();
            if (!m_pendingCheck.isEmpty())
                sendAuthorCheck(std::exchange(m_pendingCheck, {}));
        }
    });
    connect(rpc, &RpcClient::notification, this, [this](const QString &method, const QJsonValue &params) {
        if (method != QLatin1String("catalog.changed"))
            return;
        loadCategories();
        const QString repo = params.toObject().value(QStringLiteral("repo")).toString();
        if (!m_detailRepo.isEmpty() && (repo.isEmpty() || repo.compare(m_detailRepo, Qt::CaseInsensitive) == 0))
            reloadDetail();
        if (!m_detailRepo.isEmpty() && repo.isEmpty())
            loadSimilar(m_detailRepo); // a new index may change the similar apps
    });
    connect(m_jobs, &JobsModel::finished, this, [this](const QVariantMap &job) {
        const QString state = job.value(QStringLiteral("state")).toString();
        const QString kind = job.value(QStringLiteral("kind")).toString();
        const QVariantMap error = job.value(QStringLiteral("error")).toMap();
        if (kind == QLatin1String("index")) {
            const QString why = state == QLatin1String("failed")
                ? friendlyError(error.value(QStringLiteral("code")).toInt(), error.value(QStringLiteral("message")).toString())
                : QString();
            if (why != m_indexError) {
                m_indexError = why;
                emit indexErrorChanged();
            }
        }
        if (state == QLatin1String("done")) {
            if (kind == QLatin1String("install"))
                emit notice(tr("%1 installed").arg(repoOf(job)));
            else if (kind == QLatin1String("update"))
                emit notice(tr("%1 updated").arg(repoOf(job)));
        } else if (state == QLatin1String("failed")) {
            emit errorOccurred(friendlyError(error.value(QStringLiteral("code")).toInt(),
                                             error.value(QStringLiteral("message")).toString()));
        }
    });
}

bool Backend::connected() const
{
    return m_rpc->isConnected();
}

int Backend::updatesAvailable() const
{
    int n = 0;
    for (int i = 0; i < m_installed->rowCount(); ++i)
        n += m_installed->data(m_installed->index(i), CatalogModel::UpdateAvailableRole).toBool();
    return n;
}

QString Backend::friendlyError(int code, const QString &message)
{
    switch (code) {
    case RpcClient::DisconnectedCode:
        return tr("No connection to omastored. Trying to reconnect…");
    case -32002:
        return tr("An operation is already running for this item.");
    case -32003:
        return tr("A file with the same name already exists and does not belong to OmaStore:\n%1").arg(message);
    case -32004:
        return tr("This app does not publish a binary for your architecture.");
    case -32005:
        return tr("GitHub request limit exhausted. Run `gh auth login` or set GITHUB_TOKEN.\n%1")
            .arg(message);
    case -32007:
        return tr("The app is already at the latest version.");
    case -32008:
        return tr("The downloaded file does not match the published checksum. Nothing was installed.");
    case -32009:
        return tr("Operation canceled.");
    case -32010:
        return tr("Some files could not be removed. They stay recorded, so removing the app again will retry.\n%1")
            .arg(message);
    }
    return message;
}

void Backend::loadCategories()
{
    m_rpc->call(QStringLiteral("catalog.categories"), {}, [this](const QJsonValue &result, const RpcError &err) {
        if (!err.ok())
            return;
        const QVariantList cats = result.toArray().toVariantList();
        if (cats != m_categories) {
            m_categories = cats;
            emit categoriesChanged();
        }
    });
}

void Backend::openDetail(const QString &repo)
{
    if (repo != m_detailRepo) {
        m_detailRepo = repo;
        m_detail.clear();
        m_similar.clear();
        emit detailChanged();
        emit similarChanged();
    }
    reloadDetail();
    loadSimilar(repo);
}

void Backend::loadSimilar(const QString &repo)
{
    m_rpc->call(QStringLiteral("catalog.similar"), {{QStringLiteral("repo"), repo}, {QStringLiteral("limit"), 6}},
                [this, repo](const QJsonValue &result, const RpcError &err) {
        if (repo != m_detailRepo || !err.ok())
            return;
        m_similar = result.toArray().toVariantList();
        emit similarChanged();
    });
}

void Backend::closeDetail()
{
    m_detailRepo.clear();
    m_detail.clear();
    m_similar.clear();
    emit similarChanged();
    m_detailLoading = false;
    emit detailChanged();
}

void Backend::reloadDetail()
{
    if (m_detailRepo.isEmpty())
        return;
    const QString repo = m_detailRepo;
    m_detailLoading = true;
    emit detailChanged();
    m_rpc->call(QStringLiteral("catalog.get"), {{QStringLiteral("repo"), repo}},
                [this, repo](const QJsonValue &result, const RpcError &err) {
        if (repo != m_detailRepo)
            return; // the user already opened another app
        m_detailLoading = false;
        if (!err.ok()) {
            emit detailChanged();
            if (err.code != RpcClient::DisconnectedCode)
                emit errorOccurred(friendlyError(err.code, err.message));
            return;
        }
        m_detail = result.toObject().toVariantMap();
        emit detailChanged();
    });
}

void Backend::startJob(const QString &method, const QString &repo)
{
    QJsonObject params;
    if (!repo.isEmpty())
        params.insert(QStringLiteral("repo"), repo);
    m_rpc->call(method, params, [this](const QJsonValue &, const RpcError &err) {
        if (!err.ok())
            emit errorOccurred(friendlyError(err.code, err.message));
    });
}

void Backend::install(const QString &repo)
{
    startJob(QStringLiteral("install.start"), repo);
}

void Backend::update(const QString &repo)
{
    startJob(QStringLiteral("update.start"), repo);
}

void Backend::updateAll()
{
    for (int i = 0; i < m_installed->rowCount(); ++i) {
        const QModelIndex idx = m_installed->index(i);
        if (m_installed->data(idx, CatalogModel::UpdateAvailableRole).toBool())
            update(m_installed->data(idx, CatalogModel::RepoRole).toString());
    }
}

void Backend::uninstall(const QString &repo)
{
    m_rpc->call(QStringLiteral("install.uninstall"), {{QStringLiteral("repo"), repo}},
                [this, repo](const QJsonValue &, const RpcError &err) {
        if (!err.ok())
            emit errorOccurred(friendlyError(err.code, err.message));
        else
            emit notice(tr("%1 removed").arg(repo));
    });
}

void Backend::refreshIndex(bool force)
{
    QJsonObject params;
    if (force)
        params.insert(QStringLiteral("force"), true);
    m_rpc->call(QStringLiteral("index.start"), params, [this](const QJsonValue &, const RpcError &err) {
        if (!err.ok() && err.code != -32002) // already indexing: nothing to do
            emit errorOccurred(friendlyError(err.code, err.message));
    });
}

void Backend::cancelJob(const QString &jobId)
{
    m_rpc->call(QStringLiteral("jobs.cancel"), {{QStringLiteral("job"), jobId}});
}

QString Backend::readmeForDisplay(const QString &markdown) const
{
    return Markdown::forDisplay(markdown);
}

// On the first run the catalog is empty: index automatically. A failed
// query is asked again on the next connection.
void Backend::maybeIndexOnFirstRun()
{
    if (m_checkedEmpty)
        return;
    m_checkedEmpty = true;
    m_rpc->call(QStringLiteral("catalog.list"), {{QStringLiteral("limit"), 1}, {QStringLiteral("all"), true}},
                [this](const QJsonValue &result, const RpcError &err) {
        if (!err.ok()) {
            m_checkedEmpty = false;
            return;
        }
        if (result.toArray().isEmpty())
            refreshIndex(false);
    });
}

QString Backend::normalizeRepo(const QString &input)
{
    QString s = input.trimmed();
    static const QRegularExpression prefix(QStringLiteral("^(?:https?://)?(?:www\\.)?github\\.com/"),
                                           QRegularExpression::CaseInsensitiveOption);
    s.remove(prefix);
    const QStringList parts = s.split(QLatin1Char('/'), Qt::SkipEmptyParts);
    if (parts.size() < 2)
        return {};
    QString repo = parts.at(1);
    if (repo.endsWith(QLatin1String(".git")))
        repo.chop(4);
    static const QRegularExpression owner(QStringLiteral("^[A-Za-z0-9][A-Za-z0-9-]*$"));
    static const QRegularExpression name(QStringLiteral("^[A-Za-z0-9._-]+$"));
    if (!owner.match(parts.at(0)).hasMatch() || !name.match(repo).hasMatch() || repo == QLatin1String(".")
        || repo == QLatin1String(".."))
        return {};
    return parts.at(0) + QLatin1Char('/') + repo;
}

void Backend::checkRepo(const QString &input, const QString &manifest)
{
    ++m_authorCheckSeq;
    m_authorCheck.clear();
    const QString repo = normalizeRepo(input);
    if (repo.isEmpty()) {
        m_authorCheckBusy = false;
        m_authorCheckError = tr("Type the repository as owner/repo or paste its GitHub URL.");
        emit authorCheckChanged();
        return;
    }
    m_authorCheckBusy = true;
    m_authorCheckError.clear();
    emit authorCheckChanged();
    QJsonObject params{{QStringLiteral("repo"), repo}};
    if (!manifest.trimmed().isEmpty())
        params.insert(QStringLiteral("manifest"), manifest);
    if (!m_rpc->isConnected()) {
        m_pendingCheck = params; // e.g. omastore-gui --check: sent once connected
        return;
    }
    sendAuthorCheck(params);
}

void Backend::sendAuthorCheck(const QJsonObject &params)
{
    const int seq = m_authorCheckSeq;
    m_rpc->call(QStringLiteral("author.check"), params, [this, seq](const QJsonValue &result, const RpcError &err) {
        if (seq != m_authorCheckSeq)
            return; // a newer check replaced this one
        m_authorCheckBusy = false;
        if (err.ok())
            m_authorCheck = result.toObject().toVariantMap();
        else
            m_authorCheckError = friendlyError(err.code, err.message);
        emit authorCheckChanged();
    });
}

void Backend::clearAuthorCheck()
{
    ++m_authorCheckSeq;
    m_pendingCheck = {};
    m_authorCheck.clear();
    m_authorCheckBusy = false;
    m_authorCheckError.clear();
    emit authorCheckChanged();
}

void Backend::copyText(const QString &text)
{
    if (qobject_cast<QGuiApplication *>(QCoreApplication::instance()))
        QGuiApplication::clipboard()->setText(text);
    emit notice(tr("Copied to the clipboard"));
}
