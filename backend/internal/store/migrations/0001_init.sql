-- Initial OmaStore schema.

CREATE TABLE repos (
    full_name      TEXT PRIMARY KEY,           -- owner/repo
    description    TEXT NOT NULL DEFAULT '',
    stars          INTEGER NOT NULL DEFAULT 0,
    topics         TEXT NOT NULL DEFAULT '[]', -- JSON array
    license        TEXT NOT NULL DEFAULT '',
    html_url       TEXT NOT NULL DEFAULT '',
    default_branch TEXT NOT NULL DEFAULT '',
    pushed_at      DATETIME,
    head_sha       TEXT NOT NULL DEFAULT '',
    latest_tag     TEXT NOT NULL DEFAULT '',
    etag           TEXT NOT NULL DEFAULT '',
    indexed_at     DATETIME
);

CREATE TABLE apps (
    full_name   TEXT PRIMARY KEY REFERENCES repos(full_name) ON DELETE CASCADE,
    name        TEXT NOT NULL,
    summary     TEXT NOT NULL DEFAULT '',
    readme      TEXT NOT NULL DEFAULT '',
    icon_url    TEXT NOT NULL DEFAULT '',
    screenshots TEXT NOT NULL DEFAULT '[]',    -- JSON array of URLs
    category    TEXT NOT NULL DEFAULT 'Utility',
    score       REAL NOT NULL DEFAULT 0,
    installable INTEGER NOT NULL DEFAULT 0     -- 1 if the latest release has a Linux binary
);

CREATE INDEX apps_category_score ON apps(category, score DESC);
CREATE INDEX apps_score ON apps(score DESC);

CREATE TABLE assets (
    id           INTEGER PRIMARY KEY,
    full_name    TEXT NOT NULL REFERENCES repos(full_name) ON DELETE CASCADE,
    tag          TEXT NOT NULL,
    name         TEXT NOT NULL,
    url          TEXT NOT NULL,
    size         INTEGER NOT NULL DEFAULT 0,
    arch         TEXT NOT NULL DEFAULT '',     -- amd64, arm64 or empty
    format       TEXT NOT NULL DEFAULT '',     -- binary, tar.gz, tar.xz, zip, appimage, pkg.tar.zst
    digest       TEXT NOT NULL DEFAULT '',     -- "sha256:<hex>" reported by the GitHub API
    checksum_url TEXT NOT NULL DEFAULT '',     -- checksum asset that covers this file
    UNIQUE (full_name, tag, name)
);

CREATE INDEX assets_repo ON assets(full_name, tag);

-- No FK to repos: an installation stays uninstallable even if the repo
-- leaves the catalog.
CREATE TABLE installs (
    full_name    TEXT PRIMARY KEY,
    version      TEXT NOT NULL,
    installed_at DATETIME NOT NULL,
    exec_path    TEXT NOT NULL DEFAULT '',
    desktop_path TEXT NOT NULL DEFAULT '',
    files        TEXT NOT NULL DEFAULT '[]'    -- JSON array of created paths
);
