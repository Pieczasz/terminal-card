package components

import (
	"strings"
	"terminalcard/internal/deck"

	lg "github.com/charmbracelet/lipgloss"
)

func getSuitInfo(suit deck.Suit) (string, lg.Style) {
	switch suit {
	case deck.Hearts:
		return "♥︎", lg.NewStyle().Foreground(lg.Color("#FF0000"))
	case deck.Diamonds:
		return "♦", lg.NewStyle().Foreground(lg.Color("#FF0000"))
	case deck.Clubs:
		return "♣", lg.NewStyle().Foreground(lg.Color("#DDDDDD"))
	case deck.Spades:
		return "♠", lg.NewStyle().Foreground(lg.Color("#DDDDDD"))
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

func RenderCard(card deck.Card, selected bool) string {
	suitStr, style := getSuitInfo(card.Suit)
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

	borderStyle := lg.RoundedBorder()
	cardStyle := lg.NewStyle().
		Border(borderStyle).
		BorderForeground(lg.Color("#EEEEEE")).
		Padding(0, 1)

	if selected {
		cardStyle = cardStyle.BorderForeground(lg.Color("#00FF00")).MarginTop(0).MarginBottom(1)
	} else {
		cardStyle = cardStyle.MarginTop(1)
	}

	return cardStyle.Render(inner)
}
