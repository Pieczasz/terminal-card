package db

import "context"

type UserRepository interface {
	LoadUserByFingerprint(ctx context.Context, fingerprint string) (*User, *PublicKey, error)
	RegisterUserWithKey(ctx context.Context, username, fingerprint string) (*User, *PublicKey, error)
	// BestPlayers returns the top rankings by Elo. An empty gameName means every
	// game; a non-empty name filters to that games.name (unknown names yield empty).
	// Results are cached per gameName and may be up to 5 minutes stale, so a caller
	// that has just written a ranking will not see it here.
	BestPlayers(ctx context.Context, limit int, gameName string) ([]Ranking, error)
	UserProfile(ctx context.Context, userID uint) (*User, error)
	UpdateUserActivity(ctx context.Context, user *User, key *PublicKey) error
	UserMatchHistory(ctx context.Context, userID uint, limit int) ([]MatchParticipant, error)
}

type MatchRepository interface {
	// RecordCasualMatch writes history for an unranked result in one transaction,
	// creating the game row on first sight. orderedUserIDs is the finish order.
	RecordCasualMatch(ctx context.Context, gameName string, orderedUserIDs []uint) error
	// FinalizeRankedMatch moves Elo and writes history in one transaction. places is
	// parallel to orderedUserIDs and 1-based; equal entries are a draw and settle
	// without moving rating between the tied players. A nil places means a strict
	// finish order.
	FinalizeRankedMatch(ctx context.Context, gameName string, orderedUserIDs []uint, places []int) error
}
