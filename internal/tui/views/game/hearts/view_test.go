package hearts

import (
	"fmt"
	"strings"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/hearts"
	"github.com/Pieczasz/terminal-card/internal/tui/components"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestView_HandOverShowsScores(t *testing.T) {
	t.Parallel()
	m := &Model{
		Global: router.GlobalContext{
			Theme:  styles.NewTheme(true),
			Width:  80,
			Height: 24,
		},
		Base:         gameview.BaseState{Phase: game.Playing},
		stage:        logic.StageHandOver,
		handComplete: true,
		handNumber:   2,
		seatOrder:    []string{"1", "2", "3", "4"},
		seatNames: map[string]string{
			"1": "alice", "2": "bob", "3": "carol", "4": "dave",
		},
		handPoints:       map[string]int{"1": 5, "2": 8, "3": 3, "4": 10},
		cumulativeScores: map[string]int{"1": 25, "2": 8, "3": 18, "4": 17},
	}

	out := m.View().Content
	require.NotEmpty(t, out)
	assert.Contains(t, out, "HAND 2 COMPLETE")
	assert.Contains(t, out, "alice")
	assert.Contains(t, out, "25")
}

func TestView_HeartsBrokenIndicator(t *testing.T) {
	t.Parallel()
	m := &Model{
		Global:       router.GlobalContext{Theme: styles.NewTheme(true)},
		heartsBroken: true,
		trickCards:   map[string]deck.Card{},
	}
	assert.Contains(t, m.renderHeartsBrokenIndicator(), "broken")
	m.heartsBroken = false
	assert.Contains(t, m.renderHeartsBrokenIndicator(), "not yet broken")
}

// Hearts deals four, but a seat stays empty for as long as it takes the engine to end
// the match after somebody leaves, and that is a frame the view still has to render.
// With three seats the art layout's third edge wrapped back onto the hero, so the view
// drew the player their own hand count as an opponent.
func TestOpponentAt_RefusesATableThatIsNotFourHanded(t *testing.T) {
	t.Parallel()

	m := &Model{
		Global: router.GlobalContext{Theme: styles.NewTheme(true), Width: 100, Height: 30},
		Bound:  game.Bind(&game.Engine{}, "1"),
		Base: gameview.BaseState{
			Phase: game.Playing,
			Seats: []game.PlayerSnapshot{
				{ID: "1", Username: "alice", HandSize: 13},
				{ID: "2", Username: "bob", HandSize: 13},
				{ID: "3", Username: "carol", HandSize: 13},
			},
			Opponents: []game.PlayerSnapshot{
				{ID: "2", Username: "bob", HandSize: 13},
				{ID: "3", Username: "carol", HandSize: 13},
			},
		},
		trickCards: map[string]deck.Card{},
	}

	for rel := range 3 {
		_, ok := m.opponentAt(rel)
		assert.Falsef(t, ok, "rel %d must not resolve on a three-handed table", rel)
	}

	// Degraded, not blank: a player still holding cards has to be on screen.
	out := m.View().Content
	assert.Contains(t, out, "bob")
	assert.Contains(t, out, "carol")
	assert.NotContains(t, out, "alice", "the hero is never drawn as an opponent")
}

func TestOpponentAt_MapsTheThreeEdgesAtAFullTable(t *testing.T) {
	t.Parallel()

	m := &Model{
		Bound: game.Bind(&game.Engine{}, "3"),
		Base: gameview.BaseState{Seats: []game.PlayerSnapshot{
			{ID: "1"}, {ID: "2"}, {ID: "3"}, {ID: "4"},
		}}}

	seen := make([]string, 0, 3)
	for rel := range 3 {
		o, ok := m.opponentAt(rel)
		require.Truef(t, ok, "rel %d", rel)
		seen = append(seen, o.ID)
	}
	assert.Equal(t, []string{"4", "1", "2"}, seen, "clockwise from the hero, hero excluded")
}

// The four cards of a trick are the one thing a player cannot play Hearts without
// seeing. The trick drawn with faces is three rows of card art - taller than the
// middle band on any normal terminal - so it used to be cut off at the band's edge,
// which silently hid whichever card was played from the far seat.
func TestView_TheWholeTrickIsVisibleAtEverySize(t *testing.T) {
	t.Parallel()

	seats := []game.PlayerSnapshot{
		{ID: "1", Username: "alice", HandSize: 10},
		{ID: "2", Username: "bob", HandSize: 10},
		{ID: "3", Username: "carol", HandSize: 10},
		{ID: "4", Username: "dave", HandSize: 10},
	}
	trick := map[string]deck.Card{
		"1": {Rank: deck.Ace, Suit: deck.Spades},
		"2": {Rank: deck.King, Suit: deck.Hearts},
		"3": {Rank: deck.Queen, Suit: deck.Diamonds},
		"4": {Rank: deck.Jack, Suit: deck.Clubs},
	}

	for _, size := range []struct{ w, h int }{{64, 20}, {80, 24}, {100, 30}, {120, 40}} {
		t.Run(fmt.Sprintf("%dx%d", size.w, size.h), func(t *testing.T) {
			t.Parallel()

			m := &Model{
				Global: router.GlobalContext{Theme: styles.NewTheme(true), Width: size.w, Height: size.h},
				Bound:  game.Bind(&game.Engine{}, "1"),
				Base: gameview.BaseState{
					Phase: game.Playing, Seats: seats, Opponents: seats[1:],
					Hand: []deck.Card{{Rank: deck.Two, Suit: deck.Clubs}},
				},
				trickCards: trick,
				seatNames:  map[string]string{},
			}

			out := stripANSI(m.View().Content)
			for id, card := range trick {
				assert.Containsf(t, out, components.RankLabel(card.Rank),
					"the card played from seat %s is not on screen", id)
			}
		})
	}
}

// stripANSI drops the colour sequences so a frame can be searched as plain text.
func stripANSI(s string) string {
	var out strings.Builder
	inEscape := false
	for _, r := range s {
		switch {
		case r == 0x1b:
			inEscape = true
		case inEscape && (r == 'm' || r == 'K' || r == 'H'):
			inEscape = false
		case !inEscape:
			out.WriteRune(r)
		}
	}
	return out.String()
}
