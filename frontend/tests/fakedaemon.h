#pragma once

// Servidor falso do omastored para os testes: responde por uma função
// configurável e permite enviar notificações.

#include <QJsonDocument>
#include <QJsonObject>
#include <QLocalServer>
#include <QLocalSocket>
#include <QPointer>
#include <QTemporaryDir>

#include <functional>

class FakeDaemon : public QObject {
    Q_OBJECT
public:
    // Recebe (method, params) e devolve {"result": ...} ou {"error": {...}};
    // um objeto vazio significa "não responder".
    std::function<QJsonObject(const QString &, const QJsonObject &)> handler;
    QList<QJsonObject> received;

    FakeDaemon()
    {
        m_path = m_dir.path() + QStringLiteral("/omastore.sock");
        connect(&m_server, &QLocalServer::newConnection, this, [this] {
            while (QLocalSocket *s = m_server.nextPendingConnection()) {
                m_client = s;
                connect(s, &QLocalSocket::readyRead, this, [this, s] {
                    while (s->canReadLine()) {
                        const QJsonObject req = QJsonDocument::fromJson(s->readLine()).object();
                        received << req;
                        if (!handler)
                            continue;
                        QJsonObject resp = handler(req.value("method").toString(), req.value("params").toObject());
                        if (resp.isEmpty())
                            continue;
                        resp.insert("jsonrpc", "2.0");
                        resp.insert("id", req.value("id"));
                        s->write(QJsonDocument(resp).toJson(QJsonDocument::Compact) + '\n');
                    }
                });
            }
        });
    }

    bool listen() { return m_server.listen(m_path); }
    void close()
    {
        if (m_client)
            m_client->abort();
        m_server.close();
    }
    QString path() const { return m_path; }
    // O cliente pode se ver conectado antes de o servidor aceitar a conexão.
    bool hasClient() const { return !m_client.isNull(); }

    void notify(const QString &method, const QJsonValue &params)
    {
        writeRaw(QJsonDocument(QJsonObject{{"jsonrpc", "2.0"}, {"method", method}, {"params", params}})
                     .toJson(QJsonDocument::Compact) + '\n');
    }
    void writeRaw(const QByteArray &bytes)
    {
        if (m_client) {
            m_client->write(bytes);
            m_client->flush();
        }
    }
    int count(const QString &method) const
    {
        int n = 0;
        for (const QJsonObject &r : received)
            n += r.value("method").toString() == method;
        return n;
    }

private:
    QTemporaryDir m_dir;
    QString m_path;
    QLocalServer m_server;
    QPointer<QLocalSocket> m_client;
};
