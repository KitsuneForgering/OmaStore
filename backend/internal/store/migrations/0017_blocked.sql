-- Apps OmaStore's maintainers blocked (catalog/blocklist.txt in the OmaStore
-- repository): hidden from the catalog, never installed or updated.
CREATE TABLE blocked (
    full_name TEXT PRIMARY KEY COLLATE NOCASE,
    reason    TEXT NOT NULL DEFAULT ''
);
