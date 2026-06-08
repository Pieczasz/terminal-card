package player

import (
	"terminalcard/internal/db"
	"terminalcard/internal/deck"
)

type Player struct {
	ID           string
	DatabaseUser *db.User
	Cards        []deck.Card
}

func (p *Player) Compare(other *Player) bool {
	if p == nil || other == nil {
		return false
	}

	return p.DatabaseUser.ID == other.DatabaseUser.ID
}
