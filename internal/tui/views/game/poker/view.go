package poker

import (
	"fmt"
	"strconv"
	"strings"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/tui/components"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

func (m *Model) View() tea.View {
	if m.handDone || m.matchDone {
		return tea.NewView(m.renderHandOver())
	}
	if m.baseState.Phase != game.Playing {
		return tea.NewView(gameview.RenderWaitingScreen(m.global, m.baseState.Phase, m.winnerName))
	}

	compact := m.global.Height < 30
	zones := m.seatZones()

	top := m.renderTopRow(zones.top, compact)
	bot := m.renderHero(compact)
	midH := max(m.global.Height-lg.Height(top)-lg.Height(bot), 0)
	mid := m.renderMiddle(midH, zones.left, zones.right, compact)

	return tea.NewView(lg.JoinVertical(lg.Left,
		styles.PadCenter(m.global.Width, top),
		mid,
		styles.PadCenter(m.global.Width, bot),
	))
}

// seatNameWidth is the name column on the results screen. Usernames run to 16
// characters, so it has to elide rather than let a long name shove the chip
// columns out of line.
const seatNameWidth = 12

func (m *Model) renderHandOver() string {
	compact := m.global.Height < 30
	board := m.renderBoard(compact)

	title := m.global.Theme.Accented.Render(fmt.Sprintf("HAND %d/%d COMPLETE", m.handNumber, m.handsTotal))
	winner := m.global.Theme.Accented.Render(m.winnerName + " wins the hand")
	if m.matchDone {
		title = m.global.Theme.Accented.Render("MATCH COMPLETE")
		winner = m.global.Theme.Accented.Render(m.winnerName + " wins")
	}

	seatLines := make([]string, 0, len(m.seats))
	for _, s := range m.seats {
		// Chip glyphs carry their own colour, so they are joined in rather than
		// rendered through m.global.Theme.Muted.
		line := m.global.Theme.Muted.Render(fmt.Sprintf("%s %6d  ", styles.PadTruncate(s.Name, seatNameWidth), s.Chips)) +
			renderChipStack(m.global.Theme, s.Chips)
		if s.Folded {
			line += m.global.Theme.Muted.Render("  folded")
		}
		if len(s.Hole) == 2 {
			line += "  " + renderMiniCard(m.global.Theme, s.Hole[0]) + renderMiniCard(m.global.Theme, s.Hole[1])
		}
		seatLines = append(seatLines, line)
	}

	content := lg.JoinVertical(lg.Center,
		title, winner, "", board, "",
		lg.JoinVertical(lg.Left, seatLines...),
		"", m.global.Theme.Dim.Render(m.handOverHint()),
	)
	return styles.Place(m.global.Width, m.global.Height, lg.Center, lg.Center, content)
}

// handOverHint spells out that esc leaves the whole match. The screen looks like
// the end of a game, but with hands still to play esc forfeits the stack the
// player just spent them building.
func (m *Model) handOverHint() string {
	if m.matchDone {
		return "esc / enter -> lobby"
	}
	leave := "esc: leave the match, forfeiting your chips"

	var next string
	switch {
	case m.heroBusted():
		next = "out of chips - watching until the match ends"
	case m.canDeal():
		next = fmt.Sprintf("enter: deal hand %d", m.handNumber+1)
	case m.baseState.CurrentPlayer != "":
		next = "waiting for " + m.baseState.CurrentPlayer + " to deal hand " + strconv.Itoa(m.handNumber+1)
	default:
		next = "waiting for the next hand"
	}
	return next + "   |   " + leave
}

type seatZones struct {
	top   []Seat
	left  []Seat
	right []Seat
}

// seatZones places opponents clockwise from hero's left: top ≤4, left ≤2, right ≤2.
func (m *Model) seatZones() seatZones {
	heroIdx := -1
	for i, s := range m.seats {
		if s.IsHero {
			heroIdx = i
			break
		}
	}
	var opps []Seat
	if heroIdx < 0 {
		opps = append(opps, m.seats...)
	} else {
		n := len(m.seats)
		for i := 1; i < n; i++ {
			opps = append(opps, m.seats[(heroIdx+i)%n])
		}
	}

	n := len(opps)
	var z seatZones
	switch n {
	case 0:
		return z
	case 1:
		z.top = opps
	case 2:
		z.left = opps[:1]
		z.right = opps[1:]
	case 3:
		z.left = opps[:1]
		z.top = opps[1:2]
		z.right = opps[2:]
	case 4:
		z.left = opps[:1]
		z.top = opps[1:3]
		z.right = opps[3:]
	case 5:
		z.left = opps[:1]
		z.top = opps[1:4]
		z.right = opps[4:]
	case 6:
		z.left = opps[:1]
		z.top = opps[1:5]
		z.right = opps[5:]
	case 7:
		z.left = opps[:2]
		z.top = opps[2:5]
		z.right = opps[5:]
	default: // 8+
		z.left = opps[:2]
		z.top = opps[2:6]
		z.right = opps[6:min(8, n)]
	}
	return z
}

func (m *Model) renderTopRow(seats []Seat, compact bool) string {
	if len(seats) == 0 {
		return ""
	}
	parts := make([]string, 0, len(seats))
	for _, s := range seats {
		parts = append(parts, m.renderSeat(s, compact, gameview.OrientationTop))
	}
	row := lg.JoinHorizontal(lg.Bottom, parts...)
	if !compact {
		return lg.NewStyle().MarginTop(1).Render(row)
	}
	return row
}

func (m *Model) renderMiddle(height int, left, right []Seat, compact bool) string {
	w1 := m.global.Width / 4
	w3 := m.global.Width / 4
	w2 := m.global.Width - w1 - w3

	leftView := m.renderSideStack(left, compact, gameview.OrientationLeft)
	rightView := m.renderSideStack(right, compact, gameview.OrientationRight)
	center := m.renderCenter(compact)

	return lg.JoinHorizontal(lg.Top,
		styles.Place(w1, height, lg.Left, lg.Center, leftView),
		styles.Place(w2, height, lg.Center, lg.Center, center),
		styles.Place(w3, height, lg.Right, lg.Center, rightView),
	)
}

func (m *Model) renderSideStack(seats []Seat, compact bool, orientation gameview.Orientation) string {
	if len(seats) == 0 {
		return ""
	}
	parts := make([]string, 0, len(seats))
	for _, s := range seats {
		parts = append(parts, m.renderSeat(s, compact, orientation))
	}
	return lg.JoinVertical(lg.Center, parts...)
}

func (m *Model) renderCenter(compact bool) string {
	board := m.renderBoard(compact)
	potLine := m.global.Theme.Accented.Render(fmt.Sprintf("POT %d", m.pot))
	if m.sidePots > 1 {
		potLine += m.global.Theme.Muted.Render(fmt.Sprintf("  (%d pots)", m.sidePots))
	}
	street := m.global.Theme.Accented.Render(fmt.Sprintf("%s | hand %d/%d", m.street, m.handNumber, m.handsTotal))
	betLine := m.global.Theme.Muted.Render(fmt.Sprintf("bet %d | to call %d", m.currentBet, m.toCall))

	return lg.JoinVertical(lg.Center, board, "", potLine, renderChipStack(m.global.Theme, m.pot), street, betLine)
}

func (m *Model) renderBoard(compact bool) string {
	slots := make([]string, 5)
	for i := range 5 {
		if i < len(m.board) {
			if compact {
				slots[i] = renderMiniCard(m.global.Theme, m.board[i])
			} else {
				slots[i] = components.RenderCard(m.global.Theme, m.board[i], false)
			}
		} else if compact {
			slots[i] = m.global.Theme.Dim.Render("[  ]")
		} else {
			slots[i] = renderEmptySlot(m.global.Theme)
		}
	}
	return lg.JoinHorizontal(lg.Bottom, slots...)
}

func renderEmptySlot(t styles.Theme) string {
	inner := lg.Place(7, 5, lg.Center, lg.Center, "")
	return lg.NewStyle().
		Border(lg.RoundedBorder()).
		BorderForeground(t.BorderMuted).
		Padding(0, 1).
		MarginTop(1).
		Render(inner)
}

func renderMiniCard(t styles.Theme, c deck.Card) string {
	rank := rankShort(c.Rank)
	suit, style := suitGlyph(t, c.Suit)
	return style.Render(fmt.Sprintf("[%s%s]", rank, suit))
}

func renderHoleBack(t styles.Theme) string {
	return t.Dim.Render("[??]")
}

func (m *Model) renderSeat(s Seat, compact bool, orientation gameview.Orientation) string {
	t := m.global.Theme
	ns := lg.NewStyle().Bold(true).Foreground(t.Accent)
	if s.IsTurn {
		ns = t.TurnName
	} else if s.Folded {
		ns = t.Dim
	}

	badges := seatBadges(s)
	name := ns.Render(s.Name)
	if badges != "" {
		name = lg.JoinHorizontal(lg.Center, name, " ", m.global.Theme.SuccessText.Render(badges))
	}

	stack := m.global.Theme.Muted.Render(strconv.FormatUint(uint64(s.Chips), 10))
	if s.Bet > 0 {
		stack = lg.JoinHorizontal(lg.Center, stack, m.global.Theme.Muted.Render(fmt.Sprintf(" | bet %d", s.Bet)))
	}
	if s.AllIn && !s.Folded {
		stack = lg.JoinHorizontal(lg.Center, stack, " ", m.global.Theme.Accented.Render("ALL-IN"))
	}
	if s.Folded {
		stack = m.global.Theme.Dim.Render("folded")
	}

	cards := m.renderSeatCards(s, compact, orientation)
	rows := []string{cards, name, stack}
	if !compact {
		// A short terminal needs the row for cards more than for decoration.
		rows = append(rows, renderChipStack(m.global.Theme, s.Chips))
	}
	pad := lg.NewStyle().Padding(0, 1)
	block := pad.Render(lg.JoinVertical(lg.Center, rows...))

	// Only the seat on turn has a clock, so this is what tells a player the table is
	// waiting on them without them having to find the status line.
	if !s.IsTurn {
		return block
	}
	// Tenths for the hero's own clock; every other seat reads in whole seconds, which
	// is also what lets those sessions tick once a second instead of ten times.
	clock := gameview.RenderTurnClock(t, m.baseState.TurnRemaining, s.IsHero)
	return gameview.AttachTurnClock(block, clock, orientation)
}

func seatBadges(s Seat) string {
	var b []string
	if s.IsDealer {
		b = append(b, "D")
	}
	if s.IsSB {
		b = append(b, "SB")
	}
	if s.IsBB {
		b = append(b, "BB")
	}
	return strings.Join(b, "/")
}

func (m *Model) renderSeatCards(s Seat, compact bool, orientation gameview.Orientation) string {
	if len(s.Hole) == 2 {
		if compact {
			return lg.JoinHorizontal(lg.Center, renderMiniCard(m.global.Theme, s.Hole[0]), renderMiniCard(m.global.Theme, s.Hole[1]))
		}
		return lg.JoinHorizontal(lg.Bottom,
			components.RenderCard(m.global.Theme, s.Hole[0], false),
			components.RenderCard(m.global.Theme, s.Hole[1], false),
		)
	}
	if compact {
		return lg.JoinHorizontal(lg.Center, renderHoleBack(m.global.Theme), renderHoleBack(m.global.Theme))
	}
	return gameview.RenderCardBacks(m.global.Theme, 2, orientation)
}

func (m *Model) renderHero(compact bool) string {
	hero := m.heroSeat()
	var seatBlock string
	if hero != nil {
		seatBlock = m.renderSeat(*hero, compact, gameview.OrientationTop)
	}

	status := gameview.RenderStatus(m.global.Theme, m.baseState.CurrentPlayer, m.baseState.MyTurn)
	actions := m.renderActionBar()

	parts := []string{seatBlock, status, actions}
	if m.lastErr != nil {
		parts = append(parts, m.global.Theme.ErrorText.Render(m.lastErr.Error()))
	}
	block := lg.JoinVertical(lg.Center, parts...)
	if !compact {
		return lg.NewStyle().MarginBottom(1).Render(block)
	}
	return block
}

func (m *Model) renderActionBar() string {
	if m.raising {
		return m.renderRaisePrompt()
	}
	var opts []string
	if m.canFold() {
		opts = append(opts, "f fold")
	}
	if m.canCheck() {
		opts = append(opts, "c check")
	}
	if m.canCall() {
		opts = append(opts, fmt.Sprintf("c call %d", m.toCall))
	}
	if m.canRaise() {
		opts = append(opts, "r raise")
	}
	if m.canAllIn() {
		opts = append(opts, "a all-in")
	}
	if len(opts) == 0 {
		if m.handDone || m.baseState.Phase == game.Finished {
			return m.global.Theme.Muted.Render("esc -> lobby")
		}
		return m.global.Theme.Muted.Render("waiting…")
	}
	return m.global.Theme.Accented.Render(strings.Join(opts, " | "))
}

// renderRaisePrompt shows the raise being built: the running total, the chips that
// can be pushed onto it, and how far it can still go.
func (m *Model) renderRaisePrompt() string {
	total := m.global.Theme.Accented.Render(fmt.Sprintf("RAISE TO %d", m.raiseAmount))
	bounds := m.global.Theme.Muted.Render(fmt.Sprintf("(min %d, all-in %d)", m.currentBet+m.minRaise, m.streetBetMax()))
	keys := m.global.Theme.Dim.Render("[/] fine  |  enter confirm  |  esc cancel")
	return lg.JoinVertical(lg.Center,
		lg.JoinHorizontal(lg.Center, total, "  ", bounds),
		renderChipRack(m.global.Theme),
		keys,
	)
}

func rankShort(r deck.Rank) string {
	switch r {
	case deck.Ace:
		return "A"
	case deck.King:
		return "K"
	case deck.Queen:
		return "Q"
	case deck.Jack:
		return "J"
	case deck.Ten:
		return "T"
	default:
		return strconv.Itoa(int(r) + 1) // Two=1 -> "2"
	}
}

func suitGlyph(t styles.Theme, s deck.Suit) (string, lg.Style) {
	switch s {
	case deck.Hearts:
		return "♥", lg.NewStyle().Foreground(t.SuitRed)
	case deck.Diamonds:
		return "♦", lg.NewStyle().Foreground(t.SuitRed)
	case deck.Clubs:
		return "♣", lg.NewStyle().Foreground(t.SuitDark)
	case deck.Spades:
		return "♠", lg.NewStyle().Foreground(t.SuitDark)
	default:
		return "?", t.Dim
	}
}
