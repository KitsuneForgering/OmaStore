#include "rpcclient.h"

#include <QCoreApplication>
#include <QDateTime>
#include <QDir>
#include <QFileInfo>
#include <QJsonDocument>
#include <QProcess>
#include <QStandardPaths>

namespace {
// Limit of a received line (the daemon has the same limit in the other direction).
constexpr qint64 MaxLine = 16 * 1024 * 1024;
}

RpcClient::RpcClient(QString socketPath, QObject *parent)
    : QObject(parent), m_path(std::move(socketPath))
{
    m_reconnect.setInterval(1000);
    m_reconnect.setSingleShot(true);
    connect(&m_reconnect, &QTimer::timeout, this, &RpcClient::tryConnect);
    connect(&m_socket, &QLocalSocket::connected, this, [this] { emit connectedChanged(); });
    connect(&m_socket, &QLocalSocket::disconnected, this, &RpcClient::onDisconnected);
    connect(&m_socket, &QLocalSocket::readyRead, this, &RpcClient::onReadyRead);
    connect(&m_socket, &QLocalSocket::errorOccurred, this, [this](QLocalSocket::LocalSocketError err) {
        if (m_socket.state() == QLocalSocket::ConnectedState)
            return;
        // Daemon missing: try to start it, at most every 10 s.
        const bool absent = err == QLocalSocket::ServerNotFoundError
            || err == QLocalSocket::ConnectionRefusedError;
        const qint64 now = QDateTime::currentMSecsSinceEpoch();
        if (absent && m_autoStart && now - m_lastSpawn > 10000) {
            m_lastSpawn = now;
            const QString daemon = findDaemon();
            if (!daemon.isEmpty())
                QProcess::startDetached(daemon, {});
        }
        if (m_running && !m_reconnect.isActive())
            m_reconnect.start();
    });
}

// The QLocalSocket (a member) emits disconnected() in its own destructor, when
// the other members (m_pending) have already been destroyed. We disconnect the
// signals first; pending callbacks are dropped without being called, since
// whoever registered them may no longer exist.
RpcClient::~RpcClient()
{
    m_running = false;
    m_reconnect.stop();
    disconnect(&m_socket, nullptr, this, nullptr);
    m_pending.clear();
    m_socket.abort();
}

QString RpcClient::defaultSocketPath()
{
    QString dir = QStandardPaths::writableLocation(QStandardPaths::RuntimeLocation);
    if (dir.isEmpty())
        dir = QDir::tempPath();
    return dir + QStringLiteral("/omastore.sock");
}

QString RpcClient::findDaemon()
{
    QStringList candidates;
    const QString env = qEnvironmentVariable("OMASTORED");
    if (!env.isEmpty())
        candidates << env;
    const QString appDir = QCoreApplication::applicationDirPath();
    candidates << appDir + QStringLiteral("/omastored")
               << appDir + QStringLiteral("/../../bin/omastored") // development tree
               << QStringLiteral("/usr/bin/omastored")
               << QStringLiteral("/usr/local/bin/omastored");
    for (const QString &c : candidates) {
        QFileInfo fi(c);
        if (fi.isAbsolute() && fi.isFile() && fi.isExecutable())
            return fi.canonicalFilePath();
    }
    return {};
}

bool RpcClient::isConnected() const
{
    return m_socket.state() == QLocalSocket::ConnectedState;
}

void RpcClient::start()
{
    m_running = true;
    tryConnect();
}

void RpcClient::stop()
{
    m_running = false;
    m_reconnect.stop();
    m_socket.abort();
}

void RpcClient::tryConnect()
{
    if (!m_running || m_socket.state() != QLocalSocket::UnconnectedState)
        return;
    m_socket.connectToServer(m_path);
}

qint64 RpcClient::call(const QString &method, const QJsonObject &params, Callback cb)
{
    const qint64 id = ++m_seq;
    if (!isConnected()) {
        if (cb) {
            // Asynchronous response, as a real response would be.
            QMetaObject::invokeMethod(this, [cb] {
                cb({}, {DisconnectedCode, QStringLiteral("no connection to omastored")});
            }, Qt::QueuedConnection);
        }
        return id;
    }
    QJsonObject req{
        {QStringLiteral("jsonrpc"), QStringLiteral("2.0")},
        {QStringLiteral("id"), id},
        {QStringLiteral("method"), method},
        {QStringLiteral("params"), params},
    };
    if (cb)
        m_pending.insert(id, std::move(cb));
    QByteArray line = QJsonDocument(req).toJson(QJsonDocument::Compact);
    line.append('\n');
    m_socket.write(line);
    return id;
}

void RpcClient::onReadyRead()
{
    while (m_socket.canReadLine()) {
        const QByteArray line = m_socket.readLine(MaxLine).trimmed();
        if (!line.isEmpty())
            handleLine(line);
    }
    // A line without '\n' larger than the limit: the stream is corrupted.
    if (m_socket.bytesAvailable() > MaxLine)
        m_socket.abort();
}

void RpcClient::handleLine(const QByteArray &line)
{
    QJsonParseError perr;
    const QJsonDocument doc = QJsonDocument::fromJson(line, &perr);
    if (perr.error != QJsonParseError::NoError || !doc.isObject()) {
        qWarning("omastored sent invalid JSON: %s", qPrintable(perr.errorString()));
        return;
    }
    const QJsonObject msg = doc.object();
    if (!msg.contains(QStringLiteral("id"))) {
        emit notification(msg.value(QStringLiteral("method")).toString(),
                          msg.value(QStringLiteral("params")));
        return;
    }
    const qint64 id = msg.value(QStringLiteral("id")).toInteger(-1);
    Callback cb = m_pending.take(id);
    if (!cb)
        return;
    RpcError err;
    if (msg.contains(QStringLiteral("error"))) {
        const QJsonObject e = msg.value(QStringLiteral("error")).toObject();
        err.code = e.value(QStringLiteral("code")).toInt(-32603);
        err.message = e.value(QStringLiteral("message")).toString();
        if (err.code == 0)
            err.code = -32603;
    }
    cb(msg.value(QStringLiteral("result")), err);
}

void RpcClient::failPending(const QString &why)
{
    const auto pending = std::exchange(m_pending, {});
    for (const Callback &cb : pending)
        cb({}, {DisconnectedCode, why});
}

void RpcClient::onDisconnected()
{
    failPending(QStringLiteral("connection to omastored lost"));
    emit connectedChanged();
    if (m_running)
        m_reconnect.start();
}
