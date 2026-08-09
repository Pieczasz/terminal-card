package db

import "context"

type UserRepository interface {
	LoadUserByFingerprint(ctx context.Context, fingerprint string) (*User, *PublicKey, error)
	RegisterUserWithKey(ctx context.Context, username, fingerprint string) (*User, *PublicKey, error)
	// BestPlayers returns the top rankings by Elo. An empty gameName means every
	// game; a non-empty name filters to that games.name (unknown names yield empty).
	BestPlayers(ctx context.Context, limit int, gameName string) ([]Ranking, error)
	UserProfile(ctx context.Context, userID uint) (*User, error)
	UpdateUserActivity(ctx context.Context, user *User, key *PublicKey)
	UserMatchHistory(ctx context.Context, userID uint, limit int) ([]MatchParticipant, error)
}

type MatchRepository interface {
	RecordMatch(ctx context.Context, gameID uint, orderedUserIDs []uint, eloDeltas map[uint]int, ranked bool) error
	GetOrCreateGame(ctx context.Context, name string) (*Game, error)
	// FinalizeRankedMatch moves Elo and writes history in one transaction. places is
	// parallel to orderedUserIDs and 1-based; equal entries are a draw and settle
	// without moving rating between the tied players. A nil places means a strict
	// finish order.
	FinalizeRankedMatch(ctx context.Context, gameName string, orderedUserIDs []uint, places []int) error
}
