#include "backend.h"

#include "catalogmodel.h"
#include "jobsmodel.h"
#include "markdown.h"
#include "rpcclient.h"

#include <QClipboard>
#include <QDir>
#include <QFileInfo>
#include <QGuiApplication>
#include <QJsonArray>
#include <QJsonObject>
#include <QProcess>
#include <QRegularExpression>
#include <QSysInfo>
#include <QUrl>
#include <QUrlQuery>

#include <utility>

namespace {
QString repoOf(const QVariantMap &job)
{
    return job.value(QStringLiteral("repo")).toString();
}
} // namespace

Backend::Backend(RpcClient *rpc, QObject *parent)
    : QObject(parent), m_rpc(rpc), m_catalog(new CatalogModel(rpc, this)),
      m_installed(new CatalogModel(rpc, this)), m_jobs(new JobsModel(rpc, this))
{
    m_installed->setInstalledOnly(true);
    // The installed list stays whole: update counts and "Update all" read it.
    m_catalog->setPageSize(60);
    connect(m_installed, &CatalogModel::countChanged, this, &Backend::updatesAvailableChanged);
    connect(m_installed, &QAbstractItemModel::modelReset, this, &Backend::updatesAvailableChanged);

    connect(rpc, &RpcClient::connectedChanged, this, [this] {
        emit connectedChanged();
        if (m_rpc->isConnected()) {
            checkDaemon();
            loadCategories();
            loadSettings();
            reloadDetail();
            if (!m_detailRepo.isEmpty()) {
                // Detail opened before connecting (e.g. --open).
                loadSimilar(m_detailRepo);
                loadStar(m_detailRepo);
            }
            maybeIndexOnFirstRun();
            if (m_selfInstalled.isEmpty())
                checkSelf();
            if (!m_pendingCheck.isEmpty())
                sendAuthorCheck(std::exchange(m_pendingCheck, {}));
        }
    });
    connect(rpc, &RpcClient::notification, this, [this](const QString &method, const QJsonValue &params) {
        if (method != QLatin1String("catalog.changed"))
            return;
        loadCategories();
        const QString repo = params.toObject().value(QStringLiteral("repo")).toString();
        if (!m_detailRepo.isEmpty() && (repo.isEmpty() || repo.compare(m_detailRepo, Qt::CaseInsensitive) == 0))
            reloadDetail();
        if (!m_detailRepo.isEmpty() && repo.isEmpty())
            loadSimilar(m_detailRepo); // a new index may change the similar apps
    });
    connect(m_jobs, &JobsModel::finished, this, [this](const QVariantMap &job) {
        const QString state = job.value(QStringLiteral("state")).toString();
        const QString kind = job.value(QStringLiteral("kind")).toString();
        const QVariantMap error = job.value(QStringLiteral("error")).toMap();
        if (kind == QLatin1String("index")) {
            const QString why = state == QLatin1String("failed")
                ? friendlyError(error.value(QStringLiteral("code")).toInt(), error.value(QStringLiteral("message")).toString())
                : QString();
            if (why != m_indexError) {
                m_indexError = why;
                emit indexErrorChanged();
            }
        }
        if (kind == QLatin1String("self")) {
            if (state == QLatin1String("done")) {
                const QVariantMap result = job.value(QStringLiteral("result")).toMap();
                m_selfInstalled = result.value(QStringLiteral("to")).toString();
                m_selfGui = result.value(QStringLiteral("gui")).toString();
                m_selfStatus.insert(QStringLiteral("updateAvailable"), false);
                emit selfChanged();
                emit notice(tr("OmaStore %1 is installed. Restart OmaStore to use it.").arg(m_selfInstalled));
            } else if (state == QLatin1String("failed")) {
                emit errorOccurred(tr("OmaStore could not update itself; the current version keeps working.\n%1")
                                       .arg(friendlyError(error.value(QStringLiteral("code")).toInt(),
                                                          error.value(QStringLiteral("message")).toString())));
            }
            return;
        }
        if (kind == QLatin1String("deps") && repoOf(job).compare(m_detailRepo, Qt::CaseInsensitive) == 0)
            loadDeps(m_detailRepo);
        if (state == QLatin1String("canceled") && kind != QLatin1String("index")) {
            emit notice(kind == QLatin1String("update") ? tr("Update of %1 canceled; nothing changed.").arg(repoOf(job))
                        : kind == QLatin1String("deps") ? tr("Installing the dependencies of %1 was canceled.").arg(repoOf(job))
                        : tr("Installation of %1 canceled; nothing was installed.").arg(repoOf(job)));
        }
        if (kind != QLatin1String("index") && state != QLatin1String("canceled")) {
            const QString key = repoOf(job).toLower();
            const bool had = m_failures.contains(key);
            if (state == QLatin1String("failed"))
                m_failures.insert(key, error.value(QStringLiteral("message")).toString());
            else
                m_failures.remove(key);
            if ((had || m_failures.contains(key)) && key == m_detailRepo.toLower())
                emit detailChanged();
        }
        if (state == QLatin1String("done")) {
            if (kind == QLatin1String("install")) {
                emit notice(tr("%1 installed").arg(repoOf(job)));
                loadDeps(repoOf(job), true);
            } else if (kind == QLatin1String("update")) {
                emit notice(tr("%1 updated").arg(repoOf(job)));
            } else if (kind == QLatin1String("deps")) {
                emit notice(tr("System dependencies of %1 installed").arg(repoOf(job)));
            }
        } else if (state == QLatin1String("failed")) {
            emit errorOccurred(friendlyError(error.value(QStringLiteral("code")).toInt(),
                                             error.value(QStringLiteral("message")).toString()));
        }
    });
}

