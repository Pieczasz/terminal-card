package lobby

import (
	"strconv"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/game"
)

// NewPlayer seats a database user. It is the only place a db.User becomes a
// game.Player, which is what lets internal/game stay free of internal/db: the
// ratings are flattened here, once, from the Rankings the session preloaded.
func NewPlayer(u *db.User) *game.Player {
	if u == nil {
		return nil
	}
	ratings := make(map[string]uint32, len(u.Rankings))
	for _, r := range u.Rankings {
		if r.Game.Name != "" {
			ratings[r.Game.Name] = r.Elo
		}
	}
	return &game.Player{
		ID:      strconv.FormatUint(uint64(u.ID), 10),
		UserID:  u.ID,
		Name:    u.Username,
		Ratings: ratings,
	}
}
