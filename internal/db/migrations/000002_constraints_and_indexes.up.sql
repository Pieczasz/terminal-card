-- NOT NULL on columns whose Go fields cannot represent NULL. A NULL scans into the
-- zero value, so a NULL fingerprint would also satisfy UNIQUE (many NULLs allowed)
-- and a NULL placement would read back as a legitimate-looking 0. Nothing writes
-- NULL today, so these are no-ops against existing data.
ALTER TABLE public_keys ALTER COLUMN fingerprint SET NOT NULL;
ALTER TABLE games ALTER COLUMN name SET NOT NULL;
ALTER TABLE match_participants ALTER COLUMN placement SET NOT NULL;
ALTER TABLE match_participants ALTER COLUMN elo_delta SET NOT NULL;

-- The leaderboard hot path filters by game and sorts by Elo. idx_rankings_elo
-- cannot serve that once game_id is in the predicate, and it does not cover the
-- soft-delete filter either. elo >= 0 in the CHECK vs elo.MinRating = 100 in Go is
-- deliberate: the constraint is the wider bound, so application policy can move
-- without a migration.
CREATE INDEX idx_rankings_game_elo ON rankings (game_id, elo DESC) WHERE deleted_at IS NULL;

-- UserMatchHistory filters on user_id and sorts by match_id desc.
CREATE INDEX idx_match_participants_user_match ON match_participants (user_id, match_id DESC);

-- ON DELETE CASCADE seq-scans the referencing table without an index on the FK
-- column. idx_rankings_game_elo is partial, so the planner cannot use it here.
CREATE INDEX idx_matches_game_id ON matches (game_id);
CREATE INDEX idx_rankings_game_id ON rankings (game_id);
