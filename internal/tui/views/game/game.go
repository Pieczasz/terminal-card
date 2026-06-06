package game

import (
	"client/internal/deck"
	"client/internal/game"
	"client/internal/player"
	"client/internal/tui/components"
	"client/internal/tui/router"
	"client/internal/tui/styles"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	lg "github.com/charmbracelet/lipgloss"
)

type gameMsg game.Event

type model struct {
	global   router.GlobalContext
	engine   *game.Engine
	gameChan <-chan game.Event

	phase           game.Phase
	myTurn          bool
	hand            []deck.Card
	topDiscard      deck.Card
	opponents       []game.PlayerSnapshot
	selectedCardIdx int
	currentPlayer   string
	winner          string
}

func listenToGameBroadcaster(ch <-chan game.Event) tea.Cmd {
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

func New(global router.GlobalContext, engine *game.Engine) tea.Model {
	var ch <-chan game.Event
	if engine != nil {
		ch = engine.Broadcaster().Subscribe()
	}
	m := model{
		global:   global,
		engine:   engine,
		gameChan: ch,
	}
	m.syncState()
	return m
}

func (m *model) syncState() {
	if m.engine == nil {
		return
	}
	m.engine.WithState(func(state *game.State) {
		m.phase = state.Phase
		if m.phase == game.Finished {
			m.winner = state.Winner.DatabaseUser.Username
			return
		}

		if m.phase == game.Waiting {
			return
		}

		m.currentPlayer = state.Players[state.CurrentTurn].DatabaseUser.Username
		m.myTurn = m.currentPlayer == m.global.User.Username

		m.hand = nil
		m.opponents = nil

		for _, p := range state.Players {
			if fmt.Sprint(p.DatabaseUser.ID) == fmt.Sprint(m.global.User.ID) {
				m.hand = p.Cards
			} else {
				m.opponents = append(m.opponents, game.PlayerSnapshot{
					ID:       p.DatabaseUser.Username,
					HandSize: len(p.Cards),
				})
			}
		}

		top, _ := state.Discard.Peak()
		m.topDiscard = top

		// Make sure selected card index is valid
		if m.selectedCardIdx >= len(m.hand) {
			m.selectedCardIdx = max(len(m.hand)-1, 0)
		}
	})
}

func (m model) Init() tea.Cmd {
	return listenToGameBroadcaster(m.gameChan)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.global.Width = msg.Width
		m.global.Height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "esc":
			if m.phase == game.Finished {
				p := &player.Player{Id: fmt.Sprint(m.global.User.ID), DatabaseUser: m.global.User}
				l := m.global.LobbyManager.FindLobbyByPlayer(p)
				return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "lobby", Context: l} }
			}
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "home"} }
		case "left", "h":
			if m.selectedCardIdx > 0 {
				m.selectedCardIdx--
			}
		case "right", "k", "l":
			if m.selectedCardIdx < len(m.hand)-1 {
				m.selectedCardIdx++
			}
		case "0", "1", "2", "3", "4", "5", "6", "7", "8", "9":
			if len(m.hand) > 0 {
				idx := int(msg.String()[0] - '0')
				if idx < len(m.hand) {
					m.selectedCardIdx = idx
				}
			}
		case "enter":
			if m.phase == game.Finished {
				p := &player.Player{Id: fmt.Sprint(m.global.User.ID), DatabaseUser: m.global.User}
				l := m.global.LobbyManager.FindLobbyByPlayer(p)
				return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "lobby", Context: l} }
			}
			if m.myTurn && len(m.hand) > 0 {
				card := m.hand[m.selectedCardIdx]
				err := m.engine.SubmitAction(fmt.Sprint(m.global.User.ID), game.Action{
					Type:  game.ActionPlayCard,
					Cards: []deck.Card{card},
				})
				if err != nil {
					// Could show an error notification here
				}
			}
		case "d":
			if m.myTurn {
				_ = m.engine.SubmitAction(fmt.Sprint(m.global.User.ID), game.Action{
					Type: game.ActionDrawCard,
				})
			}
		}
	case gameMsg:
		m.syncState()
		return m, listenToGameBroadcaster(m.gameChan)
	}
	return m, nil
}

func (m model) View() string {
	if m.phase == game.Playing {
		// Opponents
		var oppStr strings.Builder
		oppStr.WriteString("Opponents:\n")
		for _, o := range m.opponents {
			oppStr.WriteString(fmt.Sprintf(" - %s: %d cards\n", o.ID, o.HandSize))
		}

		// Board
		boardStr := fmt.Sprintf("\nDiscard Pile:\n%s\n", components.RenderCard(m.topDiscard, false))

		// Status
		statusStr := fmt.Sprintf("\nCurrent turn: %s\n", m.currentPlayer)
		if m.myTurn {
			statusStr = "\n> YOUR TURN! <\n"
		}

		// Hand
		handStr := "\nYour hand:\n"
		var renderedCards []string
		for i, c := range m.hand {
			cardView := components.RenderCard(c, i == m.selectedCardIdx)
			if i < 10 {
				numStyle := lg.NewStyle().Foreground(lg.Color("#888888"))
				if i == m.selectedCardIdx {
					numStyle = numStyle.Foreground(lg.Color("205")).Bold(true)
				}
				numView := numStyle.Render(fmt.Sprintf("%d", i))
				cardView = lg.JoinVertical(lg.Center, cardView, numView)
			}
			renderedCards = append(renderedCards, cardView)
		}
		handStr += lg.JoinHorizontal(lg.Top, renderedCards...)
		handStr += "\n"

		content := oppStr.String() + boardStr + statusStr + handStr

		helpers := lg.NewStyle().Foreground(lg.Color("#888888")).Render("←/h: left | →/k: right | enter: play | d: draw | esc: leave")
		content = lg.JoinVertical(lg.Center, content, "", helpers)

		return lg.Place(
			m.global.Width, m.global.Height,
			lg.Center, lg.Center,
			content,
		)
	}

	// Default bounded view for waiting/finished
	innerWidth := styles.GetInnerWidth(m.global.Width)
	titleFig := styles.RenderFigureAscii("Active Game", innerWidth)
	titleText := styles.Title.Render(titleFig)
	header := styles.Title.Render(titleText)
	footer := lg.NewStyle().Render(styles.RenderActionFooter(styles.GlobalActions))

	var content string
	if m.phase == game.Finished {
		content = fmt.Sprintf("Game Over! Winner: %s\n\nPress Esc to go back.", m.winner)
	} else {
		content = "Waiting for game to start..."
	}

	return lg.Place(
		m.global.Width, m.global.Height,
		lg.Center, lg.Center,
		styles.RenderMainLayout(m.global.Width, m.global.Height, header, content, footer),
	)
}
