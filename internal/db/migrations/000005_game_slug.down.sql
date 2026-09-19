CREATE UNIQUE INDEX idx_games_name ON games(name);
DROP INDEX IF EXISTS idx_games_slug;
ALTER TABLE games DROP COLUMN slug;
