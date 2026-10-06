#include "singleinstance.h"

#include <QDir>
#include <QJsonDocument>
#include <QJsonObject>
#include <QLocalServer>
#include <QLocalSocket>
#include <QRegularExpression>
#include <QStandardPaths>

namespace {
constexpr int kTimeoutMs = 1000;
constexpr qint64 kMaxRequest = 4096;

// The same rules as the daemon's internal/repoid.
bool validRepo(const QString &repo)
{
    static const QRegularExpression re(
        QStringLiteral("^[A-Za-z0-9][A-Za-z0-9-]{0,38}/[A-Za-z0-9][A-Za-z0-9._-]{0,99}$"));
    return re.match(repo).hasMatch() && !repo.endsWith(QLatin1String("/.")) && !repo.endsWith(QLatin1String("/.."));
}
} // namespace

QVariantMap StartRequest::toMap() const
{
    return {{QStringLiteral("open"), open}, {QStringLiteral("check"), check}, {QStringLiteral("page"), page}};
}

StartRequest StartRequest::fromMap(const QVariantMap &map)
{
    StartRequest r;
    r.open = map.value(QStringLiteral("open")).toString();
    r.check = map.value(QStringLiteral("check")).toString();
    r.page = map.value(QStringLiteral("page")).toString();
    return r;
}

QString repoFromLink(const QString &arg)
{
    QString rest = arg.trimmed();
    if (!rest.startsWith(QLatin1String("omastore:"), Qt::CaseInsensitive))
        return {};
    rest = rest.mid(9);
    while (rest.startsWith(QLatin1Char('/')))
        rest.remove(0, 1);
    while (rest.endsWith(QLatin1Char('/')))
        rest.chop(1);
    return validRepo(rest) ? rest : QString();
}

SingleInstance::SingleInstance(QString path, QObject *parent) : QObject(parent), m_path(std::move(path))
{
    if (m_path.isEmpty()) {
        QString dir = QStandardPaths::writableLocation(QStandardPaths::RuntimeLocation);
        if (dir.isEmpty())
            dir = QDir::tempPath();
        m_path = dir + QStringLiteral("/omastore-gui.sock");
    }
}

bool SingleInstance::forward(const StartRequest &req) const
{
    QLocalSocket socket;
    socket.connectToServer(m_path);
    if (!socket.waitForConnected(kTimeoutMs))
        return false;
    socket.write(QJsonDocument(QJsonObject::fromVariantMap(req.toMap())).toJson(QJsonDocument::Compact) + '\n');
    if (!socket.waitForBytesWritten(kTimeoutMs))
        return false;
    // The running instance answers once it has the request.
    return socket.waitForReadyRead(kTimeoutMs) && socket.readAll().startsWith("ok");
}

bool SingleInstance::listen()
{
    m_server = new QLocalServer(this);
    m_server->setSocketOptions(QLocalServer::UserAccessOption);
    if (!m_server->listen(m_path)) {
        // A socket left by a crashed instance (forward() already failed).
        QLocalServer::removeServer(m_path);
        if (!m_server->listen(m_path))
            return false;
    }
    connect(m_server, &QLocalServer::newConnection, this, [this] {
        while (QLocalSocket *socket = m_server->nextPendingConnection()) {
            connect(socket, &QLocalSocket::disconnected, socket, &QObject::deleteLater);
            connect(socket, &QLocalSocket::readyRead, socket, [this, socket] {
                if (!socket->canReadLine()) {
                    if (socket->bytesAvailable() > kMaxRequest)
                        socket->abort();
                    return;
                }
                const QJsonObject obj = QJsonDocument::fromJson(socket->readLine(kMaxRequest)).object();
                const StartRequest req = StartRequest::fromMap(obj.toVariantMap());
                socket->write("ok\n");
                socket->disconnectFromServer();
                emit requested(req);
            });
        }
    });
    return true;
}