bool Backend::connected() const
{
    return m_rpc->isConnected();
}

int Backend::updatesAvailable() const
{
    int n = 0;
    for (int i = 0; i < m_installed->rowCount(); ++i)
        n += m_installed->data(m_installed->index(i), CatalogModel::UpdateAvailableRole).toBool();
    return n;
}

QString Backend::friendlyError(int code, const QString &message)
{
    switch (code) {
    case RpcClient::DisconnectedCode:
        return tr("No connection to omastored. Trying to reconnect…");
    case -32002:
        return tr("An operation is already running for this item.");
    case -32003:
        return tr("A file with the same name already exists and does not belong to OmaStore:\n%1").arg(message);
    case -32004:
        return tr("This app does not publish a binary for your architecture.");
    case -32005:
        return tr("GitHub request limit exhausted. Run `gh auth login` or set GITHUB_TOKEN.\n%1")
            .arg(message);
    case -32007:
        return tr("The app is already at the latest version.");
    case -32008:
        return tr("The downloaded file does not match the published checksum. Nothing was installed.");
    case -32009:
        return tr("Operation canceled.");
    case -32010:
        return tr("Some files could not be removed. They stay recorded, so removing the app again will retry.\n%1")
            .arg(message);
    case -32011:
        return tr("Starring needs a GitHub account: run `gh auth login` (or set GITHUB_TOKEN) and restart OmaStore.");
    case -32012:
        return tr("The administrator password was not given. Nothing was installed.");
    case -32013:
        return tr("System dependencies can only be installed on Arch Linux (pacman).");
    case -32014:
        return tr("Some dependencies are not in the pacman repositories (they may be in the AUR). Install them manually.");
    case -32015:
        return tr("The app is open. Close it and try again.");
    case -32016:
        return tr("There is no earlier version of this app on this computer to go back to.");
    case -32018:
        return tr("Your package database is older than the mirrors, so pacman could not download the "
                  "dependencies. Update the system (omarchy update), then try again. Nothing was installed.");
    case -32017:
        return tr("This release publishes no checksum, so OmaStore does not install it without asking. "
                  "Open the app's page to confirm.");
    }
    return message;
}

const QStringList &Backend::requiredMethods()
{
    static const QStringList methods{
        QStringLiteral("catalog.list"), QStringLiteral("catalog.get"), QStringLiteral("install.start"),
        QStringLiteral("install.uninstall"), QStringLiteral("install.rollback"), QStringLiteral("star.get"),
        QStringLiteral("deps.check"), QStringLiteral("self.status"),
    };
    return methods;
}

