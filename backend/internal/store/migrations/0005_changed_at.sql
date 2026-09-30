-- When a repository's stored data last changed. indexed_at also moves when a
-- run only confirms that nothing changed, so it cannot tell clients (and the
-- search index) that the catalog changed.
ALTER TABLE repos ADD COLUMN changed_at DATETIME;
UPDATE repos SET changed_at = indexed_at;
