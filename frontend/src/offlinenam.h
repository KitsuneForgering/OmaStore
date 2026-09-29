#pragma once

#include <QQmlNetworkAccessManagerFactory>

// Fábrica de QNetworkAccessManager para o QML engine que só permite
// recursos locais (file:, qrc:, data:). O frontend não acessa a rede: tudo
// que é remoto passa pelo daemon (image.get).
class OfflineNamFactory : public QQmlNetworkAccessManagerFactory {
public:
    QNetworkAccessManager *create(QObject *parent) override;
};

// Diz se uma URL é local (permitida).
bool isLocalUrl(const QUrl &url);
