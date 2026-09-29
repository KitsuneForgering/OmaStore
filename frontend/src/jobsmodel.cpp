#include "jobsmodel.h"

#include "rpcclient.h"

#include <QJsonArray>

namespace {
const QString Running = QStringLiteral("running");

QString str(const QJsonObject &o, const char *key)
{
    return o.value(QLatin1String(key)).toString();
}
} // namespace

JobsModel::JobsModel(RpcClient *rpc, QObject *parent)
    : QAbstractListModel(parent)
{
    connect(rpc, &RpcClient::notification, this, [this](const QString &method, const QJsonValue &params) {
        if (method.startsWith(QLatin1String("job.")))
            upsert(params.toObject());
    });
    // Ao (re)conectar, sincroniza com o que o daemon já está fazendo.
    connect(rpc, &RpcClient::connectedChanged, this, [this, rpc] {
        if (!rpc->isConnected())
            return;
        rpc->call(QStringLiteral("jobs.list"), {}, [this](const QJsonValue &result, const RpcError &err) {
            if (!err.ok())
                return;
            for (const QJsonValue &v : result.toArray())
                upsert(v.toObject());
        });
    });
}

int JobsModel::rowCount(const QModelIndex &parent) const
{
    return parent.isValid() ? 0 : int(m_jobs.size());
}

double JobsModel::progressOf(const QJsonObject &job)
{
    const double total = job.value(QStringLiteral("total")).toDouble();
    if (total <= 0)
        return -1;
    return qBound(0.0, job.value(QStringLiteral("done")).toDouble() / total, 1.0);
}

QVariant JobsModel::data(const QModelIndex &index, int role) const
{
    if (!index.isValid() || index.row() >= m_jobs.size())
        return {};
    const QJsonObject &j = m_jobs.at(index.row());
    switch (role) {
    case IdRole: return str(j, "id");
    case KindRole: return str(j, "kind");
    case RepoRole: return str(j, "repo");
    case StateRole: return str(j, "state");
    case StageRole: return str(j, "stage");
    case DoneRole: return j.value(QStringLiteral("done")).toDouble();
    case TotalRole: return j.value(QStringLiteral("total")).toDouble();
    case ProgressRole: return progressOf(j);
    case MessageRole: return str(j, "message");
    case ErrorRole: return j.value(QStringLiteral("error")).toObject().value(QStringLiteral("message")).toString();
    }
    return {};
}

QHash<int, QByteArray> JobsModel::roleNames() const
{
    return {
        {IdRole, "jobId"}, {KindRole, "kind"}, {RepoRole, "repo"}, {StateRole, "jobState"},
        {StageRole, "stage"}, {DoneRole, "done"}, {TotalRole, "total"}, {ProgressRole, "progress"},
        {MessageRole, "message"}, {ErrorRole, "error"},
    };
}

int JobsModel::find(const QString &id) const
{
    for (int i = 0; i < m_jobs.size(); ++i) {
        if (str(m_jobs.at(i), "id") == id)
            return i;
    }
    return -1;
}

int JobsModel::runningCount() const
{
    int n = 0;
    for (const QJsonObject &j : m_jobs)
        n += str(j, "state") == Running;
    return n;
}

void JobsModel::bump()
{
    ++m_revision;
    emit revisionChanged();
}

void JobsModel::upsert(const QJsonObject &job)
{
    const QString id = str(job, "id");
    if (id.isEmpty())
        return;
    const int running = runningCount();
    const int row = find(id);
    bool justFinished = false;
    if (row < 0) {
        beginInsertRows({}, int(m_jobs.size()), int(m_jobs.size()));
        m_jobs.append(job);
        endInsertRows();
        emit countChanged();
        justFinished = str(job, "state") != Running;
    } else {
        // Notificações podem chegar fora de ordem entre jobs.list e o fluxo
        // de eventos: um job concluído nunca volta a "running".
        const QJsonObject &old = m_jobs.at(row);
        if (str(old, "state") != Running && str(job, "state") == Running)
            return;
        justFinished = str(old, "state") == Running && str(job, "state") != Running;
        m_jobs[row] = job;
        emit dataChanged(index(row), index(row));
    }
    if (running != runningCount())
        emit runningCountChanged();
    bump();
    if (justFinished)
        emit finished(job.toVariantMap());
}

QVariantMap JobsModel::forRepo(const QString &repo) const
{
    for (const QJsonObject &j : m_jobs) {
        if (str(j, "state") == Running && str(j, "repo").compare(repo, Qt::CaseInsensitive) == 0) {
            QVariantMap m = j.toVariantMap();
            m.insert(QStringLiteral("progress"), progressOf(j));
            return m;
        }
    }
    return {};
}

QVariantMap JobsModel::indexJob() const
{
    for (const QJsonObject &j : m_jobs) {
        if (str(j, "state") == Running && str(j, "kind") == QLatin1String("index")) {
            QVariantMap m = j.toVariantMap();
            m.insert(QStringLiteral("progress"), progressOf(j));
            return m;
        }
    }
    return {};
}

void JobsModel::clearFinished()
{
    for (int i = int(m_jobs.size()) - 1; i >= 0; --i) {
        if (str(m_jobs.at(i), "state") != Running) {
            beginRemoveRows({}, i, i);
            m_jobs.removeAt(i);
            endRemoveRows();
        }
    }
    emit countChanged();
    bump();
}
