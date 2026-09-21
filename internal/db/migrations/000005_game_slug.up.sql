-- games.name was the persisted identity of a game, so renaming one in internal/catalog
-- would have created a second games row and orphaned every ranking on the first. The
-- slug is the catalog's own stable key (catalog.Entry.Slug); name becomes a display
-- column that each finalize refreshes in place.
ALTER TABLE games ADD COLUMN slug TEXT;

-- Backfill the five games that exist. Anything else came from a test or a hand-written
-- row, and the derived form is good enough to keep it unique and legible.
UPDATE games SET slug = CASE name
    WHEN 'Crazy Eights' THEN 'crazy_eights'
    WHEN 'Poker'        THEN 'poker'
    WHEN 'Uno'          THEN 'uno'
    WHEN 'Hearts'       THEN 'hearts'
    WHEN 'Gin Rummy'    THEN 'gin_rummy'
    ELSE lower(regexp_replace(name, '[^A-Za-z0-9]+', '_', 'g'))
END WHERE slug IS NULL;

ALTER TABLE games ALTER COLUMN slug SET NOT NULL;
CREATE UNIQUE INDEX idx_games_slug ON games(slug);

-- The old unique index on name has to go with it: a rename now writes the new display
-- string onto the existing row, and a stale row still holding that name would turn
-- every later finalize for the renamed game into a constraint violation.
DROP INDEX IF EXISTS idx_games_name;
