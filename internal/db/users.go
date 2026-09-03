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
	Username   string `gorm:"uniqueIndex;not null;type:varchar(16);check:username_valid,username ~ '^[A-Za-z0-9_]+$'"`
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

	// MatchesPlayed is the ranked track record this row has earned. Below
	// repository.provisionalMatches the account is provisional and beating it pays
	// nobody - a free SSH keypair is a free 1500-rated opponent otherwise.
	MatchesPlayed uint64 `gorm:"not null;default:0"`

	User User `gorm:"foreignKey:UserID"`
	Game Game `gorm:"foreignKey:GameID"`

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt `gorm:"index"`
}

const (
	UsernameMinLen = 3
	UsernameMaxLen = 16
)

func ValidateUsername(username string) error {
	if len(username) < UsernameMinLen {
		return errors.New("username must be at least 3 characters")
	}
	if len(username) > UsernameMaxLen {
		return errors.New("username cannot exceed 16 characters")
	}
	if !usernamePattern.MatchString(username) {
		return errors.New("username can only contain English letters, numbers, and underscores")
	}
	return nil
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
