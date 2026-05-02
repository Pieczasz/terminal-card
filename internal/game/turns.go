package game

type TurnManager struct {
	playerCount int
	current     int
	direction   int
}

func NewTurnManager(numberOfPlayers int) *TurnManager {
	return &TurnManager{
		playerCount: numberOfPlayers,
		current:     0,
		direction:   1,
	}
}

func (tm *TurnManager) Current() int {
	return tm.current
}

func (tm *TurnManager) Next() {
	tm.current = (tm.current + tm.direction + tm.playerCount) % tm.playerCount
}

func (tm *TurnManager) Reverse() {
	tm.direction = -1
}
