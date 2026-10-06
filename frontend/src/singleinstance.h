#pragma once

#include <QObject>
#include <QString>
#include <QVariantMap>

class QLocalServer;

// What the interface was asked to show: an app, an author check or a page.
struct StartRequest {
    QString open;  // owner/repo
    QString check; // owner/repo for the Publish page
    QString page;  // discover, installed or publish

    bool isEmpty() const { return open.isEmpty() && check.isEmpty() && page.isEmpty(); }
    QVariantMap toMap() const;
    static StartRequest fromMap(const QVariantMap &map);
};

// Returns the owner/repo of an omastore://owner/repo link (also
// omastore:owner/repo), or "" when arg is not a valid link.
QString repoFromLink(const QString &arg);

// One window per user: a second launch (a clicked omastore:// link, the menu)
// hands its request to the running interface and exits. The socket lives in
// $XDG_RUNTIME_DIR, which only the user can reach.
class SingleInstance : public QObject {
    Q_OBJECT
public:
    explicit SingleInstance(QString path = {}, QObject *parent = nullptr);

    // Sends req to a running instance; true means that instance took it.
    bool forward(const StartRequest &req) const;
    // Becomes the running instance; false if the socket cannot be created
    // (the interface still works, just without forwarding).
    bool listen();

signals:
    void requested(const StartRequest &req);

private:
    QString m_path;
    QLocalServer *m_server = nullptr;
};
