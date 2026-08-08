package components

import (
	"strings"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"

	lg "charm.land/lipgloss/v2"
)

const (
	// overlapWidth is how much of a card stays visible once the next one covers it:
	// the left border plus all but one column of the face. Hiding a single column is
	// enough to tuck the cards into each other while every rank and pip stays readable.
	overlapWidth = FaceWidth - 1
	// cardRows is a framed card: the two borders plus the face.
	cardRows = FaceHeight + 2
)

// FanWidth is the printed width of a fan of n cards, which is what a caller needs to
// line anything up underneath it.
func FanWidth(n, selected int) int {
	total := 0
	for i := range n {
		total += CardSlotWidth(i, n, selected)
	}
	return total
}

// CardSlotWidth is the visible width of card i in a fan of n, where selected is the
// picked-out index or -1. A closed card is wider, so anything lined up under the fan
// has to ask rather than assume.
func CardSlotWidth(i, n, selected int) int {
	// Every card contributes its left border plus the columns of face it still shows;
	// a closed one adds its right border too.
	if i == n-1 || i == selected {
		return 1 + FaceWidth + 1
	}
	return 1 + overlapWidth
}

// RenderFan draws cards overlapping left to right, the way a hand sits in a player's
// grip: each card covers the last column of the one before it, and only the rightmost
// card shows its closing edge.
//
// selected is the index to draw as picked out, or -1 for none. That card keeps its own
// right border and is drawn in the selection colour, so it reads as lying on top of the
// fan. There is no vertical lift: raising one card would break the shared top edge that
// makes the rest read as a single hand.
func RenderFan(t styles.Theme, cards []deck.Card, selected int) string {
	if len(cards) == 0 {
		return ""
	}

	rows := make([]string, cardRows)
	for i, card := range cards {
		// A card is closed when nothing covers it: the rightmost one, and the picked-out
		// one, which sits over its neighbour.
		closed := i == len(cards)-1 || i == selected

		width := overlapWidth
		if closed {
			width = FaceWidth
		}

		for r, line := range fanColumn(t, card, i == selected, width, closed) {
			rows[r] += line
		}
	}
	return strings.Join(rows, "\n")
}

// fanColumn renders one card's slice of the fan: top border, face rows, bottom border.
func fanColumn(t styles.Theme, card deck.Card, selected bool, width int, closed bool) []string {
	border := t.CardFace
	if selected {
		border = t.Selection
	}
	edge := lg.NewStyle().Foreground(border)

	suit, suitStyle := suitStyle(t, card.Suit)
	cells := FaceCells(card, suit)

	closeTop, closeMid, closeBot := "", "", ""
	if closed {
		closeTop, closeMid, closeBot = edge.Render("╮"), edge.Render("│"), edge.Render("╯")
	}

	lines := make([]string, 0, cardRows)
	lines = append(lines, edge.Render("╭"+strings.Repeat("─", width))+closeTop)
	for _, row := range cells {
		lines = append(lines, edge.Render("│")+suitStyle.Render(strings.Join(row[:width], ""))+closeMid)
	}
	lines = append(lines, edge.Render("╰"+strings.Repeat("─", width))+closeBot)
	return lines
}
