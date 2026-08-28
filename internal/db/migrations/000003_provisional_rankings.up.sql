-- Provisional accounts: identity is a free SSH keypair, so a fresh 1500-rated
-- account costs nothing to mint. matches_played is the track record that decides
-- whether beating a ranking row pays anything (see internal/repository).
ALTER TABLE rankings ADD COLUMN matches_played BIGINT NOT NULL DEFAULT 0;
