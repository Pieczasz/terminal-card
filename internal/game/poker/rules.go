package poker

import (
	"errors"

	"terminalcard/internal/deck"
	"terminalcard/internal/game"
	"terminalcard/internal/player"
)

type PokerRules struct{}

var _ game.Rules = (*PokerRules)(nil)

func (r *PokerRules) Name() string    { return "Poker" }
func (r *PokerRules) MinPlayers() int { return 2 }
func (r *PokerRules) MaxPlayers() int { return 9 }

func (r *PokerRules) InitialDeck() []deck.Card {
	return deck.StandardDeck()
}

func (r *PokerRules) InitialDealCount() int {
	return 2
}

func (r *PokerRules) OnGameStart(state *game.State) error {
	if state.Deck.Size() < 5 {
		return errors.New("not enough cards to start the game")
	}

	playerChips := make(map[string]uint, len(state.Players))
	for _, p := range state.Players {
		playerChips[p.ID] = 1000
	}

	playerBets := make(map[string]uint, len(state.Players))

	smallBlindAmount := uint(25)
	bigBlindAmount := uint(50)

	N := len(state.Players)
	var sbIndex, bbIndex, dealerIndex int
	if N == 2 {
		dealerIndex = state.CurrentTurn
		sbIndex = state.CurrentTurn
		bbIndex = (state.CurrentTurn + 1) % N
	} else {
		dealerIndex = (state.CurrentTurn - 3 + N) % N
		bbIndex = (state.CurrentTurn - 1 + N) % N
		sbIndex = (state.CurrentTurn - 2 + N) % N
	}

	sbPlayer := state.Players[sbIndex]
	bbPlayer := state.Players[bbIndex]

	playerChips[sbPlayer.ID] -= smallBlindAmount
	playerChips[bbPlayer.ID] -= bigBlindAmount

	playerBets[sbPlayer.ID] = smallBlindAmount
	playerBets[bbPlayer.ID] = bigBlindAmount

	state.Extra = &State{
		DealerIndex: dealerIndex,
		MainPool:    smallBlindAmount + bigBlindAmount,
		CurrentBet:  bigBlindAmount,
		SmallBlind:  smallBlindAmount,
		BigBlind:    bigBlindAmount,
		Phase:       PreFlop,
		PlayersFold: make([]*player.Player, 0, len(state.Players)),
		Table:       make([]*deck.Card, 0, 5),
		PlayerChips: playerChips,
		PlayerBets:  playerBets,
	}

	return nil
}

type ActionFold struct{}

func (a ActionFold) Name() string { return "poker.Fold" }

type ActionPass struct{}

func (a ActionPass) Name() string { return "poker.Pass" }

type ActionBet struct {
	Amount uint
}

func (a ActionBet) Name() string { return "poker.Bet" }

func (r *PokerRules) PreActionCondition(state *game.State, action game.Action) error {
	extra, ok := state.Extra.(*State)
	if !ok {
		return errors.New("invalid state type")
	}

	player := state.Players[state.CurrentTurn]
	switch action := action.(type) {
	case ActionFold:
		return nil
	case ActionPass: // acts as Check
		if extra.CurrentBet > extra.PlayerBets[player.ID] {
			return errors.New("cannot check, must call or raise")
		}
		return nil
	case ActionBet: // acts as Call/Raise
		if action.Amount < extra.CurrentBet {
			return errors.New("bet amount must be at least the current bet to call")
		}
		additionalNeeded := action.Amount - extra.PlayerBets[player.ID]
		if additionalNeeded > extra.PlayerChips[player.ID] {
			return errors.New("not enough chips")
		}
		return nil
	}

	return errors.New("action not allowed in poker")
}

func (r *PokerRules) ApplyAction(state *game.State, action game.Action) {
	extra, ok := state.Extra.(*State)
	if !ok {
		return
	}
	p := state.Players[state.CurrentTurn]

	switch action := action.(type) {
	case ActionFold:
		extra.PlayersFold = append(extra.PlayersFold, p)
	case ActionPass:
		// Do nothing, just pass turn
	case ActionBet:
		currentBet := extra.PlayerBets[p.ID]
		if action.Amount < currentBet {
			return
		}
		additional := action.Amount - currentBet
		if additional > extra.PlayerChips[p.ID] {
			return
		}
		extra.PlayerChips[p.ID] -= additional
		extra.PlayerBets[p.ID] = action.Amount
		extra.MainPool += additional

		if action.Amount > extra.CurrentBet {
			extra.CurrentBet = action.Amount
			extra.PlayerRaised = p
		}
	}
	extra.LastAction = action
}

func (r *PokerRules) PostActionCondition(_ *game.State, _ game.Action) error {
	// Not needed for poker currently, but we could check if round ends
	return nil
}

func (r *PokerRules) CheckWinCondition(state *game.State) bool {
	extra, ok := state.Extra.(*State)
	if !ok {
		return false
	}
	return len(extra.PlayersFold) >= len(state.Players)-1
}

func (r *PokerRules) GetStandings(state *game.State) []*player.Player {
	extra, ok := state.Extra.(*State)
	if !ok {
		return nil
	}

	if len(extra.PlayersFold) >= len(state.Players)-1 {
		// everyone folded except 1
		for _, p := range state.Players {
			folded := false
			for _, f := range extra.PlayersFold {
				if f.ID == p.ID {
					folded = true
					break
				}
			}
			if !folded {
				return []*player.Player{p}
			}
		}
	}

	// Showdown evaluation
	var activePlayers []*player.Player
	for _, p := range state.Players {
		folded := false
		for _, f := range extra.PlayersFold {
			if f.ID == p.ID {
				folded = true
				break
			}
		}
		if !folded {
			activePlayers = append(activePlayers, p)
		}
	}

	type playerHand struct {
		p     *player.Player
		score int
	}
	var hands []playerHand
	for _, p := range activePlayers {
		// we need player hole cards + table cards.
		// For now we assume players have 2 cards in their hand, and table has 5.
		var allCards []deck.Card
		allCards = append(allCards, p.Cards...)
		for _, c := range extra.Table {
			allCards = append(allCards, *c)
		}
		hands = append(hands, playerHand{p: p, score: EvaluateHand(allCards)})
	}

	// Sort by score descending
	for i := 0; i < len(hands); i++ {
		for j := i + 1; j < len(hands); j++ {
			if hands[j].score > hands[i].score {
				hands[i], hands[j] = hands[j], hands[i]
			}
		}
	}

	var standings []*player.Player
	for _, h := range hands {
		standings = append(standings, h.p)
	}

	// Append folded players at the bottom
	standings = append(standings, extra.PlayersFold...)

	return standings
}