QString Backend::helloWarning(const QJsonObject &hello)
{
    const int protocol = hello.value(QStringLiteral("protocol")).toInt();
    if (protocol > Protocol)
        return tr("omastored is newer than this window. Close OmaStore and open it again to use the new version.");
    QStringList missing;
    const QJsonArray methods = hello.value(QStringLiteral("methods")).toArray();
    for (const QString &m : requiredMethods()) {
        if (!methods.contains(m))
            missing << m;
    }
    if (protocol < Protocol || !missing.isEmpty())
        return tr("The running omastored is older than this window (protocol %1, this window needs %2), so some "
                  "actions will fail. Restart it to use the version that came with this window.")
            .arg(protocol)
            .arg(Protocol);
    return {};
}

void Backend::checkDaemon()
{
    m_rpc->call(QStringLiteral("daemon.hello"), {}, [this](const QJsonValue &result, const RpcError &err) {
        if (!err.ok())
            return;
        const QJsonObject hello = result.toObject();
        const QString warning = helloWarning(hello);
        const bool canRestart = hello.value(QStringLiteral("methods")).toArray().contains(QStringLiteral("self.restart"));
        if (warning != m_daemonWarning || canRestart != m_canRestartDaemon) {
            m_daemonWarning = warning;
            m_canRestartDaemon = canRestart;
            emit daemonChanged();
        }
    });
}

void Backend::restartDaemon()
{
    m_rpc->call(QStringLiteral("self.restart"), {}, [this](const QJsonValue &, const RpcError &err) {
        // A dropped connection means it already left; the client reconnects
        // and starts the daemon next to this interface.
        if (!err.ok() && err.code != RpcClient::DisconnectedCode)
            emit errorOccurred(err.code == -32002
                                   ? tr("Wait for the running operations to finish, then try again.")
                                   : friendlyError(err.code, err.message));
    });
}

void Backend::applySettings(const QJsonValue &result)
{
    m_autoUpdate = result.toObject().value(QStringLiteral("autoUpdate")).toBool(true);
    m_settingsAvailable = true;
    emit settingsChanged();
}

void Backend::loadSettings()
{
    m_rpc->call(QStringLiteral("settings.get"), {}, [this](const QJsonValue &result, const RpcError &err) {
        if (!err.ok()) {
            // An older daemon: the switch stays hidden.
            if (m_settingsAvailable) {
                m_settingsAvailable = false;
                emit settingsChanged();
            }
            return;
        }
        applySettings(result);
    });
}

void Backend::setAutoUpdate(bool on)
{
    if (m_settingsBusy)
        return;
    m_settingsBusy = true;
    emit settingsChanged();
    m_rpc->call(QStringLiteral("settings.set"), {{QStringLiteral("autoUpdate"), on}},
                [this, on](const QJsonValue &result, const RpcError &err) {
        m_settingsBusy = false;
        if (!err.ok()) {
            emit settingsChanged();
            emit errorOccurred(friendlyError(err.code, err.message));
            return;
        }
        applySettings(result);
        emit notice(on ? tr("Apps now update on their own; you are told what changed.")
                       : tr("Automatic updates are off; updates wait for you here."));
    });
}

void Backend::loadCategories()
{
    m_rpc->call(QStringLiteral("catalog.categories"), {}, [this](const QJsonValue &result, const RpcError &err) {
        if (!err.ok())
            return;
        const QVariantList cats = result.toArray().toVariantList();
        if (cats != m_categories) {
            m_categories = cats;
            emit categoriesChanged();
        }
    });
}

void Backend::openDetail(const QString &repo)
{
    if (repo != m_detailRepo) {
        m_detailRepo = repo;
        m_detail.clear();
        m_similar.clear();
        m_deps.clear();
        setStar(StarUnknown, false, {});
        emit detailChanged();
        emit similarChanged();
        emit depsChanged();
    }
    reloadDetail();
    loadSimilar(repo);
    loadStar(repo);
}

void Backend::setStar(int state, bool busy, const QString &hint)
{
    if (state == m_starState && busy == m_starBusy && hint == m_starHint)
        return;
    m_starState = state;
    m_starBusy = busy;
    m_starHint = hint;
    emit starChanged();
}

