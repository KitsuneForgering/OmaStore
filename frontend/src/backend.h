#pragma once

#include "catalogmodel.h"
#include "jobsmodel.h"

#include <QHash>
#include <QJsonObject>
#include <QObject>
#include <QVariantList>
#include <QVariantMap>

class RpcClient;

// Facade exposed to QML: models, detail of the selected app, categories and
// actions (install, update, remove, index). All the logic lives in the
// daemon; here there are only RPC calls and presentation state.
class Backend : public QObject {
    Q_OBJECT
    Q_PROPERTY(bool connected READ connected NOTIFY connectedChanged)
    Q_PROPERTY(CatalogModel *catalog READ catalog CONSTANT)
    Q_PROPERTY(CatalogModel *installed READ installed CONSTANT)
    Q_PROPERTY(JobsModel *jobs READ jobs CONSTANT)
    Q_PROPERTY(QVariantList categories READ categories NOTIFY categoriesChanged)
    Q_PROPERTY(QVariantMap detail READ detail NOTIFY detailChanged)
    Q_PROPERTY(bool detailLoading READ detailLoading NOTIFY detailChanged)
    // Why the last install, update or dependency job of the open app failed
    // ("" if it did not, or it succeeded since).
    Q_PROPERTY(QString detailFailure READ detailFailure NOTIFY detailChanged)
    // The open app is being removed (install.uninstall is synchronous).
    Q_PROPERTY(bool detailRemoving READ detailRemoving NOTIFY detailChanged)
    Q_PROPERTY(QVariantList similar READ similar NOTIFY similarChanged)
    Q_PROPERTY(int updatesAvailable READ updatesAvailable NOTIFY updatesAvailableChanged)
    // Compatibility report of the last author.check (Publish page).
    Q_PROPERTY(QVariantMap authorCheck READ authorCheck NOTIFY authorCheckChanged)
    Q_PROPERTY(bool authorCheckBusy READ authorCheckBusy NOTIFY authorCheckChanged)
    Q_PROPERTY(QString authorCheckError READ authorCheckError NOTIFY authorCheckChanged)
    // Why the last catalog refresh failed ("" after a successful one).
    Q_PROPERTY(QString indexError READ indexError NOTIFY indexErrorChanged)
    // Whether the GitHub user starred the open app: StarYes, StarNo or
    // StarUnknown (not loaded yet, or it could not be checked: starHint says why).
    Q_PROPERTY(int starState READ starState NOTIFY starChanged)
    Q_PROPERTY(bool starBusy READ starBusy NOTIFY starChanged)
    Q_PROPERTY(QString starHint READ starHint NOTIFY starChanged)
    // System dependencies of the open app (deps.check; see docs/ipc.md).
    Q_PROPERTY(QVariantMap deps READ deps NOTIFY depsChanged)
    // OmaStore's own update (self.status): mode, version, latest,
    // updateAvailable, notes.
    Q_PROPERTY(QVariantMap selfStatus READ selfStatus NOTIFY selfChanged)
    // Version installed by a finished self-update, waiting for a restart ("" if none).
    Q_PROPERTY(QString selfInstalled READ selfInstalled NOTIFY selfChanged)
    // Why the running omastored does not match this interface (daemon.hello:
    // an older protocol or missing methods); "" when it does.
    Q_PROPERTY(QString daemonWarning READ daemonWarning NOTIFY daemonChanged)
    // The running daemon can be restarted from here (it has self.restart).
    Q_PROPERTY(bool canRestartDaemon READ canRestartDaemon NOTIFY daemonChanged)
    // Automatic updates (settings.*): on by default. settingsAvailable is
    // false until loaded, or with a daemon older than settings.
    Q_PROPERTY(bool autoUpdate READ autoUpdate NOTIFY settingsChanged)
    Q_PROPERTY(bool settingsAvailable READ settingsAvailable NOTIFY settingsChanged)
    Q_PROPERTY(bool settingsBusy READ settingsBusy NOTIFY settingsChanged)

public:
    enum StarState { StarUnknown = -1, StarNo = 0, StarYes = 1 };
    Q_ENUM(StarState)

