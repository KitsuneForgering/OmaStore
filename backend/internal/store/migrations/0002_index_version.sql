-- Version of the indexer logic that processed each repository. When the
-- extraction/classification rules change, the indexer reprocesses repos
-- stored with a lower version even if nothing changed on GitHub.
ALTER TABLE repos ADD COLUMN index_version INTEGER NOT NULL DEFAULT 0;
