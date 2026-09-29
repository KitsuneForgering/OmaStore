#pragma once

#include <QAbstractListModel>
#include <QJsonObject>
#include <QList>

class RpcClient;

// Jobs em andamento e recém-concluídos, mantidos pelas notificações
// job.started/progress/done/failed (e por jobs.list ao conectar).
class JobsModel : public QAbstractListModel {
    Q_OBJECT
    Q_PROPERTY(int count READ rowCount NOTIFY countChanged)
    Q_PROPERTY(int runningCount READ runningCount NOTIFY runningCountChanged)
    // Incrementado a cada mudança; permite bindings em QML reavaliarem forRepo().
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
        ProgressRole, // 0..1, ou -1 se indeterminado
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

    // Job em andamento do repo (vazio se não houver).
    Q_INVOKABLE QVariantMap forRepo(const QString &repo) const;
    // Job de índice em andamento (vazio se não houver).
    Q_INVOKABLE QVariantMap indexJob() const;
    // Remove da lista os jobs concluídos.
    Q_INVOKABLE void clearFinished();

    // Aplica o estado de um job (público para testes).
    void upsert(const QJsonObject &job);

    static double progressOf(const QJsonObject &job);

signals:
    void countChanged();
    void runningCountChanged();
    void revisionChanged();
    // Emitido quando um job termina (state = done/failed/canceled).
    void finished(const QVariantMap &job);

private:
    int find(const QString &id) const;
    void bump();

    QList<QJsonObject> m_jobs;
    int m_revision = 0;
};
