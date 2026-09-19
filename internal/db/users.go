package db

import (
	"errors"
	"fmt"
	"regexp"
	"strconv"
	"time"

	"gorm.io/gorm"
)

// Nothing calls AutoMigrate - internal/db/migrations owns the schema - so
// uniqueIndex/not null/default/check/type tags would be decoration that reads like
// enforcement, and one of them had already drifted from the SQL. They are gone; the
// tags that remain (primaryKey, foreignKey, autoIncrement) are the ones GORM actually
// uses to build queries. TestSchemaNullabilityMatchesStructs derives what the SQL must
// guarantee from these structs, so the drift is caught by CI rather than by a tag.
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
	User        User `gorm:"foreignKey:UserID"`
}

type Ranking struct {
	UserID uint `gorm:"primaryKey"`
	GameID uint `gorm:"primaryKey"`

	Elo uint32

	// MatchesPlayed is the ranked track record this row has earned. Below
	// repository.provisionalMatches the account is provisional and beating it pays
	// nobody - a free SSH keypair is a free 1500-rated opponent otherwise.
	MatchesPlayed uint64

	User User `gorm:"foreignKey:UserID"`
	Game Game `gorm:"foreignKey:GameID"`

	CreatedAt time.Time
	UpdatedAt time.Time
	DeletedAt gorm.DeletedAt
}

// MaxUsernameLength is the users.username varchar width. AnonymisedUsername has to
// stay inside it too, so the limit is a constant rather than a literal per caller.
const MaxUsernameLength = 16

// AnonymisedUsername is the name an erased account is left under. It has to satisfy
// both column constraints - varchar(16) and ^[A-Za-z0-9_]+$ - because the row stays:
// other players' match history still points at it. Decimal reads best ("deleted_42");
// past 99999999 accounts it no longer fits, and base 36 of the same id does, which is
// still one distinct name per account.
func AnonymisedUsername(userID uint) string {
	name := anonymisedPrefix + strconv.FormatUint(uint64(userID), 10)
	if len(name) > MaxUsernameLength {
		name = anonymisedPrefix + strconv.FormatUint(uint64(userID), 36)
	}
	return name
}

const anonymisedPrefix = "deleted_"

func ValidateUsername(username string) error {
	if len(username) > MaxUsernameLength {
		return fmt.Errorf("username cannot exceed %d characters", MaxUsernameLength)
	}
	if !usernamePattern.MatchString(username) {
		return errors.New("username can only contain English letters, numbers, and underscores")
	}
	return nil
}

var usernamePattern = regexp.MustCompile(`^[A-Za-z0-9_]+$`)
