package components

import (
	"strings"
	"sync"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"

	lg "charm.land/lipgloss/v2"
)

// A face is nine columns by seven rows inside its border: a rank line, five rows of
// pips, and the rank again mirrored. Those are the proportions of a real card, and the
// reason a fanned hand reads as a hand rather than a row of boxes.
const (
	FaceWidth  = 9
	FaceHeight = 7
	pipRows    = 5
)

// pipLayout says which of the three columns carry a pip on each of the five rows.
// This is how cards are actually printed: the eye counts a seven by its shape long
// before it reads the corner, so the pips go where a player expects them.
var pipLayout = map[deck.Rank][pipRows]string{
	deck.Ace:   {"", "", "C", "", ""},
	deck.Two:   {"C", "", "", "", "C"},
	deck.Three: {"C", "", "C", "", "C"},
	deck.Four:  {"LR", "", "", "", "LR"},
	deck.Five:  {"LR", "", "C", "", "LR"},
	deck.Six:   {"LR", "", "LR", "", "LR"},
	deck.Seven: {"LR", "", "LR", "C", "LR"},
	deck.Eight: {"LR", "C", "LR", "C", "LR"},
	deck.Nine:  {"LR", "LR", "C", "LR", "LR"},
	deck.Ten:   {"LCR", "C", "LR", "C", "LCR"},
}

// suitMark is where the suit pip belongs inside a court card's art. It sits on the
// centre column, the same one a numbered card puts its middle pip on.
const suitMark = '@'

// courtArt gives the face cards a portrait rather than a letter in a box. Every row is
// exactly FaceWidth columns wide and suitMark is replaced by the suit.
var courtArt = map[deck.Rank][pipRows]string{
	deck.King: {
		"      WW ",
		"      {) ",
		"    @ %% ",
		"     %%% ",
		"   _%%%> ",
	},
	deck.Queen: {
		"      ww ",
		"      {( ",
		"    @ %% ",
		"     %%% ",
		"   _%%%O ",
	},
	deck.Jack: {
		"      ww ",
		"      {) ",
		"    @  % ",
		"       % ",
		"   __%%[ ",
	},
	deck.Joker: {
		"     \\o/ ",
		"      |  ",
		"    @/^\\ ",
		"     %%% ",
		"   _%%%* ",
	},
}

// cardKey is everything a rendered card depends on. The palette collapses to one bool
// because NewTheme is the only way to build a Theme and every colour in it is a pure
// function of Dark, which TestRenderCard_CacheKeyCoversTheWholePalette pins.
type cardKey struct {
	rank     deck.Rank
	suit     deck.Suit
	selected bool
	dark     bool
}

// cardCache memoises rendered cards. A face is a bordered box of placed lines, which
// is the lipgloss whitespace work that dominates a frame, and there are only a few
// hundred distinct faces.
var cardCache sync.Map // cardKey -> string

func RenderCard(t styles.Theme, card deck.Card, selected bool) string {
	key := cardKey{rank: card.Rank, suit: card.Suit, selected: selected, dark: t.Dark}
	if cached, ok := cardCache.Load(key); ok {
		rendered, _ := cached.(string)
		return rendered
	}
	rendered := renderCard(t, card, selected)
	cardCache.Store(key, rendered)
	return rendered
}

func renderCard(t styles.Theme, card deck.Card, selected bool) string {
	style := lg.NewStyle().Border(lg.RoundedBorder()).BorderForeground(t.CardFace)
	if selected {
		// A selected card is lifted out of the hand as well as recoloured, so the
		// selection survives a viewer who cannot tell the border colours apart.
		style = style.BorderForeground(t.Selection).MarginBottom(1)
	} else {
		style = style.MarginTop(1)
	}
	return style.Render(strings.Join(FaceLines(t, card), "\n"))
}

// FaceLines renders the seven inner rows of a face, styled but unbordered, so a caller
// can frame one card or overlap several into a fan.
func FaceLines(t styles.Theme, card deck.Card) []string {
	suit, suitStyle := suitStyle(t, card.Suit)
	lines := make([]string, 0, FaceHeight)
	for _, row := range FaceCells(card, suit) {
		lines = append(lines, suitStyle.Render(strings.Join(row, "")))
	}
	return lines
}

