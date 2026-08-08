package lobby

import (
	"fmt"
	"log/slog"
	"slices"
	"strconv"

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
	isRanked         bool
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
	var subErr error
	if activeLobby != nil {
		ch, subErr = activeLobby.Subscribe(playerID)
		if subErr != nil {
			// Without the feed this screen would never see another player join or the
			// game start, so the player is told instead of being left staring at a
			// roster that silently stops updating.
			slog.Error("lobby view could not subscribe to events", "error", subErr, "player_id", playerID)
			subErr = fmt.Errorf("live updates unavailable, rejoin the lobby: %w", subErr)
		}
	}
	gameName := ""
	isPrivate := true
	isRanked := false
	maxPlayers := 4
	if activeLobby != nil {
		gameName = activeLobby.GameName()
		isPrivate = activeLobby.IsPrivate()
		isRanked = activeLobby.IsRanked()
		maxPlayers = activeLobby.MaxPlayers()
	}
	return &model{
		global:       global,
		currentLobby: activeLobby,
		lobbyChan:    ch,
		cursor:       0,
		gameOptions:  []string{gameName},
		gameIndex:    0,
		isPrivate:    isPrivate,
		isRanked:     isRanked,
		maxPlayers:   maxPlayers,
		actionErr:    subErr,
	}
}

func (m *model) Init() tea.Cmd {
	return listenToLobbyBroadcaster(m.lobbyChan)
}

// Elo is retrieved from preloaded Rankings, which is updated whenever a game ends.
func (m *model) getElo(p *player.Player) uint32 {
	if p == nil || p.DatabaseUser == nil {
		return elo.ToUint32(elo.DefaultRating)
	}
	gameName := m.currentLobby.GameName()
	for _, r := range p.DatabaseUser.Rankings {
		if r.Game.Name == gameName {
			return r.Elo
		}
	}
	return elo.ToUint32(elo.DefaultRating)
}

func (m *model) unsubscribe() {
	if m.currentLobby != nil && m.lobbyChan != nil {
		playerID := ""
		if m.global.User != nil {
			playerID = fmt.Sprint(m.global.User.ID)
		}
		m.currentLobby.Unsubscribe(playerID, m.lobbyChan)
		m.lobbyChan = nil
	}
}

func (m *model) gamePlayerBounds() (minPlayers, maxPlayers int) {
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

func (m *model) selfPlayer() *player.Player {
	return views.SessionPlayer(m.global)
}

const (
	cursorGame = iota
	cursorMaxPlayers
	cursorVisibility
	cursorMode
	cursorFirstGuest
)

func (m *model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if m.currentLobby == nil {
		return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteHome} }
	}
	if handled, cmd := views.HandleCommonMsg(msg, &m.global); handled {
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		return m.handleKey(msg)
	case lobbyMsg:
		return m.handleLobbyEvent(lobby.Event(msg))
	}
	return m, nil
}

func (m *model) handleKey(msg tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if m.showLeaveConfirm {
		return m.handleLeaveConfirm(msg.String())
	}

	self := m.selfPlayer()
	isLeader := m.currentLobby.IsLeader(self)

	// Leaving via a global shortcut has to release the lobby subscription first.
	if route, ok := views.GlobalRoute(msg.String()); ok {
		m.unsubscribe()
		return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: route} }
	}

	switch msg.String() {
	case "esc", "x", "q":
		m.showLeaveConfirm = true
		return m, nil
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
		maxCursor := cursorMode + len(m.currentLobby.Guests())
		if isLeader && m.cursor < maxCursor {
			m.cursor++
		}
	case "left", "h":
		if isLeader {
			m.adjustSetting(self, -1)
		}
	case "right", "l":
		if isLeader {
			m.adjustSetting(self, +1)
		}
	case "enter":
		if isLeader && m.cursor >= cursorFirstGuest {
			guestIdx := m.cursor - cursorFirstGuest
			guests := m.currentLobby.Guests()
			if guestIdx < len(guests) {
				if err := m.global.LobbyManager.Kick(self, guests[guestIdx]); err != nil {
					slog.Error("failed to kick player", "error", err)
				}
			}
		}
	}
	return m, nil
}

