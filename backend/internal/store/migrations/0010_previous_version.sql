-- The version an update replaced: kept on disk (a running copy keeps working,
-- and install.rollback can go back to it). '' when there is none.
ALTER TABLE installs ADD COLUMN previous_version TEXT NOT NULL DEFAULT '';
ALTER TABLE installs ADD COLUMN previous_exec TEXT NOT NULL DEFAULT '';