    explicit Backend(RpcClient *rpc, QObject *parent = nullptr);

    bool connected() const;
    CatalogModel *catalog() const { return m_catalog; }
    CatalogModel *installed() const { return m_installed; }
    JobsModel *jobs() const { return m_jobs; }
    QVariantList categories() const { return m_categories; }
    QVariantMap detail() const { return m_detail; }
    bool detailLoading() const { return m_detailLoading; }
    QString detailFailure() const;
    bool detailRemoving() const { return !m_removing.isEmpty() && m_removing.compare(m_detailRepo, Qt::CaseInsensitive) == 0; }
    QVariantList similar() const { return m_similar; }
    int updatesAvailable() const;
    QVariantMap authorCheck() const { return m_authorCheck; }
    bool authorCheckBusy() const { return m_authorCheckBusy; }
    QString authorCheckError() const { return m_authorCheckError; }
    QString indexError() const { return m_indexError; }
    int starState() const { return m_starState; }
    bool starBusy() const { return m_starBusy; }
    QString starHint() const { return m_starHint; }
    QVariantMap deps() const { return m_deps; }
    QVariantMap selfStatus() const { return m_selfStatus; }
    QString selfInstalled() const { return m_selfInstalled; }
    QString daemonWarning() const { return m_daemonWarning; }
    bool canRestartDaemon() const { return m_canRestartDaemon; }
    bool autoUpdate() const { return m_autoUpdate; }
    bool settingsAvailable() const { return m_settingsAvailable; }
    bool settingsBusy() const { return m_settingsBusy; }
    // The protocol this interface was built for (docs/ipc.md) and the methods
    // it needs from the daemon.
    static constexpr int Protocol = 3;
    static const QStringList &requiredMethods();
    // daemonWarning for a daemon.hello result ("" when it fits).
    static QString helloWarning(const QJsonObject &hello);

    Q_INVOKABLE void openDetail(const QString &repo);
    Q_INVOKABLE void closeDetail();
    // allowUnverified: the user confirmed a file with no checksum (the daemon
    // refuses it otherwise, -32017).
    Q_INVOKABLE void install(const QString &repo, bool allowUnverified = false);
    Q_INVOKABLE void update(const QString &repo, bool allowUnverified = false);
    // force: remove it even while it runs (after the user confirmed).
    Q_INVOKABLE void uninstall(const QString &repo, bool force = false);
    // Back to the version the last update replaced (still on disk).
    Q_INVOKABLE void rollback(const QString &repo);
    Q_INVOKABLE void updateAll();
    Q_INVOKABLE void refreshIndex(bool force = false);
    Q_INVOKABLE void cancelJob(const QString &jobId);
    // Stars or unstars the open app's repository on GitHub.
    Q_INVOKABLE void toggleStar();
    // Installs the app's missing system dependencies (the daemon asks for
    // the administrator password through polkit).
    Q_INVOKABLE void installDeps(const QString &repo);
    // OmaStore updating itself (only installations made by install.sh).
    Q_INVOKABLE void checkSelf();
    Q_INVOKABLE void updateSelf();
    // After a self-update: stops the daemon and emits restartReady, so the
    // new version of the interface (and of the daemon) takes over.
    Q_INVOKABLE void restartSelf();
    // Stops the running (older) daemon; the client starts the one that
    // belongs to this interface when it reconnects.
    Q_INVOKABLE void restartDaemon();
    Q_INVOKABLE void setAutoUpdate(bool on);
    // What a job is doing, for people: "Downloading", "Checking GitHub"…
    Q_INVOKABLE static QString stageText(const QString &kind, const QString &stage);
    // README markdown ready to display (without remote images).
    Q_INVOKABLE QString readmeForDisplay(const QString &markdown) const;
    // Asks the daemon what the store sees of a repository (owner/repo or a
    // GitHub URL). With testManifest, manifest is tested instead of the
    // published one, even when empty (an empty omastore.toml is a valid opt-in).
    Q_INVOKABLE void checkRepo(const QString &repo, const QString &manifest = {}, bool testManifest = false);
    Q_INVOKABLE void clearAuthorCheck();
    Q_INVOKABLE void copyText(const QString &text);
    // Opens the installed app through its .desktop file (gtk-launch). Never
    // part of an installation: only when the user asks.
    Q_INVOKABLE void launch();
    // GitHub "new issue" link for the open app, prefilled with detailFailure
    // and the environment; the user reviews it in the browser before sending.
    Q_INVOKABLE QString issueUrl() const;

