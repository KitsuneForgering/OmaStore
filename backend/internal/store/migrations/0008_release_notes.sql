-- Body (markdown) of the latest release, capped by the indexer; '' if none.
ALTER TABLE repos ADD COLUMN release_notes TEXT NOT NULL DEFAULT '';