void Backend::loadStar(const QString &repo)
{
    m_rpc->call(QStringLiteral("star.get"), {{QStringLiteral("repo"), repo}},
                [this, repo](const QJsonValue &result, const RpcError &err) {
        if (repo != m_detailRepo || m_starBusy)
            return;
        if (!err.ok()) {
            // Never a dead button without a reason: the tooltip says why.
            QString why;
            if (err.code == -32011)
                why = friendlyError(err.code, err.message);
            else if (err.code == -32601)
                why = tr("This omastored is older than the interface and cannot star apps. Restart OmaStore.");
            else if (err.code == RpcClient::DisconnectedCode)
                why = tr("Not connected to omastored.");
            else
                why = tr("Could not check your star on GitHub: %1").arg(friendlyError(err.code, err.message));
            setStar(StarUnknown, false, why);
            return;
        }
        setStar(result.toObject().value(QStringLiteral("starred")).toBool() ? StarYes : StarNo, false, {});
    });
}

void Backend::toggleStar()
{
    if (m_detailRepo.isEmpty() || m_starBusy || m_starState == StarUnknown)
        return;
    const QString repo = m_detailRepo;
    const bool want = m_starState != StarYes;
    setStar(m_starState, true, {});
    m_rpc->call(QStringLiteral("star.set"), {{QStringLiteral("repo"), repo}, {QStringLiteral("starred"), want}},
                [this, repo, want](const QJsonValue &result, const RpcError &err) {
        if (repo != m_detailRepo)
            return;
        if (!err.ok()) {
            setStar(m_starState, false, err.code == -32011 ? friendlyError(err.code, err.message) : QString());
            emit errorOccurred(friendlyError(err.code, err.message));
            return;
        }
        setStar(want ? StarYes : StarNo, false, {});
        m_detail.insert(QStringLiteral("stars"), result.toObject().value(QStringLiteral("stars")).toInt());
        emit detailChanged();
        emit notice(want ? tr("You starred %1 on GitHub").arg(repo) : tr("Your star on %1 was removed").arg(repo));
    });
}

void Backend::loadDeps(const QString &repo, bool suggest)
{
    const int seq = ++m_depsSeq;
    m_rpc->call(QStringLiteral("deps.check"), {{QStringLiteral("repo"), repo}},
                [this, repo, suggest, seq](const QJsonValue &result, const RpcError &err) {
        if (!err.ok())
            return;
        const QVariantMap deps = result.toObject().toVariantMap();
        if (seq == m_depsSeq && repo.compare(m_detailRepo, Qt::CaseInsensitive) == 0) {
            m_deps = deps;
            emit depsChanged();
        }
        const QStringList packages = deps.value(QStringLiteral("toInstall")).toStringList();
        if (suggest && !packages.isEmpty())
            emit depsSuggested(repo, packages);
    });
}

void Backend::installDeps(const QString &repo)
{
    startJob(QStringLiteral("deps.install"), repo);
}

void Backend::loadSimilar(const QString &repo)
{
    const int seq = ++m_similarSeq;
    m_rpc->call(QStringLiteral("catalog.similar"), {{QStringLiteral("repo"), repo}, {QStringLiteral("limit"), 6}},
                [this, repo, seq](const QJsonValue &result, const RpcError &err) {
        if (seq != m_similarSeq || repo != m_detailRepo || !err.ok())
            return;
        m_similar = result.toArray().toVariantList();
        emit similarChanged();
    });
}

void Backend::closeDetail()
{
    m_detailRepo.clear();
    m_detail.clear();
    m_similar.clear();
    m_deps.clear();
    setStar(StarUnknown, false, {});
    emit similarChanged();
    emit depsChanged();
    m_detailLoading = false;
    emit detailChanged();
}

void Backend::reloadDetail()
{
    if (m_detailRepo.isEmpty())
        return;
    const QString repo = m_detailRepo;
    const int seq = ++m_detailSeq;
    m_detailLoading = true;
    emit detailChanged();
    m_rpc->call(QStringLiteral("catalog.get"), {{QStringLiteral("repo"), repo}},
                [this, repo, seq](const QJsonValue &result, const RpcError &err) {
        if (seq != m_detailSeq || repo != m_detailRepo)
            return; // overtaken by a newer reload, or another app was opened
        m_detailLoading = false;
        if (!err.ok()) {
            emit detailChanged();
            if (err.code != RpcClient::DisconnectedCode)
                emit errorOccurred(friendlyError(err.code, err.message));
            return;
        }
        m_detail = result.toObject().toVariantMap();
        emit detailChanged();
    });
    loadDeps(repo); // an index or install may change what is declared or installed
}

