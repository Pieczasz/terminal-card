package db

import "gorm.io/gorm"

// Queries provides abstraction layer for database operations
type Queries struct {
	db *gorm.DB
}

func NewQueries(db *gorm.DB) *Queries {
	return &Queries{db: db}
}

// GetBestPlayers returns the top players across all games, or filtered by gameID.
// TODO: cache this, maybe like top 10 of each
func (q *Queries) GetBestPlayers(limit int) ([]Ranking, error) {
	var rankings []Ranking
	err := q.db.Preload("User").Preload("Game").
		Order("elo desc").
		Limit(limit).
		Find(&rankings).Error
	return rankings, err
}

// GetUserProfile retrieves a user by ID with their public keys and rankings.
func (q *Queries) GetUserProfile(userID uint) (*User, error) {
	var user User
	err := q.db.Preload("PublicKeys").
		Preload("Rankings").
		Preload("Rankings.Game").
		First(&user, userID).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}
