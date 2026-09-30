-- GitHub repository names are case-insensitive: lookups by a name typed by
-- the user (show, install, uninstall) compare with COLLATE NOCASE.
CREATE INDEX repos_full_name_nocase ON repos(full_name COLLATE NOCASE);
CREATE INDEX installs_full_name_nocase ON installs(full_name COLLATE NOCASE);
