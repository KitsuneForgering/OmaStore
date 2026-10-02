#pragma once

#include <QAbstractListModel>
#include <QJsonObject>
#include <QList>

class RpcClient;

// Running and recently finished jobs, kept up to date by the
// job.started/progress/done/failed notifications (and by jobs.list on connect).
class JobsModel : public QAbstractListModel {
    Q_OBJECT
    Q_PROPERTY(int count READ rowCount NOTIFY countChanged)
    Q_PROPERTY(int runningCount READ runningCount NOTIFY runningCountChanged)
    // Incremented on every change; lets QML bindings re-evaluate forRepo().
    Q_PROPERTY(int revision READ revision NOTIFY revisionChanged)

public:
    enum Role {
        IdRole = Qt::UserRole + 1,
        KindRole,
        RepoRole,
        StateRole,
        StageRole,
        DoneRole,
        TotalRole,
        ProgressRole, // 0..1, or -1 if indeterminate
        MessageRole,
        ErrorRole,
    };
    Q_ENUM(Role)

    explicit JobsModel(RpcClient *rpc, QObject *parent = nullptr);

    int rowCount(const QModelIndex &parent = {}) const override;
    QVariant data(const QModelIndex &index, int role) const override;
    QHash<int, QByteArray> roleNames() const override;

    int runningCount() const;
    int revision() const { return m_revision; }

    // Running job of the repo (empty if there is none).
    Q_INVOKABLE QVariantMap forRepo(const QString &repo) const;
    // Running index job (empty if there is none).
    Q_INVOKABLE QVariantMap indexJob() const;
    // Running self-update of OmaStore (empty if there is none).
    Q_INVOKABLE QVariantMap selfJob() const;
    // Removes the finished jobs from the list.
    Q_INVOKABLE void clearFinished();

    // Applies a job's state (public for tests).
    // history: the job comes from jobs.list. One that had already finished
    // before this interface saw it is history, not news: no finished().
    void upsert(const QJsonObject &job, bool history = false);

    static double progressOf(const QJsonObject &job);

signals:
    void countChanged();
    void runningCountChanged();
    void revisionChanged();
    // Emitted when a job finishes (state = done/failed/canceled).
    void finished(const QVariantMap &job);

private:
    QVariantMap runningOfKind(QLatin1String kind) const;
    int find(const QString &id) const;
    void bump();

    QList<QJsonObject> m_jobs;
    int m_revision = 0;
};
