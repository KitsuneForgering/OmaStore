#pragma once

#include <QQmlNetworkAccessManagerFactory>

// QNetworkAccessManager factory for the QML engine that only allows local
// resources (file:, qrc:, data:). The frontend does not access the network:
// everything remote goes through the daemon (image.get).
class OfflineNamFactory : public QQmlNetworkAccessManagerFactory {
public:
    QNetworkAccessManager *create(QObject *parent) override;
};

// Reports whether a URL is local (allowed).
bool isLocalUrl(const QUrl &url);
