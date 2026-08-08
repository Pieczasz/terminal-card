package components

import (
	"strings"
	"sync"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"

	lg "charm.land/lipgloss/v2"
)

// cardKey is everything a rendered card depends on. The palette collapses to one
// bool because NewTheme is the only way to build a Theme and every colour in it is a
// pure function of Dark, which TestRenderCard_CacheKeyCoversTheWholePalette pins.
type cardKey struct {
	rank     deck.Rank
	suit     deck.Suit
	selected bool
	dark     bool
}

// cardCache memoises rendered cards. A card is a bordered box with three placed
// lines, which is exactly the lipgloss whitespace work that dominates a frame - and
// there are only 53 ranks x 4 suits x selected x dark of them, so the whole space is
// a few hundred short strings that never change. Every seat re-renders its cards on
// every clock tick, so this is the difference between paying for them once and
// paying for them ten times a second.
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
	suitStr, style := suitStyle(t, card.Suit)
	cleanRank := strings.TrimSpace(getRankStr(card.Rank))

	topRank := style.Render(lg.Place(7, 1, lg.Left, lg.Center, cleanRank))
	centerSuit := style.Render(lg.Place(7, 1, lg.Center, lg.Center, suitStr))
	bottomRank := style.Render(lg.Place(7, 1, lg.Right, lg.Center, cleanRank))

	inner := lg.JoinVertical(lg.Left,
		topRank,
		"",
		centerSuit,
		"",
		bottomRank,
	)

	cardStyle := lg.NewStyle().
		Border(lg.RoundedBorder()).
		BorderForeground(t.CardFace).
		Padding(0, 1)

	// A selected card is lifted out of the hand as well as recoloured, so the
	// selection survives a viewer who cannot distinguish the border colour.
	if selected {
		cardStyle = cardStyle.BorderForeground(t.Selection).MarginTop(0).MarginBottom(1)
	} else {
		cardStyle = cardStyle.MarginTop(1)
	}

	return cardStyle.Render(inner)
}

func suitStyle(t styles.Theme, suit deck.Suit) (string, lg.Style) {
	switch suit {
	case deck.Hearts:
		return "♥︎", lg.NewStyle().Foreground(t.SuitRed)
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

func getRankStr(rank deck.Rank) string {
	switch rank {
	case deck.Ace:
		return "A "
	case deck.Two:
		return "2 "
	case deck.Three:
		return "3 "
	case deck.Four:
		return "4 "
	case deck.Five:
		return "5 "
	case deck.Six:
		return "6 "
	case deck.Seven:
		return "7 "
	case deck.Eight:
		return "8 "
	case deck.Nine:
		return "9 "
	case deck.Ten:
		return "10"
	case deck.Jack:
		return "J "
	case deck.Queen:
		return "Q "
	case deck.King:
		return "K "
	case deck.Joker:
		return "Jk"
	default:
		return "  "
	}
}
