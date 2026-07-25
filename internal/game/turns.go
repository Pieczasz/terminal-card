package game

// TurnManager tracks the current seat index. Play only proceeds forward.
type TurnManager struct {
	playerCount int
	current     int
}

func NewTurnManager(numberOfPlayers int) *TurnManager {
	return &TurnManager{playerCount: numberOfPlayers}
}

func (tm *TurnManager) Current() int {
	return tm.current
}

func (tm *TurnManager) Next() {
	if tm.playerCount <= 0 {
		return
	}
	tm.current = (tm.current + 1) % tm.playerCount
}

func (tm *TurnManager) SetCurrent(current int) {
	tm.current = current
}

// clampCurrent forces current into [0, playerCount) so a stale index cannot panic.
func (tm *TurnManager) clampCurrent() {
	if tm.playerCount <= 0 {
		tm.current = 0
		return
	}
	tm.current = ((tm.current % tm.playerCount) + tm.playerCount) % tm.playerCount
}

// RemovePlayer shifts the cursor after the player at index leaves.
func (tm *TurnManager) RemovePlayer(index int) {
	tm.playerCount--
	if tm.playerCount <= 0 {
		tm.playerCount = 1
		tm.current = 0
		return
	}
	if tm.current > index {
		tm.current--
	}
	tm.clampCurrent()
}
