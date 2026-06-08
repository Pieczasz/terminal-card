package poker

//
// import (
// 	"terminalcard/internal/deck"
// 	"terminalcard/internal/game"
// 	"errors"
// 	"slices"
// )
//
// type PokerRules struct{}
//
// var _ game.Rules = (*PokerRules)(nil)
//
// func (r *PokerRules) Name() string    { return "Poker" }
// func (r *PokerRules) MinPlayers() int { return 2 }
// func (r *PokerRules) MaxPlayers() int { return 9 }
//
// func (r *PokerRules) InitialDeck() []deck.Card {
// 	return deck.StandardDeck()
// }
//
// func (r *PokerRules) InitialDealCount() int {
// 	return 2
// }
//
// func (r *PokerRules) PreActionCondition(state *game.State, action game.Action) error {
// 	extra, ok := state.Extra.(*State)
// 	if !ok {
// 		return errors.New("invalid state type")
// 	}
//
// 	switch action.Type {
// 	case game.ActionPass:
//
// 	}
// 	return errors.New("unknown action")
// }
//
// func (r *PokerRules) ApplyAction(state *game.State, action game.Action) {
// 	extra := state.Extra.(*State)
// 	player := state.Players[state.CurrentTurn]
//
// 	switch action.Type {
// 	case game.ActionPlayCard:
// 		card := action.Cards[0]
//
// 		newHand := make([]deck.Card, 0, len(player.Cards)-1)
// 		removed := false
// 		for _, c := range player.Cards {
// 			if c == card && !removed {
// 				removed = true
// 				continue
// 			}
// 			newHand = append(newHand, c)
// 		}
// 		player.Cards = newHand
//
// 		state.Discard.AddCard(card)
//
// 		if card.Rank != deck.Eight {
// 			extra.CurrentSuit = card.Suit
// 		}
//
// 	case game.ActionDrawCard:
// 		drawn, _ := state.Deck.Draw()
// 		player.Cards = append(player.Cards, drawn)
//
// 	case game.ActionPickSuit:
// 		extra.CurrentSuit = action.Suit
// 	}
// }
//
// func (r *PokerRules) PostActionCondition(state *game.State, action game.Action) error {
// 	return nil
// }
//
// func (r *PokerRules) CheckWinCondition(state *game.State) bool {
// 	for _, p := range state.Players {
// 		if len(p.Cards) == 0 {
// 			return true
// 		}
// 	}
// 	return false
// }
