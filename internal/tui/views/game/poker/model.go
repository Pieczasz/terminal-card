package poker

import (
	"fmt"
	"log/slog"
	"slices"

	"github.com/Pieczasz/terminal-card/internal/deck"
	"github.com/Pieczasz/terminal-card/internal/game"
	logic "github.com/Pieczasz/terminal-card/internal/game/poker"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	gameview "github.com/Pieczasz/terminal-card/internal/tui/views/game"

	tea "charm.land/bubbletea/v2"
)

type gameMsg game.Event

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
	global router.GlobalContext
	bound  *game.BoundEngine
	events <-chan game.Event

	baseState gameview.BaseState

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

func listenForEvents(ch <-chan game.Event) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return gameMsg(msg)
	}
}

// New creates a Hold'em TUI view bound to the session player.
func New(global router.GlobalContext, engine *game.Engine) tea.Model {
	playerID := ""
	if global.User != nil {
		playerID = fmt.Sprint(global.User.ID)
	}
	bound := game.Bind(engine, playerID)

	var ch <-chan game.Event
	var subErr error
	if bound != nil {
		if b := bound.Broadcaster(); b != nil {
			ch, subErr = b.Subscribe()
			if subErr != nil {
				// Without the feed the table would freeze on the current frame while the
				// hand carries on without them, so say so rather than look responsive.
				slog.Error("poker view could not subscribe to game events", "error", subErr, "player_id", playerID)
				subErr = fmt.Errorf("live table updates unavailable, leave and rejoin: %w", subErr)
			}
		}
	}
	m := &Model{
		global:  global,
		bound:   bound,
		events:  ch,
		lastErr: subErr,
	}
	m.syncState()
	return m
}

func (m *Model) Init() tea.Cmd {
	return tea.Batch(listenForEvents(m.events), gameview.ClockTick())
}

func (m *Model) syncState() {
	m.baseState = gameview.SyncBaseState(m.bound)
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
	m.matchDone = m.baseState.Phase == game.Finished
	m.handNumber = 0
	m.handsTotal = 0
	m.winnerName = m.baseState.Winner

	if m.bound == nil || m.bound.Engine() == nil {
		return
	}

	heroID := m.bound.PlayerID()
	m.bound.Engine().WithState(func(state *game.State) {
		extra, ok := state.Extra.(*logic.State)
		if !ok || extra == nil {
			return
		}
		m.pot = extra.MainPool
		m.sidePots = len(extra.Pots)
		m.street = extra.Phase.String()
		m.currentBet = extra.CurrentBet
		m.minRaise = extra.MinRaise
		m.toCall = logic.ToCall(extra, heroID)
		m.myChips = extra.PlayerChips[heroID]
		m.handDone = extra.HandComplete || state.Phase == game.Finished
		m.matchDone = extra.MatchComplete || state.Phase == game.Finished
		m.handNumber = extra.HandNumber
		m.handsTotal = extra.HandsTotal
		// Winners holds whoever took the last pot; the match itself is won by the
		// biggest stack, which is the winner the engine settles on.
		if len(extra.Winners) > 0 && !m.matchDone {
			m.winnerName = extra.Winners[0].Username()
		}

		m.board = append(m.board, extra.Table...)
		m.seats = buildSeats(state, extra, heroID)
	})

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
			Name:     p.Username(),
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
	return m.baseState.MyTurn && m.toCall == 0 && !m.handDone
}

func (m *Model) canCall() bool {
	return m.baseState.MyTurn && m.toCall > 0 && !m.handDone
}

func (m *Model) canRaise() bool {
	if !m.baseState.MyTurn || m.handDone {
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
	return m.baseState.MyTurn && !m.handDone && hero != nil && hero.Chips > 0
}

func (m *Model) canFold() bool {
	return m.baseState.MyTurn && !m.handDone
}

// canDeal reports whether the hero is the one holding the button between hands,
// and so the one who deals the next one.
func (m *Model) canDeal() bool {
	return m.handDone && !m.matchDone && m.baseState.MyTurn
}

// heroBusted reports whether the hero has lost their stack. They keep their seat
// so the remaining players' pots and standings stay intact, but they cannot act
// or deal for the rest of the match.
func (m *Model) heroBusted() bool {
	hero := m.heroSeat()
	return hero != nil && hero.Chips == 0
}