func (m *model) handleLeaveConfirm(key string) (tea.Model, tea.Cmd) {
	switch key {
	case "y", "Y":
		m.global.LobbyManager.LeaveLobby(m.selfPlayer())
		m.unsubscribe()
		return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteHome} }
	case "n", "N", "esc":
		m.showLeaveConfirm = false
	}
	return m, nil
}

func (m *model) adjustSetting(self *player.Player, delta int) {
	switch m.cursor {
	case cursorMaxPlayers:
		rulesMin, rulesMax := m.gamePlayerBounds()
		next := m.maxPlayers + delta
		if delta < 0 && (next < rulesMin || next < m.currentLobby.CurrentPlayers()) {
			return
		}
		if delta > 0 && next > rulesMax {
			return
		}
		m.maxPlayers = next
		if err := m.currentLobby.SetMaxPlayers(self, m.maxPlayers, rulesMin, rulesMax); err != nil {
			m.maxPlayers -= delta
			slog.Error("failed to set max players", "error", err)
		}
	case cursorVisibility:
		m.isPrivate = !m.isPrivate
		if err := m.currentLobby.SetPrivate(self, m.isPrivate); err != nil {
			m.isPrivate = !m.isPrivate
			slog.Error("failed to set privacy", "error", err)
		}
	case cursorMode:
		m.isRanked = !m.isRanked
		if err := m.currentLobby.SetRanked(self, m.isRanked); err != nil {
			m.isRanked = !m.isRanked
			slog.Error("failed to set ranked mode", "error", err)
		}
	case cursorGame, cursorFirstGuest:
		// Game selection is fixed once a lobby exists; guest rows have no
		// left/right adjustment. Both are intentional no-ops.
	default:
	}
}

func (m *model) handleLobbyEvent(msg lobby.Event) (tea.Model, tea.Cmd) {
	switch msg.Type {
	case lobby.EventLobbyClosed:
		m.unsubscribe()
		return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteHome} }
	case lobby.EventGameStarted:
		engine, ok := msg.Payload.(*game.Engine)
		if !ok || engine == nil {
			slog.Error("GAME_STARTED payload was not a game engine")
			return m, listenToLobbyBroadcaster(m.lobbyChan)
		}
		mod, ok := m.global.GameRegistry.Module(m.currentLobby.GameName())
		if !ok {
			// Keep the listener armed: an unregistered game is no reason to eject
			// the player, and returning a nil command would leave this view deaf to
			// every later lobby event while still holding its subscriber slot.
			slog.Error("game not registered, cannot route to its view", "game", m.currentLobby.GameName())
			return m, listenToLobbyBroadcaster(m.lobbyChan)
		}
		m.unsubscribe()
		return m, func() tea.Msg {
			return router.ChangeViewMsg{ViewName: router.GameRoute(mod.Slug), Context: engine}
		}
	case lobby.EventSettingsUpdated, lobby.EventPlayersUpdated:
		self := m.selfPlayer()
		if !m.currentLobby.Leader().Equal(self) {
			found := false
			for _, g := range m.currentLobby.Guests() {
				if g.Equal(self) {
					found = true
					break
				}
			}
			if !found {
				m.unsubscribe()
				return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteHome} }
			}
		}
		m.isPrivate = m.currentLobby.IsPrivate()
		m.isRanked = m.currentLobby.IsRanked()
		m.maxPlayers = m.currentLobby.MaxPlayers()
		maxCursor := cursorMode + len(m.currentLobby.Guests())
		if m.cursor > maxCursor {
			m.cursor = maxCursor
		}
	}
	return m, listenToLobbyBroadcaster(m.lobbyChan)
}

func (m *model) View() tea.View {
	if m.currentLobby == nil {
		return tea.NewView("No active lobby.")
	}

	innerWidth := styles.InnerWidth(m.global.Width)
	titleFig := styles.RenderFigureASCII("Lobby", innerWidth)
	header := m.global.Theme.Title.Render(titleFig)

	isLeader := m.currentLobby.IsLeader(m.selfPlayer())

	footerActions := slices.Concat([]string{"x - Leave Lobby", "r - Ready"}, styles.GlobalActions)
	footer := m.global.Theme.RenderActionFooter(footerActions)

	if m.showLeaveConfirm {
		redYes := m.global.Theme.ErrorText.Bold(true).Render("Yes")
		popupText := fmt.Sprintf("Are you sure you want to leave the lobby?\n\n[y] %s   [n] No", redYes)

		return tea.NewView(views.RenderCenteredLayout(m.global, header, popupText, footer))
	}

	form := m.renderForm(isLeader, innerWidth)
	if m.actionErr != nil {
		errLine := m.global.Theme.ErrorText.Render(m.actionErr.Error())
		form = lg.JoinVertical(lg.Center, form, "", errLine)
	}

	return tea.NewView(views.RenderCenteredLayout(m.global, header, form, footer))
}

