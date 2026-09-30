-- The repository's omastore.toml manifest, already validated, as JSON ('' if absent).
ALTER TABLE apps ADD COLUMN manifest TEXT NOT NULL DEFAULT '';
