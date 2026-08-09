package poker

import (
	"slices"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/poker"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
)

// Seat is one player position around the table for rendering.
type Seat struct {
	PlayerID string
	Name     string
	Chips    uint
	Bet      uint
	Folded   bool
	AllIn    bool
	IsDealer bool
	IsSB     bool
	IsBB     bool
	IsTurn   bool
	IsHero   bool
	HandSize int
	Hole     []deck.Card
}

type Model struct {
	gameview.Session

	seats      []Seat
	board      []deck.Card
	pot        uint
	sidePots   int
	street     string
	currentBet uint
	toCall     uint
	minRaise   uint
	myChips    uint
	handDone   bool
	matchDone  bool
	handNumber int
	handsTotal int
	winnerName string
	lastErr    error

	raising     bool
	raiseAmount uint
}

// New creates a Hold'em TUI view bound to the session player.
func New(global router.GlobalContext, engine *game.Engine) tea.Model {
	session, err := gameview.NewSession(global, engine, "poker")
	m := &Model{Session: session, lastErr: err}
	m.syncState()
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(m.Listen(), gameview.ClockTick())
}

func (m *Model) syncState() {
	m.SyncBase()
	m.seats = nil
	m.board = nil
	m.pot = 0
	m.sidePots = 0
	m.street = ""
	m.currentBet = 0
	m.toCall = 0
	m.minRaise = 0
	m.myChips = 0
	m.handDone = false
	m.matchDone = m.Base.Phase == game.Finished
	m.handNumber = 0
	m.handsTotal = 0
	m.winnerName = m.Base.Winner

	if m.Bound == nil || m.Bound.Engine() == nil {
		return
	}

	heroID := m.Bound.PlayerID()

	// One acquisition for the whole frame. Poker is the documented exception that
	// reaches past BoundEngine into whole-table state, because rendering a table
	// means rendering every seat - and since it holds that lock anyway, the betting
	// scalars are read under it too. Splitting them across a WithHiddenState call
	// and this one let an opponent act in between, so the pot could render a bet
	// short of the seat that had already posted it, and the action bar could offer
	// a call the engine had already moved past.
	m.Bound.Engine().WithState(func(state *game.State) {
		e, ok := state.Extra.(*logic.State)
		if !ok || e == nil {
			return
		}
		m.pot = e.MainPool
		m.sidePots = len(e.Pots)
		m.street = e.Phase.String()
		m.currentBet = e.CurrentBet
		m.minRaise = e.MinRaise
		m.toCall = logic.ToCall(e, heroID)
		m.myChips = e.PlayerChips[heroID]
		m.handDone = e.HandComplete || m.Base.Phase == game.Finished
		m.matchDone = e.MatchComplete || m.Base.Phase == game.Finished
		m.handNumber = e.HandNumber
		m.handsTotal = e.HandsTotal
		// Winners holds whoever took the last pot; the match itself is won by the
		// biggest stack, which is the winner the engine settles on.
		if len(e.Winners) > 0 && !m.matchDone {
			m.winnerName = e.Winners[0].DisplayName()
		}

		m.board = append(m.board, e.Table...)

		m.seats = buildSeats(state, e, heroID)
	})

	// A half-built raise belongs to the hero's turn: once the action has moved on,
	// whether by folding, a timeout or the hand ending, the prompt goes with it.
	if !m.Base.MyTurn {
		m.raising = false
	}
	if m.raising {
		m.raiseAmount = m.clampRaise(m.raiseAmount)
	}
}

// buildSeats snapshots every seat for rendering. Hole cards are copied out only
// for the hero, or for anyone still live once the hand is shown down - everyone
// else gets a hand size and nothing more. A pot that nobody contested is won
// face-down: with hands left to play, showing those cards would hand the table a
// free read. Caller must hold the state lock.
func buildSeats(state *game.State, extra *logic.State, heroID string) []Seat {
	reveal := extra.ReachedShowdown || state.Phase == game.Finished

	seats := make([]Seat, 0, len(state.Players))
	for i, p := range state.Players {
		if p == nil {
			continue
		}
		s := Seat{
			PlayerID: p.ID,
			Name:     p.DisplayName(),
			Chips:    extra.PlayerChips[p.ID],
			Bet:      extra.PlayerBets[p.ID],
			Folded:   extra.Folded[p.ID],
			AllIn:    extra.PlayersAllIn[p.ID],
			IsDealer: i == extra.DealerIndex,
			IsSB:     i == extra.SBIndex,
			IsBB:     i == extra.BBIndex,
			IsTurn:   state.Phase == game.Playing && state.CurrentTurn == i && !extra.HandComplete,
			IsHero:   p.ID == heroID,
			HandSize: len(p.Cards),
		}
		if s.IsHero || (reveal && !s.Folded) {
			s.Hole = slices.Clone(p.Cards)
		}
		seats = append(seats, s)
	}
	return seats
}

// clampRaise holds a raise-to amount within [minimum legal raise, hero's stack].
// The stack bound is applied last so a hero who cannot cover the minimum raise
// is offered their whole stack rather than an amount they don't have.
func (m *Model) clampRaise(amount uint) uint {
	return min(max(amount, m.currentBet+m.minRaise), m.streetBetMax())
}

func (m *Model) streetBetMax() uint {
	hero := m.heroSeat()
	if hero == nil {
		return 0
	}
	return hero.Bet + hero.Chips
}

func (m *Model) heroSeat() *Seat {
	for i := range m.seats {
		if m.seats[i].IsHero {
			return &m.seats[i]
		}
	}
	return nil
}

func (m *Model) canCheck() bool {
	return m.Base.MyTurn && m.toCall == 0 && !m.handDone
}

func (m *Model) canCall() bool {
	return m.Base.MyTurn && m.toCall > 0 && !m.handDone
}

func (m *Model) canRaise() bool {
	if !m.Base.MyTurn || m.handDone {
		return false
	}
	hero := m.heroSeat()
	if hero == nil || hero.Chips == 0 {
		return false
	}
	minTo := m.currentBet + m.minRaise
	return hero.Bet+hero.Chips > m.currentBet && hero.Bet+hero.Chips >= minTo
}

func (m *Model) canAllIn() bool {
	hero := m.heroSeat()
	return m.Base.MyTurn && !m.handDone && hero != nil && hero.Chips > 0
}

func (m *Model) canFold() bool {
	return m.Base.MyTurn && !m.handDone
}

// canDeal reports whether the hero is the one holding the button between hands,
// and so the one who deals the next one.
func (m *Model) canDeal() bool {
	return m.handDone && !m.matchDone && m.Base.MyTurn
}

// heroBusted reports whether the hero has lost their stack. They keep their seat
// so the remaining players' pots and standings stay intact, but they cannot act
// or deal for the rest of the match.
func (m *Model) heroBusted() bool {
	hero := m.heroSeat()
	return hero != nil && hero.Chips == 0
}
