package game

import (
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"

	lg "charm.land/lipgloss/v2"
)

// The breakpoints every game view lays out against. They live here rather than in each
// view so that "compact" means the same thing on all five tables; they were copied into
// six files, and a table that disagreed with its own hand about how much room there was
// drew one of them off the screen.
//
// The heights are what the bands actually cost, not round numbers. A seat drawn with
// its card backs is eight rows, the pile in the middle is eleven, the turn banner three,
// a fanned hand ten and the hints two: forty rows before anything is squeezed, which is
// why full art needs a tall terminal and everything shorter drops the seat art first
// and the hand fan last. The hand is the part the player acts on, so it goes last.
const (
	compactHeight      = 40
	compactWidth       = 80
	superCompactHeight = 26
	superCompactWidth  = 72
)

// IsCompact reports whether the terminal has to give up its decoration: seats lose
// their card art, the key hints lose their margins and the chip stacks go.
func IsCompact(width, height int) bool {
	return height < compactHeight || width < compactWidth
}

// IsSuperCompact reports whether there is not even room for the hero's own fan, and
// the whole frame is down to names, counts and a one-line hand.
func IsSuperCompact(width, height int) bool {
	return height < superCompactHeight || width < superCompactWidth
}

// RenderBands is the three-band table frame the four non-poker games draw: the
// opponents across the top, the table itself in the middle, and the hero's own hand
// with its key hints along the bottom. mid is called with the rows left over once the
// two fixed bands have taken theirs, which is also the height its side seats get.
//
// The result is clamped to the terminal. PadCenter and Place both hand overwide
// content straight through, so a band that overruns its budget has to be cut here or
// the terminal wraps it and shifts every row below.
func RenderBands(g router.GlobalContext, top, player, hints string, mid func(height int) string) string {
	compact := IsCompact(g.Width, g.Height)
	superCompact := IsSuperCompact(g.Width, g.Height)

	topBand := top
	if !superCompact {
		topBand = lg.NewStyle().MarginTop(1).Render(top)
	}

	var botBand string
	switch {
	case superCompact:
		botBand = player
	case compact:
		botBand = lg.JoinVertical(lg.Center, player, g.Theme.Dim.Render(hints))
	default:
		botBand = lg.NewStyle().MarginBottom(1).Render(
			lg.JoinVertical(lg.Center, player, g.Theme.Dim.MarginTop(1).Render(hints)),
		)
	}

	// Each band is held to its own budget before the frame is: Place and PadCenter
	// both hand back content bigger than the box they were given, and a band that
	// overran would otherwise be trimmed out of the frame's total - eating the
	// opponents along the top instead of its own overflow.
	topBand = styles.Clamp(g.Width, 0, topBand)
	botBand = styles.Clamp(g.Width, 0, botBand)
	midHeight := max(g.Height-lg.Height(topBand)-lg.Height(botBand), 0)

	return styles.Clamp(g.Width, g.Height, lg.JoinVertical(lg.Left,
		styles.PadCenter(g.Width, topBand),
		styles.Clamp(g.Width, midHeight, mid(midHeight)),
		styles.PadCenter(g.Width, botBand),
	))
}

// RenderTableRow lays the middle band out: the two side stacks against the edges with
// the centre between them.
//
// The sides take only the width they need rather than a third of the table each. A name
// and a hand count is narrower than a stack of card art, and the centre is the part that
// has to fit the pile, the indicators and the pickers - given a fixed third it overran
// it, and the row then ran wider than the terminal by however much the centre needed.
// The sides are still capped at a third so a long username cannot squeeze the pile out.
func RenderTableRow(width, height int, left, center, right string) string {
	side := min(max(lg.Width(left), lg.Width(right)), width/3)
	centerWidth := max(width-2*side, 0)

	return lg.JoinHorizontal(lg.Top,
		styles.Place(side, height, lg.Left, lg.Center, styles.Clamp(side, height, left)),
		styles.Place(centerWidth, height, lg.Center, lg.Center, styles.Clamp(centerWidth, height, center)),
		styles.Place(side, height, lg.Right, lg.Center, styles.Clamp(side, height, right)),
	)
}

// HandKeyHints is the key line for the two games a player drives entirely from the
// hand cursor. Crazy Eights and Uno had a byte-identical copy each.
const HandKeyHints = "<-/h: left | ->/l: right | enter: play/confirm | d: draw | esc: leave/cancel"

// HandWidth is how many columns the hero's hand may take. The bottom band spans the
// terminal, less a column at each edge so a full-width fan is not flush against it.
func HandWidth(width int) int {
	return max(width-2, 0)
}

// stripHandRows is what the compact rank-and-suit hand costs at its widest.
const stripHandRows = 3

// HandRows is how many rows the hero's hand may take. Zero means unbounded; a short
// terminal cannot spare ten of them for card art and still show the table above the
// hand, so it gets the compact strip - the same fallback an unfittable width gets.
func HandRows(height int) int {
	if height < superCompactHeight {
		return stripHandRows
	}
	return 0
}
