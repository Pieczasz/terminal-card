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
}

type PublicKey struct {
	gorm.Model
	Fingerprint string
	Name        string
	LastUsedAt  time.Time
	UserID      uint
}
