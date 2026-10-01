-- Repositories checked recently that have no app omastore.toml. Without a
-- token each check costs requests out of 60 per hour, so the indexer skips
-- these for a while instead of checking the same repositories on every run.
CREATE TABLE not_apps (
    full_name     TEXT PRIMARY KEY COLLATE NOCASE,
    checked_at    DATETIME NOT NULL,
    index_version INTEGER NOT NULL DEFAULT 0
);
