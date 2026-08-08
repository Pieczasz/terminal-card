package game

import (
	"fmt"
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/tui/components"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"
	"github.com/Pieczasz/terminal-card/internal/tui/views"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

func RenderHand(t styles.Theme, hand []deck.Card, selectedIdx int, selectionLift float64, disableSelection bool) string {
	renderedCards := make([]string, 0, len(hand))
	for i, c := range hand {
		isSelected := i == selectedIdx && !disableSelection
		var lift int
		if isSelected {
			lift = max(int(math.Round(selectionLift)), 0)
		}

		cardView := components.RenderCard(t, c, isSelected)
		if i < 10 {
			numStyle := t.Dim
			if isSelected {
				numStyle = t.PlayerItemSelected.Bold(true)
			}
			numView := numStyle.Render(strconv.Itoa(i))
			cardView = lg.JoinVertical(lg.Center, cardView, numView)
		}

		maxLift := 2
		if lift > maxLift {
			lift = maxLift
		}

		cardView = lg.NewStyle().
			MarginTop(maxLift - lift).
			MarginBottom(lift).
			Render(cardView)

		renderedCards = append(renderedCards, cardView)
	}
	return lg.JoinHorizontal(lg.Top, renderedCards...)
}

type Orientation int

const (
	OrientationTop Orientation = iota
	OrientationLeft
	OrientationRight
)

func renderTopCards(t styles.Theme, count int) string {
	if count <= 0 {
		return ""
	}

	botLine := lg.NewStyle().Foreground(t.CardFace).Render("╰" + strings.Repeat("┴", count-1) + "───────╯")

	edge := lg.NewStyle().Foreground(t.CardFace).Render("│" + strings.Repeat("│", count-1))
	body := lg.NewStyle().Foreground(t.CardBack).Render("░░░░░░░")
	rightEdge := lg.NewStyle().Foreground(t.CardFace).Render("│")

	midLine := edge + body + rightEdge

	var sb strings.Builder
	sb.Grow(len(midLine)*4 + len(botLine) + 5)

	sb.WriteString(midLine)
	sb.WriteByte('\n')
	sb.WriteString(midLine)
	sb.WriteByte('\n')
	sb.WriteString(midLine)
	sb.WriteByte('\n')
	sb.WriteString(midLine)
	sb.WriteByte('\n')
	sb.WriteString(botLine)

	return sb.String()
}

func renderLeftCards(t styles.Theme, count int) string {
	if count <= 0 {
		return ""
	}

	topEdge := lg.NewStyle().Foreground(t.CardFace).Render("─────╮")
	midEdge := lg.NewStyle().Foreground(t.CardFace).Render("─────┤")
	botEdge := lg.NewStyle().Foreground(t.CardFace).Render("─────╯")
	cardBody := lg.NewStyle().Foreground(t.CardBack).Render("░░░░░") + lg.NewStyle().Foreground(t.CardFace).Render("│")

	return buildVerticalCardsString(count, topEdge, midEdge, botEdge, cardBody)
}

func renderRightCards(t styles.Theme, count int) string {
	if count <= 0 {
		return ""
	}

	topEdge := lg.NewStyle().Foreground(t.CardFace).Render("╭─────")
	midEdge := lg.NewStyle().Foreground(t.CardFace).Render("├─────")
	botEdge := lg.NewStyle().Foreground(t.CardFace).Render("╰─────")
	cardBody := lg.NewStyle().Foreground(t.CardFace).Render("│") + lg.NewStyle().Foreground(t.CardBack).Render("░░░░░")

	return buildVerticalCardsString(count, topEdge, midEdge, botEdge, cardBody)
}

func buildVerticalCardsString(count int, topEdge, midEdge, botEdge, cardBody string) string {
	var sb strings.Builder
	sb.Grow((count + 3) * 20)

	sb.WriteString(topEdge)
	sb.WriteByte('\n')
	for range count - 1 {
		sb.WriteString(midEdge)
		sb.WriteByte('\n')
	}
	sb.WriteString(cardBody)
	sb.WriteByte('\n')
	sb.WriteString(cardBody)
	sb.WriteByte('\n')
	sb.WriteString(cardBody)
	sb.WriteByte('\n')
	sb.WriteString(cardBody)
	sb.WriteByte('\n')
	sb.WriteString(botEdge)

	return sb.String()
}

func RenderOpponent(t styles.Theme, o game.PlayerSnapshot, isCurrentTurn bool, orientation Orientation, remaining time.Duration) string {
	nameStyle := t.SectionHeading
	if isCurrentTurn {
		nameStyle = t.TurnName
	}
	nameView := nameStyle.Render(o.Username)
	cardsCountView := t.Muted.Render(fmt.Sprintf("[%d cards]", o.HandSize))

	infoView := lg.JoinVertical(lg.Center, nameView, cardsCountView)

	var cardsView string
	switch orientation {
	case OrientationTop:
		cardsView = renderTopCards(t, o.HandSize)
	case OrientationLeft:
		cardsView = renderLeftCards(t, o.HandSize)
	case OrientationRight:
		cardsView = renderRightCards(t, o.HandSize)
	}

	var block string
	switch orientation {
	case OrientationTop:
		block = lg.JoinVertical(lg.Center, cardsView, infoView)
	case OrientationLeft:
		block = lg.JoinVertical(lg.Left, infoView, cardsView)
	default:
		block = lg.JoinVertical(lg.Right, infoView, cardsView)
	}

	if !isCurrentTurn {
		return block
	}
	return AttachTurnClock(block, RenderTurnClock(t, remaining, false), orientation)
}

func RenderOpponentMinimal(t styles.Theme, o game.PlayerSnapshot, isCurrentTurn bool) string {
	nameStyle := t.SectionHeading
	if isCurrentTurn {
		nameStyle = t.TurnName
	}
	nameView := nameStyle.Render(o.Username)
	cardsCountView := t.Muted.Render(fmt.Sprintf("[%d cards]", o.HandSize))

	return lg.JoinHorizontal(lg.Center, nameView, " ", cardsCountView)
}

// RenderCardBacks draws count face-down card backs oriented for a table edge,
// so every game view shows opponents' hidden cards the same way.
func RenderCardBacks(t styles.Theme, count int, orientation Orientation) string {
	switch orientation {
	case OrientationLeft:
		return renderLeftCards(t, count)
	case OrientationRight:
		return renderRightCards(t, count)
	default:
		return renderTopCards(t, count)
	}
}

// RenderStatus names whose turn it is. The countdown itself is not here: it is drawn
// on the seat it belongs to, so a player reads the number next to the cards rather
// than having to look somewhere else on the table.
func RenderStatus(t styles.Theme, currentPlayer string, isMyTurn bool) string {
	statusStyle := t.Dim.MarginTop(1).MarginBottom(1)
	statusStr := fmt.Sprintf("Current turn: %s", currentPlayer)
	if isMyTurn {
		statusStyle = statusStyle.Foreground(t.Success).Bold(true)
		statusStr = "> YOUR TURN <"
	}
	return statusStyle.Render(statusStr)
}

// preciseClockThreshold is when the countdown starts counting in tenths. Whole seconds
// hide up to a second of the time a player actually has, and in the last few seconds
// that is the difference between acting and being folded for them.
const preciseClockThreshold = 6 * time.Second

// tenthTickInterval is the tick rate while the countdown is showing tenths. It is only
// paid for the last seconds of a turn: ten renders a second, for every client at the
// table, is not something to run for a whole thirty-second turn over SSH.
const tenthTickInterval = 100 * time.Millisecond

// FormatTurnClock renders the turn countdown, or empty when no clock is running.
//
// precise asks for tenths (5.5, 5.4, ...) below preciseClockThreshold; without it the
// clock stays in m:ss the whole way down. Only the player who has to act needs the
// finer reading, and it is not free: tenths have to be driven by a tick ten times a
// second, and every client at the table paying that for somebody else's clock is the
// most expensive thing a table does. Both forms round up, so the display never claims
// less time than the player has and never reads zero while they can still act.
func FormatTurnClock(remaining time.Duration, precise bool) string {
	if remaining <= 0 {
		return ""
	}
	if precise && remaining < preciseClockThreshold {
		tenths := int(math.Ceil(remaining.Seconds() * 10))
		return fmt.Sprintf("%d.%d", tenths/10, tenths%10)
	}
	secs := int((remaining + time.Second - 1) / time.Second)
	return fmt.Sprintf("%d:%02d", secs/60, secs%60)
}

// RenderTurnClock is the countdown as drawn on the seat that is on turn. It turns
// urgent inside the last seconds whoever is reading it, and counts in tenths only for
// the player those seconds belong to.
func RenderTurnClock(t styles.Theme, remaining time.Duration, precise bool) string {
	clock := FormatTurnClock(remaining, precise)
	if clock == "" {
		return ""
	}
	if remaining < preciseClockThreshold {
		return t.ErrorText.Bold(true).Render(clock)
	}
	return t.Muted.Render(clock)
}

// AttachTurnClock places the countdown against a seat block: underneath for the seats
// along the top and bottom, and alongside for the ones stacked down the sides, where
// another row would push the stack apart. The clock sits toward the middle of the
// table on both sides. An empty clock leaves the block untouched.
func AttachTurnClock(block, clock string, orientation Orientation) string {
	if clock == "" {
		return block
	}
	switch orientation {
	case OrientationLeft:
		return lg.JoinHorizontal(lg.Center, block, " ", clock)
	case OrientationRight:
		return lg.JoinHorizontal(lg.Center, clock, " ", block)
	case OrientationTop:
	}
	return lg.JoinVertical(lg.Center, block, clock)
}

// ClockTickMsg drives the turn countdown.
type ClockTickMsg time.Time

// ClockTickFor schedules the next countdown tick at the rate the current reading
// needs: once a second while the clock shows whole seconds, ten times a second only
// for the player whose clock is running out.
//
// onTurn is what keeps a table cheap. A frame costs the server thousands of
// allocations, so every seat re-rendering ten times a second because one player is
// running late would cost the table six times its own worth of work, for a digit
// nobody but that player is acting on.
func ClockTickFor(remaining time.Duration, onTurn bool) tea.Cmd {
	interval := time.Second
	if onTurn && remaining > 0 && remaining < preciseClockThreshold {
		interval = tenthTickInterval
	}
	return tea.Tick(interval, func(t time.Time) tea.Msg { return ClockTickMsg(t) })
}

// ClockTick starts the countdown before any deadline is known, which is what a view's
// Init has to work with.
func ClockTick() tea.Cmd { return ClockTickFor(0, false) }

func RenderWaitingScreen(g router.GlobalContext, phase game.Phase, winner string) string {
	innerWidth := styles.InnerWidth(g.Width)
	titleFig := styles.RenderFigureASCII("Active Game", innerWidth)
	header := g.Theme.Title.Render(titleFig)
	footer := g.Theme.RenderActionFooter(styles.GlobalActions)

	var content string
	if phase == game.Finished {
		content = fmt.Sprintf("Game Over! Winner: %s\n\nPress Esc to go back.", winner)
	} else {
		content = "Waiting for game to start..."
	}

	return views.RenderCenteredLayout(g, header, content, footer)
}