void Backend::startJob(const QString &method, const QString &repo, QJsonObject params)
{
    if (!repo.isEmpty())
        params.insert(QStringLiteral("repo"), repo);
    m_rpc->call(method, params, [this](const QJsonValue &, const RpcError &err) {
        if (!err.ok())
            emit errorOccurred(friendlyError(err.code, err.message));
    });
}

void Backend::install(const QString &repo, bool allowUnverified)
{
    QJsonObject params;
    if (allowUnverified)
        params.insert(QStringLiteral("allowUnverified"), true);
    startJob(QStringLiteral("install.start"), repo, params);
}

void Backend::update(const QString &repo, bool allowUnverified)
{
    QJsonObject params;
    if (allowUnverified)
        params.insert(QStringLiteral("allowUnverified"), true);
    startJob(QStringLiteral("update.start"), repo, params);
}

void Backend::updateAll()
{
    for (int i = 0; i < m_installed->rowCount(); ++i) {
        const QModelIndex idx = m_installed->index(i);
        if (m_installed->data(idx, CatalogModel::UpdateAvailableRole).toBool())
            update(m_installed->data(idx, CatalogModel::RepoRole).toString());
    }
}

void Backend::uninstall(const QString &repo, bool force)
{
    if (!m_removing.isEmpty())
        return;
    m_removing = repo;
    emit detailChanged();
    if (m_detail.value(QStringLiteral("install")).toMap().value(QStringLiteral("services")).toList().size() > 0)
        emit notice(tr("Removing managed user services of %1").arg(repo));
    QJsonObject params{{QStringLiteral("repo"), repo}};
    if (force)
        params.insert(QStringLiteral("force"), true);
    m_rpc->call(QStringLiteral("install.uninstall"), params, [this, repo](const QJsonValue &, const RpcError &err) {
        m_removing.clear();
        emit detailChanged();
        if (err.code == -32015) {
            // "…: the app is running: name (pid), …": the processes after the last ": ".
            const QString who = err.message.section(QStringLiteral(": "), -1);
            emit removeRefusedInUse(repo, who);
            return;
        }
        if (!err.ok())
            emit errorOccurred(friendlyError(err.code, err.message));
        else
            emit notice(tr("%1 removed").arg(repo));
    });
}

void Backend::rollback(const QString &repo)
{
    m_rpc->call(QStringLiteral("install.rollback"), {{QStringLiteral("repo"), repo}},
                [this, repo](const QJsonValue &result, const RpcError &err) {
        if (!err.ok()) {
            emit errorOccurred(friendlyError(err.code, err.message));
            return;
        }
        emit notice(tr("%1 is back at %2. The newer version is still offered as an update.")
                        .arg(repo, result.toObject().value(QStringLiteral("version")).toString()));
    });
}

void Backend::refreshIndex(bool force)
{
    QJsonObject params;
    if (force)
        params.insert(QStringLiteral("force"), true);
    m_rpc->call(QStringLiteral("index.start"), params, [this](const QJsonValue &, const RpcError &err) {
        if (!err.ok() && err.code != -32002) // already indexing: nothing to do
            emit errorOccurred(friendlyError(err.code, err.message));
    });
}

void Backend::cancelJob(const QString &jobId)
{
    m_rpc->call(QStringLiteral("jobs.cancel"), {{QStringLiteral("job"), jobId}});
}

void Backend::checkSelf()
{
    m_rpc->call(QStringLiteral("self.status"), {}, [this](const QJsonValue &result, const RpcError &err) {
        if (!err.ok())
            return; // an older daemon without self.status: nothing to offer
        const QVariantMap st = result.toObject().toVariantMap();
        if (st != m_selfStatus) {
            m_selfStatus = st;
            emit selfChanged();
        }
    });
}

void Backend::updateSelf()
{
    startJob(QStringLiteral("self.update"), {});
}

void Backend::restartSelf()
{
    const QFileInfo gui(m_selfGui);
    if (m_selfInstalled.isEmpty() || !gui.isAbsolute() || gui.fileName() != QLatin1String("omastore-gui")) {
        emit errorOccurred(tr("Close and reopen OmaStore to use the new version."));
        return;
    }
    m_rpc->call(QStringLiteral("self.restart"), {}, [this](const QJsonValue &, const RpcError &err) {
        // A dropped connection means the daemon already went away.
        if (!err.ok() && err.code != RpcClient::DisconnectedCode) {
            emit errorOccurred(err.code == -32002
                                   ? tr("Wait for the running operations to finish, then restart OmaStore.")
                                   : friendlyError(err.code, err.message));
            return;
        }
        m_rpc->stop(); // no reconnection to the daemon that is leaving
        emit restartReady(m_selfGui);
    });
}

