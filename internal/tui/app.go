package tui

import (
	"client/internal/db"
	"client/internal/game"
	"client/internal/lobby"
	"client/internal/player"
	"fmt"
	"strings"

	tea "github.com/charmbracelet/bubbletea"
	lg "github.com/charmbracelet/lipgloss"
	"gorm.io/gorm"
)

type currentState uint

const (
	homepage currentState = iota
	inLobby
	inGame
)

type model struct {
	state  currentState
	width  int
	height int

	user         db.User
	database     *gorm.DB
	lobbyManager *lobby.Manager
	gameRegistry *game.Registry

	currentLobby *lobby.Lobby
	currentGame  *game.Engine
	lobbyChan    <-chan lobby.LobbyEvent
	gameChan     <-chan game.Event
}

func Model(user db.User, database *gorm.DB, lobbyManager *lobby.Manager, gameRegistry *game.Registry) tea.Model {
	return model{
		state:        homepage,
		user:         user,
		database:     database,
		lobbyManager: lobbyManager,
		gameRegistry: gameRegistry,
	}
}

func (m model) Init() tea.Cmd {
	return nil
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	switch msg := msg.(type) {
	case tea.WindowSizeMsg:
		m.width = msg.Width
		m.height = msg.Height
	case tea.KeyMsg:
		switch msg.String() {
		case "q", "ctrl+c":
			return m, tea.Quit
		case "n":
			if m.state == homepage {
				p := &player.Player{Id: fmt.Sprint(m.user.ID), DatabaseUser: &m.user}
				gameOpt := lobby.WithCardGame(&db.Game{Name: "Crazy Eights"})
				l, err := m.lobbyManager.NewLobby(p, gameOpt)
				if err == nil {
					m.currentLobby = l
					m.state = inLobby
					m.lobbyChan = l.Broadcaster().Subscribe()
					return m, listenToLobbyBroadcaster(m.lobbyChan)
				}
			}
		case "s":
			if m.state == inLobby {
				engine, err := m.currentLobby.StartGame(m.gameRegistry)
				if err == nil {
					m.currentGame = engine
					m.state = inGame
				}
			}
		}
	case lobbyMsg:
		// A message from the lobby broadcaster
		if msg.Type == "GAME_STARTED" {
			// Start listening to the game broadcaster instead
			// Note: Realistically we need the engine instance, but StartGame was already triggered by leader.
		}
		return m, listenToLobbyBroadcaster(m.lobbyChan)
	case gameMsg:
		// Update game UI state
		return m, listenToGameBroadcaster(m.gameChan)
	}
	return m, nil
}

type lobbyMsg lobby.LobbyEvent
type gameMsg game.Event

func listenToLobbyBroadcaster(ch <-chan lobby.LobbyEvent) tea.Cmd {
	return func() tea.Msg {
		if ch == nil {
			return nil
		}
		msg, ok := <-ch
		if !ok {
			return nil
		}
		return lobbyMsg(msg)
	}
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

func (m model) View() string {
	// Main box setup
	maxWidth := m.width * 5 / 6
	title := StyleTitle.Render("Play card games in your terminal")
	boxStyle := StyleBox.Width(maxWidth).Align(lg.Center)
	mainBox := boxStyle.Render(
		lg.JoinVertical(lg.Center, title),
	)

	rawActions := []string{
		"n - Create new game",
		"j - Join game",
		"p - Your Profile",
		"q - Quit",
	}

	var renderedActions []string
	var totalActionsWidth int

	for i, action := range rawActions {
		style := StyleHomePageActionsText
		if i == len(rawActions)-1 {
			style = style.PaddingRight(0)
		}

		r := style.Render(action)
		renderedActions = append(renderedActions, r)
		totalActionsWidth += lg.Width(r)
	}

	// "space-between"
	numItems := len(renderedActions)
	numGaps := numItems - 1

	var gapSize int
	if numGaps > 0 {
		gapSize = (maxWidth - totalActionsWidth) / numGaps
	}
	if gapSize < 0 {
		gapSize = 0
	}

	spacer := strings.Repeat(" ", gapSize)
	homePageActions := strings.Join(renderedActions, spacer)

	uiStack := lg.JoinVertical(
		lg.Center,
		mainBox,
		lg.NewStyle().MarginTop(1).Render(homePageActions),
	)

	if m.state == inLobby {
		uiStack = lg.JoinVertical(lg.Center,
			StyleTitle.Render(fmt.Sprintf("Lobby Code: %s", m.currentLobby.Code())),
			"Waiting for players...",
			"s - Start Game",
		)
	} else if m.state == inGame {
		uiStack = lg.JoinVertical(lg.Center,
			StyleTitle.Render("Game Started!"),
			"(Game UI would be here)",
		)
	}

	return lg.Place(
		m.width, m.height,
		lg.Center, lg.Center,
		uiStack,
	)
}
