#include "imageprovider.h"

#include "rpcclient.h"

#include <QImageReader>
#include <QJsonObject>
#include <QMetaObject>
#include <QUrl>

namespace {
class Response : public QQuickImageResponse {
public:
    QQuickTextureFactory *textureFactory() const override
    {
        return QQuickTextureFactory::textureFactoryForImage(m_image);
    }
    QString errorString() const override { return m_error; }

    void finish(QImage image, QString error)
    {
        m_image = std::move(image);
        m_error = std::move(error);
        emit finished();
    }

private:
    QImage m_image;
    QString m_error;
};
} // namespace

DaemonImageProvider::DaemonImageProvider(RpcClient *rpc) : m_rpc(rpc) {}

QImage DaemonImageProvider::load(const QString &path, const QSize &requestedSize)
{
    QImageReader reader(path);
    reader.setAutoTransform(true);
    if (requestedSize.isValid() && requestedSize.width() > 0 && requestedSize.height() > 0) {
        QSize size = reader.size();
        if (size.isValid())
            size.scale(requestedSize, Qt::KeepAspectRatio);
        else
            size = requestedSize;
        reader.setScaledSize(size);
    }
    return reader.read();
}

QQuickImageResponse *DaemonImageProvider::requestImageResponse(const QString &id, const QSize &requestedSize)
{
    auto *resp = new Response;
    const QString url = QUrl::fromPercentEncoding(id.toUtf8());
    QPointer<Response> guard(resp);
    QPointer<RpcClient> rpc = m_rpc;
    if (!rpc || !url.startsWith(QLatin1String("https://"))) {
        QMetaObject::invokeMethod(resp, [guard] {
            if (guard)
                guard->finish({}, QStringLiteral("invalid image URL"));
        }, Qt::QueuedConnection);
        return resp;
    }
    // The request may come from another thread; the RPC client lives in the GUI thread.
    QMetaObject::invokeMethod(rpc, [rpc, url, requestedSize, guard] {
        rpc->call(QStringLiteral("image.get"), {{QStringLiteral("url"), url}},
                  [requestedSize, guard](const QJsonValue &result, const RpcError &err) {
            if (!guard)
                return;
            if (!err.ok()) {
                guard->finish({}, err.message);
                return;
            }
            const QString path = result.toObject().value(QStringLiteral("path")).toString();
            QImage img = load(path, requestedSize);
            guard->finish(img, img.isNull() ? QStringLiteral("could not read %1").arg(path) : QString());
        });
    }, Qt::QueuedConnection);
    return resp;
}