QString Backend::stageText(const QString &kind, const QString &stage)
{
    if (kind == QLatin1String("index")) {
        if (stage == QLatin1String("discover"))
            return tr("Searching GitHub");
        if (stage == QLatin1String("state"))
            return tr("Checking for changes");
        return tr("Reading apps");
    }
    if (stage == QLatin1String("download"))
        return tr("Downloading");
    if (stage == QLatin1String("verify"))
        return tr("Verifying");
    if (stage == QLatin1String("extract"))
        return tr("Extracting");
    if (stage == QLatin1String("integrate"))
        return tr("Adding to the menu");
    if (stage == QLatin1String("service"))
        return tr("Configuring user service");
    if (stage == QLatin1String("done"))
        return tr("Done");
    if (stage == QLatin1String("authorize"))
        return tr("Waiting for the administrator password");
    return tr("Preparing");
}

QString Backend::readmeForDisplay(const QString &markdown) const
{
    return Markdown::forDisplay(markdown);
}

// On the first run the catalog is empty: index automatically. A failed
// query is asked again on the next connection.
void Backend::maybeIndexOnFirstRun()
{
    if (m_checkedEmpty)
        return;
    m_checkedEmpty = true;
    m_rpc->call(QStringLiteral("catalog.list"), {{QStringLiteral("limit"), 1}, {QStringLiteral("all"), true}},
                [this](const QJsonValue &result, const RpcError &err) {
        if (!err.ok()) {
            m_checkedEmpty = false;
            return;
        }
        if (result.toArray().isEmpty())
            refreshIndex(false);
    });
}

QString Backend::normalizeRepo(const QString &input)
{
    QString s = input.trimmed();
    static const QRegularExpression prefix(QStringLiteral("^(?:https?://)?(?:www\\.)?github\\.com/"),
                                           QRegularExpression::CaseInsensitiveOption);
    s.remove(prefix);
    const QStringList parts = s.split(QLatin1Char('/'), Qt::SkipEmptyParts);
    if (parts.size() < 2)
        return {};
    QString repo = parts.at(1);
    if (repo.endsWith(QLatin1String(".git")))
        repo.chop(4);
    static const QRegularExpression owner(QStringLiteral("^[A-Za-z0-9][A-Za-z0-9-]*$"));
    static const QRegularExpression name(QStringLiteral("^[A-Za-z0-9._-]+$"));
    if (!owner.match(parts.at(0)).hasMatch() || !name.match(repo).hasMatch() || repo == QLatin1String(".")
        || repo == QLatin1String(".."))
        return {};
    return parts.at(0) + QLatin1Char('/') + repo;
}

void Backend::checkRepo(const QString &input, const QString &manifest, bool testManifest)
{
    ++m_authorCheckSeq;
    m_authorCheck.clear();
    const QString repo = normalizeRepo(input);
    if (repo.isEmpty()) {
        m_authorCheckBusy = false;
        m_authorCheckError = tr("Type the repository as owner/repo or paste its GitHub URL.");
        emit authorCheckChanged();
        return;
    }
    m_authorCheckBusy = true;
    m_authorCheckError.clear();
    emit authorCheckChanged();
    QJsonObject params{{QStringLiteral("repo"), repo}};
    if (testManifest)
        params.insert(QStringLiteral("manifest"), manifest);
    if (!m_rpc->isConnected()) {
        m_pendingCheck = params; // e.g. omastore-gui --check: sent once connected
        return;
    }
    sendAuthorCheck(params);
}

void Backend::sendAuthorCheck(const QJsonObject &params)
{
    const int seq = m_authorCheckSeq;
    m_rpc->call(QStringLiteral("author.check"), params, [this, seq](const QJsonValue &result, const RpcError &err) {
        if (seq != m_authorCheckSeq)
            return; // a newer check replaced this one
        m_authorCheckBusy = false;
        if (err.ok())
            m_authorCheck = result.toObject().toVariantMap();
        else
            m_authorCheckError = friendlyError(err.code, err.message);
        emit authorCheckChanged();
    });
}

