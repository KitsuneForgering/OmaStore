-- Verified build provenance of a release file: JSON {workflow, ref, commit}
-- from a GitHub artifact attestation, or '' when none was verified.
ALTER TABLE assets ADD COLUMN provenance TEXT NOT NULL DEFAULT '';
