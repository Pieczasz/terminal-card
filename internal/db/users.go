package db

import (
	"errors"
	"regexp"
	"time"

	"gorm.io/gorm"
)

type User struct {
	gorm.Model
	LastSeenAt time.Time
	Username   string `gorm:"uniqueIndex;type:varchar(16);check:username_valid,username ~ '^[A-Za-z0-9_]+$'"`
	PublicKeys []PublicKey
	Rankings   []Ranking
}

type PublicKey struct {
	gorm.Model
	Fingerprint string `gorm:"uniqueIndex"`
	Name        string
	LastUsedAt  time.Time
	UserID      uint
	User        User `gorm:"foreignKey:UserID"`
}

type Ranking struct {
	UserID uint `gorm:"primaryKey"`
	GameID uint `gorm:"primaryKey"`

	Elo uint32 `gorm:"check:elo_valid,elo >= 0 AND elo <= 4000;default:1500"`

	User User `gorm:"foreignKey:UserID"`
	Game Game `gorm:"foreignKey:GameID"`

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

func ValidateUsername(username string) error {
	if len(username) > 16 {
		return errors.New("username cannot exceed 16 characters")
	}
	if !usernamePattern.MatchString(username) {
		return errors.New("username can only contain English letters, numbers, and underscores")
	}
	return nil
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
