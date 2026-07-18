package lobby

import (
	"fmt"
	"log/slog"
	"github.com/Pieczasz/terminal-card/internal/elo"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/lobby"
	"github.com/Pieczasz/terminal-card/internal/player"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"
	"github.com/Pieczasz/terminal-card/internal/tui/views"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

type lobbyMsg lobby.Event

type model struct {
	global       router.GlobalContext
	currentLobby *lobby.Lobby
	lobbyChan    <-chan lobby.Event

	cursor           int
	gameOptions      []string
	gameIndex        int
	isPrivate        bool
	maxPlayers       int
	showLeaveConfirm bool
	actionErr        error
}

func listenToLobbyBroadcaster(ch <-chan lobby.Event) tea.Cmd {
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

// New returns a new lobby model. We pass the current active lobby through Context.
func New(global router.GlobalContext, activeLobby *lobby.Lobby) tea.Model {
	playerID := ""
	if global.User != nil {
		playerID = fmt.Sprint(global.User.ID)
	}
	var ch <-chan lobby.Event
	if activeLobby != nil {
		ch = activeLobby.Subscribe(playerID)
	}
	gameName := ""
	isPrivate := true
	maxPlayers := 4
	if activeLobby != nil {
		gameName = activeLobby.GameName()
		isPrivate = activeLobby.IsPrivate()
		maxPlayers = activeLobby.MaxPlayers()
	}
	return model{
		global:       global,
		currentLobby: activeLobby,
		lobbyChan:    ch,
		cursor:       0,
		gameOptions:  []string{gameName},
		gameIndex:    0,
		isPrivate:    isPrivate,
		maxPlayers:   maxPlayers,
	}
}

func (m model) Init() tea.Cmd {
	return listenToLobbyBroadcaster(m.lobbyChan)
}

// Elo is retrieved from preloaded Rankings, which is updated whenever a game ends.
func (m model) getElo(p *player.Player) uint32 {
	if p == nil || p.DatabaseUser == nil {
		return uint32(elo.DefaultRating)
	}
	gameName := m.currentLobby.GameName()
	for _, r := range p.DatabaseUser.Rankings {
		if r.Game.Name == gameName {
			return r.Elo
		}
	}
	return uint32(elo.DefaultRating)
}

func (m model) unsubscribe() model {
	if m.currentLobby != nil && m.lobbyChan != nil {
		playerID := ""
		if m.global.User != nil {
			playerID = fmt.Sprint(m.global.User.ID)
		}
		m.currentLobby.Unsubscribe(playerID, m.lobbyChan)
		m.lobbyChan = nil
	}
	return m
}

func (m model) gamePlayerBounds() (minPlayers, maxPlayers int) {
	minPlayers, maxPlayers = 2, 6
	if m.global.GameRegistry == nil || m.currentLobby == nil {
		return minPlayers, maxPlayers
	}
	rules, err := m.global.GameRegistry.Create(m.currentLobby.GameName())
	if err != nil {
		return minPlayers, maxPlayers
	}
	return rules.MinPlayers(), rules.MaxPlayers()
}

func (m model) selfPlayer() *player.Player {
	return &player.Player{ID: fmt.Sprint(m.global.User.ID), DatabaseUser: m.global.User}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.currentLobby == nil {
		return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "home"} }
	}
	if handled, cmd := views.HandleCommonMsg(msg, &m.global); handled {
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.showLeaveConfirm {
			switch msg.String() {
			case "y", "Y":
				p := &player.Player{ID: fmt.Sprint(m.global.User.ID), DatabaseUser: m.global.User}
				m.global.LobbyManager.LeaveLobby(p)
				m = m.unsubscribe()
				return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "home"} }
			case "n", "N", "esc":
				m.showLeaveConfirm = false
			}
			return m, nil
		}

		isLeader := m.currentLobby.IsLeader(&player.Player{ID: fmt.Sprint(m.global.User.ID), DatabaseUser: m.global.User})
		self := &player.Player{ID: fmt.Sprint(m.global.User.ID), DatabaseUser: m.global.User}

		switch msg.String() {
		case "esc", "x":
			m.showLeaveConfirm = true
			return m, nil
		case "n":
			m = m.unsubscribe()
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "lobby_create"} }
		case "f":
			m = m.unsubscribe()
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "lobby_join"} }
		case "p":
			m = m.unsubscribe()
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "profile"} }
		case "t":
			m = m.unsubscribe()
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "leaderboard"} }
		case "r":
			if err := m.currentLobby.ToggleReady(self, m.global.GameRegistry); err != nil {
				m.actionErr = err
				slog.Error("failed to toggle ready or start game engine", "error", err)
			} else {
				m.actionErr = nil
			}
			return m, nil
		case "up", "k":
			if isLeader && m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			maxCursor := 2 + len(m.currentLobby.Guests())
			if isLeader && m.cursor < maxCursor {
				m.cursor++
			}
		case "left", "h":
			if isLeader {
				switch m.cursor {
				case 1:
					rulesMin, rulesMax := m.gamePlayerBounds()
					if m.maxPlayers > rulesMin && m.maxPlayers > m.currentLobby.CurrentPlayers() {
						m.maxPlayers--
						if err := m.currentLobby.SetMaxPlayers(self, m.maxPlayers, rulesMin, rulesMax); err != nil {
							m.maxPlayers++
							slog.Error("failed to set max players", "error", err)
						}
					}
				case 2:
					m.isPrivate = !m.isPrivate
					if err := m.currentLobby.SetPrivate(self, m.isPrivate); err != nil {
						m.isPrivate = !m.isPrivate
						slog.Error("failed to set privacy", "error", err)
					}
				}
			}
		case "right", "l":
			if isLeader {
				switch m.cursor {
				case 1:
					rulesMin, rulesMax := m.gamePlayerBounds()
					if m.maxPlayers < rulesMax {
						m.maxPlayers++
						if err := m.currentLobby.SetMaxPlayers(self, m.maxPlayers, rulesMin, rulesMax); err != nil {
							m.maxPlayers--
							slog.Error("failed to set max players", "error", err)
						}
					}
				case 2:
					m.isPrivate = !m.isPrivate
					if err := m.currentLobby.SetPrivate(self, m.isPrivate); err != nil {
						m.isPrivate = !m.isPrivate
						slog.Error("failed to set privacy", "error", err)
					}
				}
			}
		case "enter":
			if isLeader && m.cursor > 2 {
				guestIdx := m.cursor - 3
				guests := m.currentLobby.Guests()
				if guestIdx < len(guests) {
					if err := m.global.LobbyManager.Kick(self, guests[guestIdx]); err != nil {
						slog.Error("failed to kick player", "error", err)
					}
				}
			}
		}

	case lobbyMsg:
		if msg.Type == "LOBBY_CLOSED" {
			m = m.unsubscribe()
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "home"} }
		}
		if msg.Type == "GAME_STARTED" {
			engine, ok := msg.Payload.(*game.Engine)
			if !ok || engine == nil {
				slog.Error("GAME_STARTED payload was not a game engine")
				return m, listenToLobbyBroadcaster(m.lobbyChan)
			}

			routeName, err := m.global.GameRegistry.RouteName(m.currentLobby.GameName())
			if err != nil {
				slog.Error("no TUI route for game", "game", m.currentLobby.GameName(), "error", err)
				return m, nil
			}
			m = m.unsubscribe()
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: routeName, Context: engine} }
		}
		if msg.Type == "SETTINGS_UPDATED" || msg.Type == "PLAYERS_UPDATED" {
			p := &player.Player{ID: fmt.Sprint(m.global.User.ID), DatabaseUser: m.global.User}

			// Check if we got kicked
			if !m.currentLobby.Leader().Compare(p) {
				found := false
				for _, g := range m.currentLobby.Guests() {
					if g.Compare(p) {
						found = true
						break
					}
				}
				if !found {
					m = m.unsubscribe()
					return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "home"} }
				}
			}

			m.isPrivate = m.currentLobby.IsPrivate()
			m.maxPlayers = m.currentLobby.MaxPlayers()

			maxCursor := 2 + len(m.currentLobby.Guests())
			if m.cursor > maxCursor {
				m.cursor = maxCursor
			}
		}
		return m, listenToLobbyBroadcaster(m.lobbyChan)
	}
	return m, nil
}

