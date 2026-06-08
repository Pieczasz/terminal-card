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
	tm.direction = -tm.direction
}

func (tm *TurnManager) SetCurrent(current int) {
	tm.current = current
}

func (tm *TurnManager) RemovePlayer(index int) {
	tm.playerCount--
	if tm.playerCount <= 0 {
		tm.playerCount = 1 // safety
		return
	}

	if tm.current > index {
		tm.current--
	} else if tm.current == index {
		if tm.direction == 1 {
			// Current player removed and going forward, the next player physically shifts into the same index.
			// But wait, if index is the last element, it should wrap.
			tm.current = tm.current % tm.playerCount
		} else {
			// Going backwards. The previous player shifts index down if they were after, but we handle that already.
			// Actually, just step backwards from the current physical index.
			tm.current = (tm.current - 1 + tm.playerCount) % tm.playerCount
		}
	}
}
