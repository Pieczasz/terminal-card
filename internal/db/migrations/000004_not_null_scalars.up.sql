-- Columns whose Go field is a plain scalar. A NULL scans into the zero value there, so
-- a NULL elo reads back as rating 0 and a NULL game_id as game 0 - both look like data
-- rather than like a missing value. TestSchemaNullabilityMatchesStructs derives its
-- list from the GORM structs, so a new scalar field fails CI until it is pinned here.

-- Backfills first, for the two that have a sensible resting value. The foreign keys
-- get none on purpose: a row that really has no owner is a bug worth failing on, not
-- one worth inventing a parent for.
UPDATE rankings SET elo = 1500 WHERE elo IS NULL;
ALTER TABLE rankings ALTER COLUMN elo SET NOT NULL;

UPDATE public_keys SET name = '' WHERE name IS NULL;
ALTER TABLE public_keys ALTER COLUMN name SET NOT NULL;

ALTER TABLE matches ALTER COLUMN game_id SET NOT NULL;
ALTER TABLE public_keys ALTER COLUMN user_id SET NOT NULL;
