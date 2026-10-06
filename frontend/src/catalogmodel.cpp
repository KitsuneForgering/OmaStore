#include "catalogmodel.h"

#include "rpcclient.h"

#include <QJsonObject>

#include <algorithm>
#include <utility>

CatalogModel::CatalogModel(RpcClient *rpc, QObject *parent)
    : QAbstractListModel(parent), m_rpc(rpc)
{
    m_debounce.setSingleShot(true);
    m_debounce.setInterval(150);
    connect(&m_debounce, &QTimer::timeout, this, &CatalogModel::reload);
    connect(rpc, &RpcClient::connectedChanged, this, [this] {
        if (m_rpc->isConnected())
            reload();
    });
    connect(rpc, &RpcClient::notification, this, [this](const QString &method, const QJsonValue &) {
        if (method == QLatin1String("catalog.changed")) {
            // Same filters: keep what was scrolled through.
            m_keepCount = true;
            m_debounce.start();
        }
    });
}

int CatalogModel::rowCount(const QModelIndex &parent) const
{
    return parent.isValid() ? 0 : int(m_items.size());
}

QVariant CatalogModel::data(const QModelIndex &index, int role) const
{
    if (!index.isValid() || index.row() >= m_items.size())
        return {};
    const QJsonObject o = m_items.at(index.row()).toObject();
    switch (role) {
    case RepoRole: return o.value(QStringLiteral("repo")).toString();
    case Qt::DisplayRole:
    case NameRole: return o.value(QStringLiteral("name")).toString();
    case SummaryRole: return o.value(QStringLiteral("summary")).toString();
    case IconUrlRole: return o.value(QStringLiteral("iconUrl")).toString();
    case CategoryRole: return o.value(QStringLiteral("category")).toString();
    case StarsRole: return o.value(QStringLiteral("stars")).toInt();
    case InstallableRole: return o.value(QStringLiteral("installable")).toBool();
    case LatestVersionRole: return o.value(QStringLiteral("latestVersion")).toString();
    case InstalledVersionRole: return o.value(QStringLiteral("installedVersion")).toString();
    case UpdateAvailableRole: return o.value(QStringLiteral("updateAvailable")).toBool();
    case ScreenshotsRole: return o.value(QStringLiteral("screenshots")).toArray().toVariantList();
    }
    return {};
}

QHash<int, QByteArray> CatalogModel::roleNames() const
{
    return {
        {RepoRole, "repo"},
        {NameRole, "name"},
        {SummaryRole, "summary"},
        {IconUrlRole, "iconUrl"},
        {CategoryRole, "category"},
        {StarsRole, "stars"},
        {InstallableRole, "installable"},
        {LatestVersionRole, "latestVersion"},
        {InstalledVersionRole, "installedVersion"},
        {UpdateAvailableRole, "updateAvailable"},
        {ScreenshotsRole, "screenshots"},
    };
}

void CatalogModel::setCategory(const QString &c)
{
    if (c == m_category)
        return;
    m_category = c;
    emit categoryChanged();
    scheduleReload();
}

void CatalogModel::setQuery(const QString &q)
{
    if (q == m_query)
        return;
    m_query = q;
    emit queryChanged();
    scheduleReload();
}

void CatalogModel::setInstalledOnly(bool on)
{
    if (on == m_installedOnly)
        return;
    m_installedOnly = on;
    emit installedOnlyChanged();
    scheduleReload();
}

void CatalogModel::scheduleReload()
{
    m_keepCount = false;
    m_debounce.start();
}

QJsonObject CatalogModel::filterParams() const
{
    QJsonObject params;
    if (!m_category.isEmpty())
        params.insert(QStringLiteral("category"), m_category);
    if (!m_query.trimmed().isEmpty())
        params.insert(QStringLiteral("query"), m_query.trimmed());
    if (m_installedOnly)
        params.insert(QStringLiteral("installed"), true);
    return params;
}

void CatalogModel::reload()
{
    load(std::exchange(m_keepCount, false));
}

void CatalogModel::load(bool keepCount)
{
    m_debounce.stop();
    if (!m_rpc)
        return;
    QJsonObject params = filterParams();
    int limit = 0;
    if (m_pageSize > 0) {
        limit = m_pageSize;
        if (keepCount)
            limit = std::max(limit, int((m_items.size() + m_pageSize - 1) / m_pageSize) * m_pageSize);
        params.insert(QStringLiteral("limit"), limit);
    }
    const quint64 gen = ++m_generation;
    setLoading(true);
    m_rpc->call(QStringLiteral("catalog.list"), params,
                [this, gen, limit, guard = QPointer<CatalogModel>(this)](const QJsonValue &result, const RpcError &err) {
        if (!guard || gen != m_generation)
            return; // response to a request that was already superseded
        setLoading(false);
        if (!err.ok()) {
            setError(err.message);
            return;
        }
        setError({});
        const QJsonArray items = result.toArray();
        const bool complete = limit == 0 || items.size() < limit;
        const bool changed = complete != m_complete;
        m_complete = complete;
        setItems(items);
        if (changed)
            emit countChanged();
    });
}

bool CatalogModel::canFetchMore(const QModelIndex &parent) const
{
    return !parent.isValid() && m_pageSize > 0 && !m_complete && !m_loading && m_rpc && m_rpc->isConnected();
}

void CatalogModel::fetchMore(const QModelIndex &parent)
{
    if (!canFetchMore(parent))
        return;
    QJsonObject params = filterParams();
    params.insert(QStringLiteral("limit"), m_pageSize);
    params.insert(QStringLiteral("offset"), int(m_items.size()));
    const quint64 gen = m_generation;
    setLoading(true);
    m_rpc->call(QStringLiteral("catalog.list"), params,
                [this, gen, guard = QPointer<CatalogModel>(this)](const QJsonValue &result, const RpcError &err) {
        if (!guard || gen != m_generation)
            return; // the filters changed: a reload replaces everything
        setLoading(false);
        if (!err.ok()) {
            setError(err.message);
            return;
        }
        const QJsonArray page = result.toArray();
        if (page.size() < m_pageSize) {
            m_complete = true;
            emit countChanged();
        }
        // The catalog may have changed between pages: skip apps already shown.
        QJsonArray fresh;
        for (const QJsonValue &v : page)
            if (indexOf(v.toObject().value(QStringLiteral("repo")).toString()) < 0)
                fresh.append(v);
        if (fresh.isEmpty())
            return;
        beginInsertRows({}, int(m_items.size()), int(m_items.size() + fresh.size()) - 1);
        for (const QJsonValue &v : fresh)
            m_items.append(v);
        endInsertRows();
        emit countChanged();
    });
}

void CatalogModel::setItems(const QJsonArray &items)
{
    const int before = int(m_items.size());
    beginResetModel();
    m_items = items;
    endResetModel();
    if (before != m_items.size())
        emit countChanged();
}

QVariantMap CatalogModel::get(int row) const
{
    if (row < 0 || row >= m_items.size())
        return {};
    return m_items.at(row).toObject().toVariantMap();
}

int CatalogModel::indexOf(const QString &repo) const
{
    for (int i = 0; i < m_items.size(); ++i) {
        if (m_items.at(i).toObject().value(QStringLiteral("repo")).toString() == repo)
            return i;
    }
    return -1;
}

void CatalogModel::setLoading(bool on)
{
    if (on != m_loading) {
        m_loading = on;
        emit loadingChanged();
    }
}

void CatalogModel::setError(const QString &e)
{
    if (e != m_error) {
        m_error = e;
        emit errorChanged();
    }
}
