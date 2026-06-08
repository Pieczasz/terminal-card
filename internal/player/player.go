package player

import (
	"terminalcard/internal/db"
	"terminalcard/internal/deck"
)

type Player struct {
	Id           string
	DatabaseUser *db.User
	Cards        []deck.Card
}

func (p *Player) Compare(other *Player) bool {
	if p == nil || other == nil {
		return false
	}

	return p.DatabaseUser.ID == other.DatabaseUser.ID
}
