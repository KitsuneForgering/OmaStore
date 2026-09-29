#pragma once

#include <QPointer>
#include <QQuickAsyncImageProvider>

class RpcClient;

// image://omastore/<url codificada>: pede a imagem ao daemon (image.get),
// que baixa e guarda em cache, e carrega o arquivo local resultante.
class DaemonImageProvider : public QQuickAsyncImageProvider {
public:
    explicit DaemonImageProvider(RpcClient *rpc);
    QQuickImageResponse *requestImageResponse(const QString &id, const QSize &requestedSize) override;

    // Carrega um arquivo local respeitando o tamanho pedido (SVG é
    // rasterizado já no tamanho final).
    static QImage load(const QString &path, const QSize &requestedSize);

private:
    QPointer<RpcClient> m_rpc;
};
