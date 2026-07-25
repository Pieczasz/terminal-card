CREATE TABLE users (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ,
    username VARCHAR(16) UNIQUE,
    CONSTRAINT username_valid CHECK (username ~ '^[A-Za-z0-9_]+$')
);
CREATE INDEX idx_users_deleted_at ON users(deleted_at);

CREATE TABLE public_keys (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    fingerprint TEXT UNIQUE,
    name TEXT,
    last_used_at TIMESTAMPTZ,
    user_id BIGINT REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX idx_public_keys_deleted_at ON public_keys(deleted_at);

CREATE TABLE games (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    name TEXT
);
CREATE INDEX idx_games_deleted_at ON games(deleted_at);
CREATE UNIQUE INDEX idx_games_name ON games(name);

CREATE TABLE rankings (
    user_id BIGINT REFERENCES users(id) ON DELETE CASCADE,
    game_id BIGINT REFERENCES games(id) ON DELETE CASCADE,
    elo BIGINT DEFAULT 1500,
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, game_id),
    CONSTRAINT elo_valid CHECK (elo >= 0 AND elo <= 4000)
);
CREATE INDEX idx_rankings_deleted_at ON rankings(deleted_at);

CREATE TABLE matches (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    game_id BIGINT REFERENCES games(id) ON DELETE CASCADE
);
CREATE INDEX idx_matches_deleted_at ON matches(deleted_at);

CREATE TABLE match_participants (
    match_id BIGINT REFERENCES matches(id) ON DELETE CASCADE,
    user_id BIGINT REFERENCES users(id) ON DELETE CASCADE,
    placement BIGINT,
    elo_delta BIGINT,
    PRIMARY KEY (match_id, user_id)
);