// FaceCells builds the face as single-column cells. Cells rather than one string per
// row because a fan has to cut a card off mid-face, and slicing a styled string by
// display column is a good way to cut an escape sequence in half.
func FaceCells(card deck.Card, suit string) [][]string {
	rank := rankLabel(card.Rank)
	rows := make([][]string, 0, FaceHeight)
	rows = append(rows, rankRow(rank, false))
	rows = append(rows, artRows(card.Rank, suit)...)
	rows = append(rows, rankRow(rank, true))
	return rows
}

// rankRow puts the rank in a corner: top-left, or bottom-right when mirrored, the way
// a real card reads the same upside down.
func rankRow(rank string, mirrored bool) []string {
	cells := blankRow()
	glyphs := strings.Split(rank, "")
	for i, g := range glyphs {
		if mirrored {
			cells[FaceWidth-len(glyphs)+i] = g
			continue
		}
		cells[i] = g
	}
	return cells
}

func artRows(rank deck.Rank, suit string) [][]string {
	if art, ok := courtArt[rank]; ok {
		rows := make([][]string, 0, pipRows)
		for _, line := range art {
			rows = append(rows, artRow(line, suit))
		}
		return rows
	}

	rows := make([][]string, 0, pipRows)
	for _, cols := range pipLayout[rank] {
		rows = append(rows, pipRow(cols, suit))
	}
	return rows
}

func artRow(line, suit string) []string {
	cells := blankRow()
	for i, r := range []rune(line) {
		if i >= FaceWidth {
			break
		}
		if r == suitMark {
			cells[i] = suit
			continue
		}
		cells[i] = string(r)
	}
	return cells
}

// The three pip columns. A court card's suit shares the centre one, so a face card and
// a numbered card put their middle mark in the same place.
const (
	leftColumn = 2
	// CentreColumn is exported so the court art can be held to it by test.
	CentreColumn = 4
	rightColumn  = 6
)

// pipRow places pips at the three columns, so a pip lands in the same place on every
// card in the hand.
func pipRow(cols, suit string) []string {
	cells := blankRow()
	for _, c := range cols {
		switch c {
		case 'L':
			cells[leftColumn] = suit
		case 'C':
			cells[CentreColumn] = suit
		case 'R':
			cells[rightColumn] = suit
		}
	}
	return cells
}

func blankRow() []string {
	cells := make([]string, FaceWidth)
	for i := range cells {
		cells[i] = " "
	}
	return cells
}

// SuitGlyph is the one-column symbol for a suit, and the style it is drawn in.
func SuitGlyph(t styles.Theme, suit deck.Suit) (string, lg.Style) {
	return suitStyle(t, suit)
}

func suitStyle(t styles.Theme, suit deck.Suit) (string, lg.Style) {
	switch suit {
	case deck.Hearts:
		// The text-presentation selector keeps the heart one column wide; without it
		// some terminals draw it as a double-width emoji and the whole grid shifts.
		return "♥\ufe0e", lg.NewStyle().Foreground(t.SuitRed)
	case deck.Diamonds:
		return "♦", lg.NewStyle().Foreground(t.SuitRed)
	case deck.Clubs:
		return "♣", lg.NewStyle().Foreground(t.SuitDark)
	case deck.Spades:
		return "♠", lg.NewStyle().Foreground(t.SuitDark)
	default:
		return " ", lg.NewStyle()
	}
}

func rankLabel(rank deck.Rank) string {
	switch rank {
	case deck.Ace:
		return "A"
	case deck.Two:
		return "2"
	case deck.Three:
		return "3"
	case deck.Four:
		return "4"
	case deck.Five:
		return "5"
	case deck.Six:
		return "6"
	case deck.Seven:
		return "7"
	case deck.Eight:
		return "8"
	case deck.Nine:
		return "9"
	case deck.Ten:
		return "10"
	case deck.Jack:
		return "J"
	case deck.Queen:
		return "Q"
	case deck.King:
		return "K"
	case deck.Joker:
		return "Jk"
	default:
		return ""
	}
}
