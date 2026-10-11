-- The catalog groups the three freedesktop media main categories together.
-- Keep the original categories in the manifest for .desktop generation.
UPDATE apps SET category = 'AudioVideo' WHERE category IN ('Audio', 'Video');
