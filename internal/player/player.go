package player

import (
	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/deck"
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
	if p.DatabaseUser == nil || other.DatabaseUser == nil {
		return p.ID != "" && p.ID == other.ID
	}
	return p.DatabaseUser.ID == other.DatabaseUser.ID
}

func (p *Player) Username() string {
	if p == nil {
		return ""
	}
	if p.DatabaseUser != nil && p.DatabaseUser.Username != "" {
		return p.DatabaseUser.Username
	}
	return p.ID
}
