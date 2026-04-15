package db

import (
	"time"

	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	LastSeenAt time.Time
	Username   string
	PublicKeys []PublicKey
	Rankings   []Ranking
}

type PublicKey struct {
	gorm.Model
	Fingerprint string
	Name        string
	LastUsedAt  time.Time
	UserID      uint
}

type Ranking struct {
	UserID uint `gorm:"primaryKey"`
	GameID uint `gorm:"primaryKey"`

	Elo uint32 `gorm:"check:elo_valid,elo >= 0 AND elo <= 4000;default:1000"`

	// Allows preload data from memory using gorm .Preload
	User User `gorm:"foreignKey:UserID"`
	Game Game `gorm:"foreignKey:GameID"`

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}