// renderForm lays the settings and player columns side by side, stacking them
// vertically when they would not fit innerWidth: lipgloss word-wraps the columns
// rather than shrinking them.
func (m *model) renderForm(isLeader bool, innerWidth int) string {
	settingsStack := m.renderSettings(isLeader)
	playersStack := lg.JoinVertical(lg.Left, m.renderPlayerList(isLeader)...)

	if lg.Width(settingsStack)+lg.Width(playersStack)+4 > innerWidth {
		settingsCol := lg.NewStyle().Align(lg.Left).Render(settingsStack)
		playersCol := lg.NewStyle().Align(lg.Left).MarginTop(2).Render(playersStack)
		return lg.JoinVertical(lg.Left, settingsCol, playersCol)
	}
	settingsCol := lg.NewStyle().Align(lg.Left).MarginRight(6).Render(settingsStack)
	playersCol := lg.NewStyle().Align(lg.Left).Render(playersStack)
	return lg.NewStyle().Align(lg.Center).Render(lg.JoinHorizontal(lg.Top, settingsCol, playersCol))
}

func (m *model) renderSettings(isLeader bool) string {
	renderOption := func(idx int, label, value string) string {
		cursor := "  "
		if isLeader && m.cursor == idx {
			cursor = "> "
			label = m.global.Theme.PlayerItemSelected.Render(label)
			value = m.global.Theme.PlayerItemSelected.Render(value)
		}
		return fmt.Sprintf("%s%s: < %s >", cursor, label, value)
	}

	vis := "Public"
	if m.isPrivate {
		vis = "Private"
	}
	mode := "Casual"
	if m.isRanked {
		mode = "Ranked"
	}

	return lg.JoinVertical(lg.Left,
		"  "+m.global.Theme.SectionHeading.Render("Settings"),
		fmt.Sprintf("  Lobby Code: %s", m.global.Theme.LobbyCode.Render(m.currentLobby.Code())),
		renderOption(cursorGame, "Game", m.gameOptions[m.gameIndex]),
		renderOption(cursorMaxPlayers, "Max Players", strconv.Itoa(m.maxPlayers)),
		renderOption(cursorVisibility, "Visibility", fmt.Sprintf("%-7s", vis)),
		renderOption(cursorMode, "Mode", fmt.Sprintf("%-7s", mode)),
	)
}

// renderPlayerList returns the heading, the leader row, then one row per guest.
// Only the leader gets a cursor, since only they can kick.
func (m *model) renderPlayerList(isLeader bool) []string {
	guests := m.currentLobby.Guests()
	rows := make([]string, 0, 2+len(guests))
	rows = append(rows, "  "+m.global.Theme.SectionHeading.Render("Players"))

	leader := m.currentLobby.Leader()
	rows = append(rows, fmt.Sprintf("  %s %s (Elo: %d)%s",
		m.global.Theme.HostTag.Render("[Leader]"), leader.Username(), m.getElo(leader), m.readyMark(leader)))

	for i, g := range guests {
		cursor := "  "
		isSelected := isLeader && m.cursor == i+cursorFirstGuest
		if isSelected {
			cursor = "> "
		}
		row := fmt.Sprintf("%s%s %s (Elo: %d)%s",
			cursor, m.global.Theme.GuestTag.Render("[Guest] "), g.Username(), m.getElo(g), m.readyMark(g))
		if isSelected {
			row = m.global.Theme.PlayerItemSelected.Render(row)
		}
		rows = append(rows, row)
	}
	return rows
}

func (m *model) readyMark(p *player.Player) string {
	if !m.currentLobby.IsReady(p) {
		return ""
	}
	return m.global.Theme.SuccessText.Render(" - Ready")
}

// Close releases the lobby subscription when the router replaces this view or the
// session ends.
func (m *model) Close() {
	m.unsubscribe()
}
