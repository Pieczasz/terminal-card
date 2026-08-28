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

// artSeatLimit is how many opponents can be drawn with their card backs before the
// table stops reading as a table: past this the seats are down to a name and a count
// whatever the terminal size, because five stacks of art have nothing to say that the
// five counts do not.
const artSeatLimit = 3

// RenderOpponentTop is the row of opponents across the top edge, laid out in at most
// width columns.
func RenderOpponentTop(t styles.Theme, base BaseState, width int, compact bool) string {
	zones := SplitZones(base.Opponents)
	if len(zones.Top) == 0 {
		return ""
	}
	pad := lg.NewStyle().Padding(0, 1)
	// Each seat gets an equal share of the width, less the padding around it.
	budget := seatBudget(width, len(zones.Top)) - 2

	parts := make([]string, 0, len(zones.Top))
	minimal := minimalSeats(base, compact)
	for _, o := range zones.Top {
		parts = append(parts, pad.Render(renderOpponentSeat(t, base, o, OrientationTop, budget, minimal)))
	}
	// Names are not budgeted the way the art is - a seat is drawn as wide as its
	// player named themselves - so the row is cut to the width it was given.
	return styles.Clamp(width, 0, lg.JoinHorizontal(lg.Bottom, parts...))
}

// RenderOpponentSides is the two stacks of opponents down the left and right edges,
// each fitting into at most height rows - which is the middle band's height, not the
// terminal's, since that is all the stacks have to sit in.
func RenderOpponentSides(t styles.Theme, base BaseState, height int, compact bool) (left, right string) {
	zones := SplitZones(base.Opponents)
	minimal := minimalSeats(base, compact)
	return renderOpponentStack(t, base, zones.Left, OrientationLeft, seatBudget(height, len(zones.Left)), minimal),
		renderOpponentStack(t, base, zones.Right, OrientationRight, seatBudget(height, len(zones.Right)), minimal)
}

// minimalSeats reports whether the table is too crowded, or the terminal too small,
// for anybody's card art.
func minimalSeats(base BaseState, compact bool) bool {
	return compact || len(base.Opponents) > artSeatLimit
}

func seatBudget(total, seats int) int {
	if seats <= 0 {
		return 0
	}
	return max(total/seats, 0)
}

func renderOpponentStack(
	t styles.Theme,
	base BaseState,
	seats []game.PlayerSnapshot,
	orientation Orientation,
	budget int,
	minimal bool,
) string {
	if len(seats) == 0 {
		return ""
	}
	parts := make([]string, 0, len(seats))
	for _, o := range seats {
		// Two of the seat's rows go to its name and hand count.
		parts = append(parts, renderOpponentSeat(t, base, o, orientation, budget-2, minimal))
	}
	return lg.JoinVertical(lg.Center, parts...)
}

// seatArtFloor is the smallest budget one card of art fits in, on the axis the
// orientation stacks along. Below it the seat is drawn minimally: art cut down to
// nothing reads as a bug, a name and a count reads as a small terminal.
func seatArtFloor(orientation Orientation) int {
	if orientation == OrientationTop {
		return 1 + topCardsFrame
	}
	return 1 + sideCardsFrame
}

func renderOpponentSeat(
	t styles.Theme,
	base BaseState,
	o game.PlayerSnapshot,
	orientation Orientation,
	budget int,
	minimal bool,
) string {
	isTurn := base.CurrentPlayerID == o.ID
	if minimal || budget < seatArtFloor(orientation) {
		return RenderOpponentMinimal(t, o, isTurn)
	}
	return RenderOpponent(t, o, isTurn, orientation, base.TurnRemaining, budget)
}
