-- Manifesto omastore.toml do repositório, já validado, em JSON ('' se não houver).
ALTER TABLE apps ADD COLUMN manifest TEXT NOT NULL DEFAULT '';
