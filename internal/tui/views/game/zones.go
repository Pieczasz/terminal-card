package game

import (
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"

	lg "charm.land/lipgloss/v2"
)

// TableZones is a table's opponents split across the three edges a view draws
// them on. The hero holds the bottom edge, so they are never in here.
type TableZones[T any] struct {
	Left  []T
	Top   []T
	Right []T
}

// topCapacity is how many seats the top edge takes before the sides carry the
// rest. Beyond four the row runs wider than a terminal.
const topCapacity = 4

// SplitZones seats opponents clockwise from the hero's left: down the left edge,
// across the top, then down the right.
//
// Every opponent is placed. A layout with a fixed number of slots drops whoever
// does not fit, and a seat that is not drawn is a player still holding cards that
// nobody at the table can see.
func SplitZones[T any](opponents []T) TableZones[T] {
	n := len(opponents)
	if n == 0 {
		return TableZones[T]{}
	}

	side := max((n-topCapacity+1)/2, 0)
	// Two opponents read as sitting across the corners from the hero rather than
	// shoulder to shoulder above them, so the sides take one each before the top
	// starts filling.
	if n > 1 && side == 0 {
		side = 1
	}

	return TableZones[T]{
		Left:  opponents[:side],
		Top:   opponents[side : n-side],
		Right: opponents[n-side:],
	}
}

// OpponentEdges is the opponents of a table already rendered, one string per edge.
type OpponentEdges struct {
	Top   string
	Left  string
	Right string
}

// artSeatLimit is how many opponents can be drawn with their card backs before
// the table stops fitting: an Uno hand runs past twenty cards, and four stacks
// that wide across the top are wider than any terminal.
const artSeatLimit = 3

// RenderOpponentEdges lays every opponent around the table: a row across the top
// and a stack down each side. compact says the terminal is too short for the card
// art; a crowded table drops it too, and then the hand count carries what a player
// actually reads off somebody else's seat.
func RenderOpponentEdges(t styles.Theme, base BaseState, compact bool) OpponentEdges {
	zones := SplitZones(base.Opponents)
	minimal := compact || len(base.Opponents) > artSeatLimit

	return OpponentEdges{
		Top:   renderOpponentRow(t, base, zones.Top, minimal),
		Left:  renderOpponentStack(t, base, zones.Left, OrientationLeft, minimal),
		Right: renderOpponentStack(t, base, zones.Right, OrientationRight, minimal),
	}
}

func renderOpponentRow(t styles.Theme, base BaseState, seats []game.PlayerSnapshot, minimal bool) string {
	if len(seats) == 0 {
		return ""
	}
	pad := lg.NewStyle().Padding(0, 1)
	parts := make([]string, 0, len(seats))
	for _, o := range seats {
		parts = append(parts, pad.Render(renderOpponentSeat(t, base, o, OrientationTop, minimal)))
	}
	return lg.JoinHorizontal(lg.Bottom, parts...)
}

func renderOpponentStack(
	t styles.Theme,
	base BaseState,
	seats []game.PlayerSnapshot,
	orientation Orientation,
	minimal bool,
) string {
	if len(seats) == 0 {
		return ""
	}
	parts := make([]string, 0, len(seats))
	for _, o := range seats {
		parts = append(parts, renderOpponentSeat(t, base, o, orientation, minimal))
	}
	return lg.JoinVertical(lg.Center, parts...)
}

func renderOpponentSeat(
	t styles.Theme,
	base BaseState,
	o game.PlayerSnapshot,
	orientation Orientation,
	minimal bool,
) string {
	isTurn := base.CurrentPlayerID == o.ID
	if minimal {
		return RenderOpponentMinimal(t, o, isTurn)
	}
	return RenderOpponent(t, o, isTurn, orientation, base.TurnRemaining)
}
