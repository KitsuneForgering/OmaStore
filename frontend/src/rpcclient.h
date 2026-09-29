#pragma once

#include <QHash>
#include <QJsonObject>
#include <QJsonValue>
#include <QLocalSocket>
#include <QObject>
#include <QTimer>

#include <functional>

// Erro de uma chamada JSON-RPC. code == 0 significa sucesso.
struct RpcError {
    int code = 0;
    QString message;
    bool ok() const { return code == 0; }
};

// Cliente JSON-RPC 2.0 do omastored sobre QLocalSocket (uma mensagem por
// linha). Reconecta sozinho e, se o daemon não estiver rodando, tenta
// iniciá-lo. Ver docs/ipc.md.
class RpcClient : public QObject {
    Q_OBJECT
    Q_PROPERTY(bool connected READ isConnected NOTIFY connectedChanged)

public:
    using Callback = std::function<void(const QJsonValue &result, const RpcError &error)>;

    // Código usado quando a conexão cai com chamadas pendentes.
    static constexpr int DisconnectedCode = -1;

    explicit RpcClient(QString socketPath = defaultSocketPath(), QObject *parent = nullptr);
    ~RpcClient() override;

    static QString defaultSocketPath();
    // Onde procurar o daemon. Nunca usa o PATH, que inclui ~/.local/bin
    // (onde ficam os apps instalados pela loja).
    static QString findDaemon();

    bool isConnected() const;
    QString socketPath() const { return m_path; }

    // Liga/desliga a tentativa de iniciar o daemon (desligado nos testes).
    void setAutoStart(bool on) { m_autoStart = on; }
    void setReconnectInterval(int ms) { m_reconnect.setInterval(ms); }

    // Inicia a conexão (e as reconexões automáticas).
    void start();
    void stop();

    // Envia uma chamada; cb é chamado exatamente uma vez (com erro
    // DisconnectedCode se a conexão cair antes da resposta).
    qint64 call(const QString &method, const QJsonObject &params = {}, Callback cb = {});

signals:
    void connectedChanged();
    void notification(const QString &method, const QJsonValue &params);

private:
    void tryConnect();
    void onReadyRead();
    void onDisconnected();
    void handleLine(const QByteArray &line);
    void failPending(const QString &why);

    QString m_path;
    QLocalSocket m_socket;
    QTimer m_reconnect;
    QHash<qint64, Callback> m_pending;
    qint64 m_seq = 0;
    bool m_running = false;
    bool m_autoStart = true;
    qint64 m_lastSpawn = 0;
};
