package db

import (
	"gorm.io/gorm"
)

type Match struct {
	gorm.Model
	GameID uint
	// Ranked separates a rated result from a casual one, which is recorded for
	// history but leaves every participant's Elo where it was.
	Ranked       bool
	Game         Game               `gorm:"foreignKey:GameID"`
	Participants []MatchParticipant `gorm:"foreignKey:MatchID"`
}

type MatchParticipant struct {
	MatchID   uint `gorm:"primaryKey;autoIncrement:false"`
	UserID    uint `gorm:"primaryKey;autoIncrement:false"`
	Placement int  // 1 for first place/winner, 2 for second, etc.
	EloDelta  int  // How much elo they gained/lost

	User  User  `gorm:"foreignKey:UserID"`
	Match Match `gorm:"foreignKey:MatchID"`
}
