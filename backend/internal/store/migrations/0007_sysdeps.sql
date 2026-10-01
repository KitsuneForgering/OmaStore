-- System dependencies the app declares in its PKGBUILD/.SRCINFO, as JSON
-- (see internal/sysdeps; '' if the repository has none).
ALTER TABLE apps ADD COLUMN sysdeps TEXT NOT NULL DEFAULT '';
