-- Durable history of version changes, retained independently of the current install row.
CREATE TABLE install_history (
    id           INTEGER PRIMARY KEY,
    full_name    TEXT NOT NULL,
    action       TEXT NOT NULL,
    from_version TEXT NOT NULL DEFAULT '',
    to_version   TEXT NOT NULL DEFAULT '',
    occurred_at  DATETIME NOT NULL
);

CREATE INDEX install_history_repo ON install_history(full_name COLLATE NOCASE, id DESC);
