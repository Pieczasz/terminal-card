package db

import "context"

type UserRepository interface {
	LoadUserByFingerprint(ctx context.Context, fingerprint string) (*User, *PublicKey, error)
	RegisterUserWithKey(ctx context.Context, username, fingerprint string) (*User, *PublicKey, error)
	BestPlayers(ctx context.Context, limit int) ([]Ranking, error)
	UserProfile(ctx context.Context, userID uint) (*User, error)
	UpdateUserActivity(ctx context.Context, user *User, key *PublicKey)
	UserMatchHistory(ctx context.Context, userID uint, limit int) ([]MatchParticipant, error)
}

type MatchRepository interface {
	UpdateRankings(ctx context.Context, gameID uint, orderedUserIDs []uint) (map[uint]int, error)
	RecordMatch(ctx context.Context, gameID uint, orderedUserIDs []uint, eloDeltas map[uint]int) error
	GetOrCreateGame(ctx context.Context, name string) (*Game, error)
	FinalizeRankedMatch(ctx context.Context, gameName string, orderedUserIDs []uint) error
}
