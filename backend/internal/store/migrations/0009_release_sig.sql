-- Fingerprint of the latest release (tag + name, size and digest of every
-- asset), compared by the cache check: a file replaced or added under the
-- same tag changes it. '' until the repository is indexed again.
ALTER TABLE repos ADD COLUMN release_sig TEXT NOT NULL DEFAULT '';
