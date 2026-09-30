#include "offlinenam.h"

#include <QNetworkAccessManager>
#include <QNetworkRequest>
#include <QUrl>

bool isLocalUrl(const QUrl &url)
{
    const QString s = url.scheme().toLower();
    return s == QLatin1String("file") || s == QLatin1String("qrc") || s == QLatin1String("data") || s.isEmpty();
}

namespace {
class OfflineNam : public QNetworkAccessManager {
public:
    using QNetworkAccessManager::QNetworkAccessManager;

protected:
    QNetworkReply *createRequest(Operation op, const QNetworkRequest &req, QIODevice *data) override
    {
        if (isLocalUrl(req.url()))
            return QNetworkAccessManager::createRequest(op, req, data);
        qWarning("OmaStore: network access blocked in the frontend: %s", qPrintable(req.url().toDisplayString()));
        // An invalid URL makes the QNAM reply with an error, without touching the network.
        QNetworkRequest blocked(req);
        blocked.setUrl(QUrl(QStringLiteral("blocked:")));
        return QNetworkAccessManager::createRequest(op, blocked, data);
    }
};
} // namespace

QNetworkAccessManager *OfflineNamFactory::create(QObject *parent)
{
    return new OfflineNam(parent);
}