    // The issue link from an allow-list of fields: repo, versions, the release
    // files for arch, the store version and the error ($HOME shown as ~).
    static QString buildIssueUrl(const QVariantMap &detail, const QString &failure, const QString &arch,
                                 const QString &storeVersion, const QString &home);

    // "owner/repo" from what a user typed or pasted (URL, .git, spaces);
    // "" if it is not a repository name.
    static QString normalizeRepo(const QString &input);

    // Friendly message for a daemon error code (docs/ipc.md).
    static QString friendlyError(int code, const QString &message);

signals:
    void connectedChanged();
    void categoriesChanged();
    void detailChanged();
    void similarChanged();
    void updatesAvailableChanged();
    void authorCheckChanged();
    void indexErrorChanged();
    void starChanged();
    void depsChanged();
    void selfChanged();
    void daemonChanged();
    void settingsChanged();
    // The daemon stopped for the restart: start gui (the updated interface) and quit.
    void restartReady(const QString &gui);
    // An app was installed and has missing system dependencies that pacman
    // can install: offer to install them.
    void depsSuggested(const QString &repo, const QStringList &packages);
    // Error to show to the user.
    void errorOccurred(const QString &message);
    // Removing repo was refused because it is running (processes: who).
    void removeRefusedInUse(const QString &repo, const QString &processes);
    // Informational notice (e.g. installation finished).
    void notice(const QString &message);

private:
    void loadSettings();
    void applySettings(const QJsonValue &result);
    void loadCategories();
    void checkDaemon();
    void reloadDetail();
    void loadSimilar(const QString &repo);
    void startJob(const QString &method, const QString &repo, QJsonObject params = {});
    void maybeIndexOnFirstRun();
    void sendAuthorCheck(const QJsonObject &params);
    void loadStar(const QString &repo);
    void loadDeps(const QString &repo, bool suggest = false);
    void setStar(int state, bool busy, const QString &hint);

    RpcClient *m_rpc;
    CatalogModel *m_catalog;
    CatalogModel *m_installed;
    JobsModel *m_jobs;
    QVariantList m_categories;
    QString m_detailRepo;
    QVariantMap m_detail;
    QVariantList m_similar;
    bool m_detailLoading = false;
    bool m_checkedEmpty = false;
    QVariantMap m_authorCheck;
    bool m_authorCheckBusy = false;
    QString m_authorCheckError;
    int m_authorCheckSeq = 0;
    QJsonObject m_pendingCheck; // author.check asked for before connecting
    QString m_indexError;
    int m_starState = StarUnknown;
    bool m_starBusy = false;
    QString m_starHint;
    QString m_removing; // repo being uninstalled
    // Generation of the latest request of each kind: an older answer for the
    // same app (a reload overtaken by another) is dropped.
    int m_detailSeq = 0;
    int m_similarSeq = 0;
    int m_depsSeq = 0;
    QVariantMap m_deps;
    QHash<QString, QString> m_failures; // lowercase repo → last job error
    QVariantMap m_selfStatus;
    QString m_selfInstalled;
    QString m_selfGui; // launcher of the updated interface
    QString m_daemonWarning;
    bool m_canRestartDaemon = false;
    bool m_autoUpdate = true;
    bool m_settingsAvailable = false;
    bool m_settingsBusy = false;
};
