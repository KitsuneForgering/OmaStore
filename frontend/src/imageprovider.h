#pragma once

#include <QPointer>
#include <QQuickAsyncImageProvider>

class RpcClient;

// image://omastore/<encoded url>: asks the daemon for the image (image.get),
// which downloads and caches it, and loads the resulting local file.
class DaemonImageProvider : public QQuickAsyncImageProvider {
public:
    explicit DaemonImageProvider(RpcClient *rpc);
    QQuickImageResponse *requestImageResponse(const QString &id, const QSize &requestedSize) override;

    // Loads a local file honoring the requested size (SVG is
    // rasterized directly at the final size).
    static QImage load(const QString &path, const QSize &requestedSize);

private:
    QPointer<RpcClient> m_rpc;
};
