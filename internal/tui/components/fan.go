package components

import (
	"strings"
	"sync"

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
	sel := map[int]struct{}{}
	if selected >= 0 {
		sel[selected] = struct{}{}
	}
	return renderFanCore(t, cards, sel)
}

// RenderFanMulti is RenderFan for multi-select: every index in selected is drawn as
// picked out. Used by Hearts' pass phase.
func RenderFanMulti(t styles.Theme, cards []deck.Card, selected map[int]struct{}) string {
	return renderFanCore(t, cards, selected)
}

func renderFanCore(t styles.Theme, cards []deck.Card, selected map[int]struct{}) string {
	if len(cards) == 0 {
		return ""
	}

	rows := make([]string, cardRows)
	for i, card := range cards {
		_, isSel := selected[i]
		// A card is closed when nothing covers it: the rightmost one, and any picked-out
		// one, which sits over its neighbour.
		closed := i == len(cards)-1 || isSel

		width := overlapWidth
		if closed {
			width = FaceWidth
		}

		for r, line := range fanColumn(t, card, isSel, width, closed) {
			rows[r] += line
		}
	}
	return strings.Join(rows, "\n")
}

// CardSlotWidthMulti is CardSlotWidth for a multi-select fan.
func CardSlotWidthMulti(i, n int, selected map[int]struct{}) int {
	if i == n-1 {
		return 1 + FaceWidth + 1
	}
	if _, ok := selected[i]; ok {
		return 1 + FaceWidth + 1
	}
	return 1 + overlapWidth
}

// columnKey is everything a fan column depends on; width follows from closed.
type columnKey struct {
	rank     deck.Rank
	suit     deck.Suit
	selected bool
	closed   bool
	dark     bool
}

// columnCache memoises fan columns. Every row of every card would otherwise re-emit
// its own colour sequence on every frame - a seven-card hand is over a hundred style
// renders - and a column only has a few hundred possible forms.
var columnCache sync.Map // columnKey -> []string

// fanColumn renders one card's slice of the fan: top border, face rows, bottom border.
// The returned slice is shared with the cache and must not be mutated.
func fanColumn(t styles.Theme, card deck.Card, selected bool, width int, closed bool) []string {
	key := columnKey{rank: card.Rank, suit: card.Suit, selected: selected, closed: closed, dark: t.Dark}
	if cached, ok := columnCache.Load(key); ok {
		lines, _ := cached.([]string)
		return lines
	}
	lines := renderFanColumn(t, card, selected, width, closed)
	columnCache.Store(key, lines)
	return lines
}

func renderFanColumn(t styles.Theme, card deck.Card, selected bool, width int, closed bool) []string {
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
