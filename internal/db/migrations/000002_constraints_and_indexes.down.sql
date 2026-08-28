DROP INDEX IF EXISTS idx_rankings_game_id;
DROP INDEX IF EXISTS idx_matches_game_id;
DROP INDEX IF EXISTS idx_match_participants_user_match;
DROP INDEX IF EXISTS idx_rankings_game_elo;

ALTER TABLE match_participants ALTER COLUMN elo_delta DROP NOT NULL;
ALTER TABLE match_participants ALTER COLUMN placement DROP NOT NULL;
ALTER TABLE games ALTER COLUMN name DROP NOT NULL;
ALTER TABLE public_keys ALTER COLUMN fingerprint DROP NOT NULL;
