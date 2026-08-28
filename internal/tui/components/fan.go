package components

import (
	"fmt"
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
	// minTuckWidth is the narrowest a covered card may be squeezed to. It is the
	// centre pip column and no less: the suit of a card with a single central pip -
	// an ace - lives there, and a hand whose suits are invisible is unplayable.
	minTuckWidth = CentreColumn + 1
	// cardRows is a framed card: the two borders plus the face.
	cardRows = FaceHeight + 2
	// FanRows is how many rows a fan occupies, for a caller budgeting height.
	FanRows = cardRows
)

// FanTuck is how many face columns each covered card shows so a fan of n cards fits
// maxWidth, or 0 when not even the tightest fan does and the caller has to fall back
// to RenderStrip. A maxWidth of zero or less means unbounded.
func FanTuck(n, maxWidth int) int {
	if n <= 0 || maxWidth <= 0 {
		return overlapWidth
	}
	for tuck := overlapWidth; tuck >= minTuckWidth; tuck-- {
		if fanWidth(n, tuck) <= maxWidth {
			return tuck
		}
	}
	return 0
}

// fanWidth is the widest a fan of n cards at this tuck can get: it budgets for a
// picked-out card in the middle of the hand, which keeps its own closing border.
// Budgeting for the worst case is what stops the hand changing width - and so the
// whole layout shifting sideways - as the cursor moves along it.
func fanWidth(n, tuck int) int {
	if n <= 0 {
		return 0
	}
	width := (n-1)*(1+tuck) + 1 + FaceWidth + 1
	if n > 1 {
		width += FaceWidth + 1 - tuck
	}
	return width
}

// CardSlotWidth is the visible width of card i in a fan of n drawn at this tuck,
// where selected is the picked-out index or -1. A closed card is wider, so anything
// lined up under the fan has to ask rather than assume.
func CardSlotWidth(i, n, selected, tuck int) int {
	// Every card contributes its left border plus the columns of face it still shows;
	// a closed one adds its right border too.
	if i == n-1 || i == selected {
		return 1 + FaceWidth + 1
	}
	return 1 + tuck
}

// CardSlotWidthMulti is CardSlotWidth for a multi-select fan.
func CardSlotWidthMulti(i, n, tuck int, selected map[int]struct{}) int {
	if i == n-1 {
		return 1 + FaceWidth + 1
	}
	if _, ok := selected[i]; ok {
		return 1 + FaceWidth + 1
	}
	return 1 + tuck
}

// RenderFan draws cards overlapping left to right, the way a hand sits in a player's
// grip: each card covers all but tuck columns of the one before it, and only the
// rightmost card shows its closing edge.
//
// selected is the index to draw as picked out, or -1 for none. That card keeps its own
// right border and is drawn in the selection colour, so it reads as lying on top of the
// fan. There is no vertical lift: raising one card would break the shared top edge that
// makes the rest read as a single hand.
func RenderFan(t styles.Theme, cards []deck.Card, selected, tuck int) string {
	sel := map[int]struct{}{}
	if selected >= 0 {
		sel[selected] = struct{}{}
	}
	return renderFanCore(t, cards, sel, tuck)
}

// RenderFanMulti is RenderFan for multi-select: every index in selected is drawn as
// picked out. Used by Hearts' pass phase.
func RenderFanMulti(t styles.Theme, cards []deck.Card, selected map[int]struct{}, tuck int) string {
	return renderFanCore(t, cards, selected, tuck)
}

func renderFanCore(t styles.Theme, cards []deck.Card, selected map[int]struct{}, tuck int) string {
	if len(cards) == 0 {
		return ""
	}

	rows := make([]strings.Builder, cardRows)
	for i, card := range cards {
		_, isSel := selected[i]
		// A card is closed when nothing covers it: the rightmost one, and any picked-out
		// one, which sits over its neighbour.
		closed := i == len(cards)-1 || isSel

		width := tuck
		if closed {
			width = FaceWidth
		}

		for r, line := range fanColumn(t, card, isSel, width, closed) {
			rows[r].WriteString(line)
		}
	}

	lines := make([]string, cardRows)
	for i := range rows {
		lines[i] = rows[i].String()
	}
	return strings.Join(lines, "\n")
}

// stripCellWidth is one card in the compact strip: a marker column, the rank label
// and the suit. Fixed so the cells line up in a grid however the ranks vary.
const stripCellWidth = 4

// RenderStrip is the hand written out as rank-and-suit cells, wrapped to maxWidth.
//
// It is what a hand falls back to when no fan fits at all: thirteen cards of art run
// past a hundred columns and a terminal is admitted at MinWidth, so a Hearts hand on a
// standard-width terminal has nowhere to be drawn. selected marks staged cards and
// cursor the focused one; pass a nil map for single selection.
func RenderStrip(t styles.Theme, cards []deck.Card, selected map[int]struct{}, cursor, maxWidth int) string {
	if len(cards) == 0 {
		return ""
	}
	perRow := len(cards)
	if maxWidth > 0 {
		perRow = max(maxWidth/stripCellWidth, 1)
	}

	rows := make([]string, 0, len(cards)/perRow+1)
	var row strings.Builder
	for i, card := range cards {
		if i > 0 && i%perRow == 0 {
			rows = append(rows, row.String())
			row.Reset()
		}
		row.WriteString(stripCell(t, card, i, selected, cursor))
	}
	return strings.Join(append(rows, row.String()), "\n")
}

func stripCell(t styles.Theme, card deck.Card, i int, selected map[int]struct{}, cursor int) string {
	suit, style := suitStyle(t, card.Suit)
	marker := " "
	if _, ok := selected[i]; ok {
		marker = "*"
		style = t.PlayerItemSelected.Bold(true)
	} else if i == cursor {
		marker = ">"
		style = t.PlayerItemSelected
	}
	return style.Render(fmt.Sprintf("%s%2s%s", marker, RankLabel(card.Rank), suit))
}

// columnKey is everything a fan column depends on.
type columnKey struct {
	rank     deck.Rank
	suit     deck.Suit
	width    int
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
	key := columnKey{
		rank: card.Rank, suit: card.Suit, width: width,
		selected: selected, closed: closed, dark: t.Dark,
	}
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
