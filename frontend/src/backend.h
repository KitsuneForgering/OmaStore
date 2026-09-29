#pragma once

#include "catalogmodel.h"
#include "jobsmodel.h"

#include <QObject>
#include <QVariantList>
#include <QVariantMap>

class RpcClient;

// Fachada exposta ao QML: modelos, detalhe do app selecionado, categorias e
// ações (instalar, atualizar, remover, indexar). Toda a lógica fica no
// daemon; aqui só há chamadas RPC e estado de apresentação.
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
    // Markdown do README pronto para exibir (sem imagens remotas).
    Q_INVOKABLE QString readmeForDisplay(const QString &markdown) const;

    // Mensagem amigável para um código de erro do daemon (docs/ipc.md).
    static QString friendlyError(int code, const QString &message);

signals:
    void connectedChanged();
    void categoriesChanged();
    void detailChanged();
    void similarChanged();
    void updatesAvailableChanged();
    // Erro para mostrar ao usuário.
    void errorOccurred(const QString &message);
    // Aviso informativo (ex.: instalação concluída).
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
