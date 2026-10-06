-- Ownership of lifecycle-managed integrations travels with the installation.
ALTER TABLE installs ADD COLUMN integrations TEXT NOT NULL DEFAULT '[]';
