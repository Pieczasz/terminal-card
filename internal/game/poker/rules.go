package poker

import (
	"errors"

	"terminalcard/internal/deck"
	"terminalcard/internal/game"
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

	switch action := action.(type) {
	case ActionFold:
		return nil
	case ActionPass:
		if extra.CurrentBet > 0 {
			return errors.New("cannot check, must call or raise")
		}
		return nil
	case ActionBet:
		_ = action // Access amount later if we check player chips
		return nil // TODO: Check if player has enough chips
	}

	return errors.New("action not allowed in poker")
}

func (r *PokerRules) ApplyAction(state *game.State, action game.Action) {
	extra := state.Extra.(*State)
	player := state.Players[state.CurrentTurn]

	switch action := action.(type) {
	case ActionFold:
		extra.PlayersFold = append(extra.PlayersFold, player)
	case ActionPass:
		// Do nothing
	case ActionBet:
		extra.CurrentBet = action.Amount // This might be a raise, simplifying for now
		extra.MainPool += action.Amount
		// TODO: Deduct from PlayerChips, add to PlayerBets
	}
}

func (r *PokerRules) PostActionCondition(state *game.State, action game.Action) error {
	return nil
}

func (r *PokerRules) CheckWinCondition(state *game.State) bool {
	extra := state.Extra.(*State)
	// If everyone else folded, the remaining player wins
	if len(extra.PlayersFold) >= len(state.Players)-1 {
		return true
	}
	return false
}