void Backend::clearAuthorCheck()
{
    ++m_authorCheckSeq;
    m_pendingCheck = {};
    m_authorCheck.clear();
    m_authorCheckBusy = false;
    m_authorCheckError.clear();
    emit authorCheckChanged();
}

QString Backend::detailFailure() const
{
    return m_failures.value(m_detailRepo.toLower());
}

void Backend::launch()
{
    const QString desktop = m_detail.value(QStringLiteral("install")).toMap().value(QStringLiteral("desktopPath")).toString();
    const QString id = QFileInfo(desktop).completeBaseName();
    // Absolute paths: never through PATH, which holds the downloaded apps.
    const QString gtkLaunch = QStringLiteral("/usr/bin/gtk-launch");
    const QString gio = QStringLiteral("/usr/bin/gio");
    bool ok = false;
    if (id.startsWith(QLatin1String("omastore-")) && QFileInfo::exists(desktop)) {
        if (QFileInfo(gtkLaunch).isExecutable())
            ok = QProcess::startDetached(gtkLaunch, {id});
        else if (QFileInfo(gio).isExecutable())
            ok = QProcess::startDetached(gio, {QStringLiteral("launch"), desktop});
    }
    if (!ok)
        emit errorOccurred(tr("Could not open %1: its menu entry or gtk-launch is missing.").arg(m_detailRepo));
}

QString Backend::issueUrl() const
{
    return buildIssueUrl(m_detail, detailFailure(), QSysInfo::currentCpuArchitecture(),
                         QCoreApplication::applicationVersion(), QDir::homePath());
}

QString Backend::buildIssueUrl(const QVariantMap &detail, const QString &failure, const QString &arch,
                               const QString &storeVersion, const QString &home)
{
    const QString repo = normalizeRepo(detail.value(QStringLiteral("repo")).toString());
    if (repo.isEmpty())
        return {};
    // Release files are named after Go's architectures (docs/ipc.md).
    const QString goarch = arch == QLatin1String("x86_64") ? QStringLiteral("amd64")
                         : arch == QLatin1String("arm64") ? QStringLiteral("arm64") : arch;
    QStringList files;
    for (const QVariant &a : detail.value(QStringLiteral("assets")).toList()) {
        const QVariantMap m = a.toMap();
        const QString assetArch = m.value(QStringLiteral("arch")).toString();
        if (assetArch.isEmpty() || assetArch == goarch)
            files << m.value(QStringLiteral("name")).toString();
    }
    const QString installed = detail.value(QStringLiteral("install")).toMap().value(QStringLiteral("version")).toString();
    QString error = failure.simplified();
    if (!home.isEmpty() && home != QLatin1String("/"))
        error.replace(home, QStringLiteral("~"));

    QStringList body{QStringLiteral("<!-- Describe what happened and what you expected. -->"), QString(),
                     QString(), QStringLiteral("**Environment** (filled in by OmaStore; edit freely)"),
                     QStringLiteral("- App version: %1 (latest release: %2)")
                         .arg(installed.isEmpty() ? QStringLiteral("not installed") : installed,
                              detail.value(QStringLiteral("latestVersion")).toString()),
                     QStringLiteral("- Architecture: %1").arg(arch)};
    if (!files.isEmpty())
        body << QStringLiteral("- Release files for it: %1").arg(files.join(QStringLiteral(", ")));
    body << QStringLiteral("- OmaStore: %1").arg(storeVersion.isEmpty() ? QStringLiteral("unknown") : storeVersion);
    if (!error.isEmpty())
        body << QStringLiteral("- Error: `%1`").arg(error.replace(QLatin1Char('`'), QLatin1Char('\'')));

    QUrlQuery query;
    if (!error.isEmpty())
        query.addQueryItem(QStringLiteral("title"), QStringLiteral("Installing through OmaStore fails"));
    query.addQueryItem(QStringLiteral("body"), body.join(QLatin1Char('\n')));
    QUrl url(QStringLiteral("https://github.com/%1/issues/new").arg(repo));
    url.setQuery(query);
    return url.toString(QUrl::FullyEncoded);
}

void Backend::copyText(const QString &text)
{
    if (qobject_cast<QGuiApplication *>(QCoreApplication::instance()))
        QGuiApplication::clipboard()->setText(text);
    emit notice(tr("Copied to the clipboard"));
}
