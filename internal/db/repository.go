package db

import "context"

type UserRepository interface {
	LoadUserByFingerprint(ctx context.Context, fingerprint string) (*User, *PublicKey, error)
	RegisterUserWithKey(ctx context.Context, username, fingerprint string) (*User, *PublicKey, error)
	GetBestPlayers(ctx context.Context, limit int) ([]Ranking, error)
	GetUserProfile(ctx context.Context, userID uint) (*User, error)
	UpdateUserActivity(ctx context.Context, user *User, key *PublicKey)
	GetUserMatchHistory(ctx context.Context, userID uint, limit int) ([]MatchParticipant, error)
}

type MatchRepository interface {
	UpdateRankings(ctx context.Context, gameID uint, orderedUserIDs []uint) (map[uint]int, error)
	RecordMatch(ctx context.Context, gameID uint, orderedUserIDs []uint, eloDeltas map[uint]int) error
}
