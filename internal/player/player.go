package player

import "client/internal/db"

type Player struct {
	DatabaseUser *db.User
}

func (p *Player) Compare(other *Player) bool {
	if p == nil || other == nil {
		return false
	}

	return p.DatabaseUser.ID == other.DatabaseUser.ID
}