func (m model) View() tea.View {
	if m.currentLobby == nil {
		return tea.NewView("No active lobby.")
	}

	innerWidth := styles.GetInnerWidth(m.global.Width)
	titleFig := styles.RenderFigureASCII("Lobby", innerWidth)
	titleText := styles.Title.Render(titleFig)
	header := styles.Title.Render(titleText)

	isLeader := m.currentLobby.IsLeader(m.selfPlayer())

	var footerActions []string
	footerActions = append(footerActions, "x - Leave Lobby")
	footerActions = append(footerActions, "r - Ready")
	footerActions = append(footerActions, styles.GlobalActions...)
	footer := lg.NewStyle().Render(styles.RenderActionFooter(footerActions))

	if m.showLeaveConfirm {
		redYes := lg.NewStyle().Foreground(lg.Color("#FF4444")).Bold(true).Render("Yes")
		popupText := fmt.Sprintf("Are you sure you want to leave the lobby?\n\n[y] %s   [n] No", redYes)

		return tea.NewView(views.RenderCenteredLayout(m.global.Width, m.global.Height, header, popupText, footer))
	}

	renderOption := func(idx int, label, value string) string {
		cursor := "  "
		if isLeader && m.cursor == idx {
			cursor = "> "
			label = lg.NewStyle().Foreground(lg.Color("205")).Render(label)
			value = lg.NewStyle().Foreground(lg.Color("205")).Render(value)
		}
		return fmt.Sprintf("%s%s: < %s >", cursor, label, value)
	}

	gameStr := renderOption(0, "Game", m.gameOptions[m.gameIndex])
	playersStr := renderOption(1, "Max Players", fmt.Sprintf("%d", m.maxPlayers))

	vis := fmt.Sprintf("%-7s", "Public")
	if m.isPrivate {
		vis = fmt.Sprintf("%-7s", "Private")
	}
	visStr := renderOption(2, "Visibility", vis)

	var playerList []string
	playerList = append(playerList, "  "+lg.NewStyle().Foreground(lg.Color("#FFA500")).Bold(true).Render("Players"))

	leader := m.currentLobby.Leader()
	leaderElo := m.getElo(leader)

	leaderReadyStr := ""
	if m.currentLobby.IsReady(leader) {
		leaderReadyStr = lg.NewStyle().Foreground(lg.Color("46")).Render(" - Ready")
	}
	playerList = append(playerList, fmt.Sprintf("  %s %s (Elo: %d)%s", styles.HostTag.Render("[Leader]"), leader.Username(), leaderElo, leaderReadyStr))

	guests := m.currentLobby.Guests()
	for i, g := range guests {
		cursor := "  "
		isSelected := isLeader && m.cursor == i+3
		if isSelected {
			cursor = "> "
		}
		guestElo := m.getElo(g)
		guestReadyStr := ""
		if m.currentLobby.IsReady(g) {
			guestReadyStr = lg.NewStyle().Foreground(lg.Color("46")).Render(" - Ready")
		}
		row := fmt.Sprintf("%s%s %s (Elo: %d)%s", cursor, styles.GuestTag.Render("[Guest] "), g.Username(), guestElo, guestReadyStr)
		if isSelected {
			row = styles.PlayerItemSelected.Render(row)
		}
		playerList = append(playerList, row)
	}

	codeDisplay := fmt.Sprintf("  Lobby Code: %s", styles.LobbyCode.Render(m.currentLobby.Code()))

	settingsStack := lg.JoinVertical(lg.Left,
		"  "+lg.NewStyle().Foreground(lg.Color("#FFA500")).Bold(true).Render("Settings"),
		codeDisplay,
		gameStr,
		playersStr,
		visStr,
	)

	playersStack := lg.JoinVertical(lg.Left, playerList...)

	settingsWidth := lg.Width(settingsStack)
	playersWidth := lg.Width(playersStack)

	var form string
	if settingsWidth+playersWidth+4 > innerWidth {
		// Stack vertically to avoid lipgloss word-wrapping chaos
		settingsCol := lg.NewStyle().Align(lg.Left).Render(settingsStack)
		playersCol := lg.NewStyle().Align(lg.Left).MarginTop(2).Render(playersStack)
		form = lg.JoinVertical(lg.Left, settingsCol, playersCol)
	} else {
		// Render side by side with a natural gap, we wrap the form in a centered block to center it as a whole
		settingsCol := lg.NewStyle().Align(lg.Left).MarginRight(6).Render(settingsStack)
		playersCol := lg.NewStyle().Align(lg.Left).Render(playersStack)
		form = lg.NewStyle().Align(lg.Center).Render(lg.JoinHorizontal(lg.Top, settingsCol, playersCol))
	}

	if m.actionErr != nil {
		errLine := lg.NewStyle().Foreground(lg.Color("9")).Render(m.actionErr.Error())
		form = lg.JoinVertical(lg.Center, form, "", errLine)
	}

	return tea.NewView(views.RenderCenteredLayout(m.global.Width, m.global.Height, header, form, footer))
}
