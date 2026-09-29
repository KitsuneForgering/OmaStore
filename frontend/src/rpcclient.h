#pragma once

#include <QHash>
#include <QJsonObject>
#include <QJsonValue>
#include <QLocalSocket>
#include <QObject>
#include <QTimer>

#include <functional>

// Error of a JSON-RPC call. code == 0 means success.
struct RpcError {
    int code = 0;
    QString message;
    bool ok() const { return code == 0; }
};

// JSON-RPC 2.0 client for omastored over QLocalSocket (one message per
// line). Reconnects on its own and, if the daemon is not running, tries to
// start it. See docs/ipc.md.
class RpcClient : public QObject {
    Q_OBJECT
    Q_PROPERTY(bool connected READ isConnected NOTIFY connectedChanged)

public:
    using Callback = std::function<void(const QJsonValue &result, const RpcError &error)>;

    // Code used when the connection drops with pending calls.
    static constexpr int DisconnectedCode = -1;

    explicit RpcClient(QString socketPath = defaultSocketPath(), QObject *parent = nullptr);
    ~RpcClient() override;

    static QString defaultSocketPath();
    // Where to look for the daemon. Never uses PATH, which includes ~/.local/bin
    // (where the apps installed by the store live).
    static QString findDaemon();

    bool isConnected() const;
    QString socketPath() const { return m_path; }

    // Turns the attempt to start the daemon on/off (off in tests).
    void setAutoStart(bool on) { m_autoStart = on; }
    void setReconnectInterval(int ms) { m_reconnect.setInterval(ms); }

    // Starts the connection (and the automatic reconnections).
    void start();
    void stop();

    // Sends a call; cb is called exactly once (with error
    // DisconnectedCode if the connection drops before the response).
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
