#include "backend.h"

#include "catalogmodel.h"
#include "jobsmodel.h"
#include "markdown.h"
#include "rpcclient.h"

#include <QJsonArray>
#include <QJsonObject>

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
                loadSimilar(m_detailRepo); // detalhe aberto antes de conectar (ex.: --open)
            maybeIndexOnFirstRun();
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
            loadSimilar(m_detailRepo); // índice novo pode mudar os parecidos
    });
    connect(m_jobs, &JobsModel::finished, this, [this](const QVariantMap &job) {
        const QString state = job.value(QStringLiteral("state")).toString();
        const QString kind = job.value(QStringLiteral("kind")).toString();
        const QVariantMap error = job.value(QStringLiteral("error")).toMap();
        if (state == QLatin1String("done")) {
            if (kind == QLatin1String("install"))
                emit notice(tr("%1 instalado").arg(repoOf(job)));
            else if (kind == QLatin1String("update"))
                emit notice(tr("%1 atualizado").arg(repoOf(job)));
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
        return tr("Sem conexão com o omastored. Tentando reconectar…");
    case -32002:
        return tr("Já existe uma operação em andamento para este item.");
    case -32003:
        return tr("Um arquivo com o mesmo nome já existe e não pertence ao OmaStore:\n%1").arg(message);
    case -32004:
        return tr("Este app não publica um binário para a sua arquitetura.");
    case -32005:
        return tr("Limite de requisições do GitHub esgotado. Faça `gh auth login` ou defina GITHUB_TOKEN.\n%1")
            .arg(message);
    case -32007:
        return tr("O app já está na versão mais recente.");
    case -32008:
        return tr("O arquivo baixado não confere com o checksum publicado. Nada foi instalado.");
    case -32009:
        return tr("Operação cancelada.");
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
            return; // o usuário já abriu outro app
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
            emit notice(tr("%1 removido").arg(repo));
    });
}

void Backend::refreshIndex(bool force)
{
    QJsonObject params;
    if (force)
        params.insert(QStringLiteral("force"), true);
    m_rpc->call(QStringLiteral("index.start"), params, [this](const QJsonValue &, const RpcError &err) {
        if (!err.ok() && err.code != -32002) // já indexando: nada a fazer
            emit errorOccurred(friendlyError(err.code, err.message));
    });
}

void Backend::cancelJob(const QString &jobId)
{
    m_rpc->call(QStringLiteral("jobs.cancel"), {{QStringLiteral("job"), jobId}});
}

QString Backend::readmeForDisplay(const QString &markdown) const
{
    return Markdown::stripImages(markdown);
}

// Na primeira execução o catálogo está vazio: indexa automaticamente.
void Backend::maybeIndexOnFirstRun()
{
    if (m_checkedEmpty)
        return;
    m_checkedEmpty = true;
    m_rpc->call(QStringLiteral("catalog.list"), {{QStringLiteral("limit"), 1}, {QStringLiteral("all"), true}},
                [this](const QJsonValue &result, const RpcError &err) {
        if (err.ok() && result.toArray().isEmpty())
            refreshIndex(false);
    });
}
