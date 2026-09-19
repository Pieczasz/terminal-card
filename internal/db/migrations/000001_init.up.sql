CREATE TABLE users (
    id UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    last_seen_at TIMESTAMPTZ,
    -- NOT NULL: a NULL username would satisfy UNIQUE (many NULLs) and skip the CHECK.
    -- 40 chars is deleted_ + 32 hex of the UUID; chosen names stay 1-16 via the CHECK
    -- and ValidateUsername, so a raw insert cannot squat a 40-char player name.
    username VARCHAR(40) NOT NULL UNIQUE,
    CONSTRAINT username_valid CHECK (
        username ~ '^[A-Za-z0-9_]+$'
        AND (
            char_length(username) <= 16
            OR username ~ '^deleted_[0-9a-f]{32}$'
        )
    )
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
    user_id UUID REFERENCES users(id) ON DELETE CASCADE
);
CREATE INDEX idx_public_keys_deleted_at ON public_keys(deleted_at);
CREATE INDEX idx_public_keys_user_id ON public_keys(user_id);

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
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    game_id BIGINT REFERENCES games(id) ON DELETE CASCADE,
    elo BIGINT DEFAULT 1500,
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    PRIMARY KEY (user_id, game_id),
    CONSTRAINT elo_valid CHECK (elo >= 0 AND elo <= 4000)
);
CREATE INDEX idx_rankings_deleted_at ON rankings(deleted_at);
CREATE INDEX idx_rankings_elo ON rankings(elo DESC);

CREATE TABLE matches (
    id BIGSERIAL PRIMARY KEY,
    created_at TIMESTAMPTZ,
    updated_at TIMESTAMPTZ,
    deleted_at TIMESTAMPTZ,
    game_id BIGINT REFERENCES games(id) ON DELETE CASCADE,
    -- Casual matches are recorded for history but never move Elo, and a ranked
    -- match can legitimately swing zero, so the two cannot be told apart by delta.
    ranked BOOLEAN NOT NULL DEFAULT FALSE
);
CREATE INDEX idx_matches_deleted_at ON matches(deleted_at);

CREATE TABLE match_participants (
    match_id BIGINT REFERENCES matches(id) ON DELETE CASCADE,
    user_id UUID REFERENCES users(id) ON DELETE CASCADE,
    placement BIGINT,
    elo_delta BIGINT,
    PRIMARY KEY (match_id, user_id)
);
CREATE INDEX idx_match_participants_user_id ON match_participants(user_id);
