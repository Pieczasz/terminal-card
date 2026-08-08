package lobby

import (
	"fmt"
	"slices"
	"strconv"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/lobby"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"
	"github.com/Pieczasz/terminal-card/internal/tui/views"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

const (
	createCursorGame = iota
	createCursorPlayers
	createCursorVisibility
	createCursorMode
	createCursorSubmit
)

type createModel struct {
	global      router.GlobalContext
	err         error
	cursor      int
	isPrivate   bool
	isRanked    bool
	maxPlayers  int
	gameOptions []string
	gameIndex   int
}

func NewCreate(global router.GlobalContext) tea.Model {
	gameOptions := global.GameRegistry.GameNames()
	if len(gameOptions) == 0 {
		gameOptions = []string{"Crazy Eights"}
	}
	return &createModel{
		global:      global,
		cursor:      0,
		isPrivate:   true,
		isRanked:    false, // casual default - matches lobby.setupDefaultOptions
		maxPlayers:  4,
		gameOptions: gameOptions,
		gameIndex:   0,
	}
}

func (m *createModel) Init() tea.Cmd {
	return nil
}

func (m *createModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := views.HandleCommonMsg(msg, &m.global); handled {
		return m, cmd
	}

	key, ok := msg.(tea.KeyPressMsg)
	if !ok {
		return m, nil
	}
	return m.handleKey(key)
}

func (m *createModel) handleKey(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if cmd, ok := views.NavigateOn(key.String()); ok {
		return m, cmd
	}

	switch key.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < createCursorSubmit {
			m.cursor++
		}
	case "left", "h":
		m.adjustSetting(-1)
	case "right", "l":
		m.adjustSetting(+1)
	case "enter":
		if m.cursor == createCursorSubmit {
			return m.createLobby()
		}
	}
	return m, nil
}

// adjustSetting moves the highlighted setting by delta. The two booleans toggle in
// either direction, matching how the row renders as "< value >".
func (m *createModel) adjustSetting(delta int) {
	switch m.cursor {
	case createCursorGame:
		if next := m.gameIndex + delta; next >= 0 && next < len(m.gameOptions) {
			m.gameIndex = next
			m.clampMaxPlayers()
		}
	case createCursorVisibility:
		m.isPrivate = !m.isPrivate
	case createCursorMode:
		m.isRanked = !m.isRanked
	case createCursorPlayers:
		if next := m.maxPlayers + delta; next >= 2 && next <= m.gameMaxPlayers() {
			m.maxPlayers = next
		}
	case createCursorSubmit:
		// The submitted row has no left/right adjustment.
	}
}

func (m *createModel) createLobby() (tea.Model, tea.Cmd) {
	l, err := m.global.LobbyManager.New(views.SessionPlayer(m.global),
		lobby.WithCardGame(&db.Game{Name: m.gameOptions[m.gameIndex]}),
		lobby.WithMaxPlayers(m.maxPlayers),
		lobby.WithPrivate(m.isPrivate),
		lobby.WithRanked(m.isRanked),
	)
	if err != nil {
		m.err = err
		return m, nil
	}
	return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteLobby, Context: l} }
}

func (m *createModel) gameMaxPlayers() int {
	rules, err := m.global.GameRegistry.Create(m.gameOptions[m.gameIndex])
	if err != nil {
		return 8
	}
	return rules.MaxPlayers()
}

func (m *createModel) clampMaxPlayers() {
	maxP := m.gameMaxPlayers()
	minP := 2
	if rules, err := m.global.GameRegistry.Create(m.gameOptions[m.gameIndex]); err == nil {
		minP = rules.MinPlayers()
	}
	// minP is applied last so it wins if a game's bounds ever cross.
	m.maxPlayers = max(min(m.maxPlayers, maxP), minP)
}

func (m *createModel) View() tea.View {
	renderOption := func(idx int, label, value string) string {
		cursor := "  "
		if m.cursor == idx {
			cursor = "> "
			label = m.global.Theme.PlayerItemSelected.Render(label)
			value = m.global.Theme.PlayerItemSelected.Render(value)
		}
		return fmt.Sprintf("%s%s: < %s >", cursor, label, value)
	}

	gameStr := renderOption(createCursorGame, "Game", m.gameOptions[m.gameIndex])
	playersStr := renderOption(createCursorPlayers, "Max Players", strconv.Itoa(m.maxPlayers))

	vis := fmt.Sprintf("%-7s", "Public")
	if m.isPrivate {
		vis = fmt.Sprintf("%-7s", "Private")
	}
	visStr := renderOption(createCursorVisibility, "Visibility", vis)

	mode := fmt.Sprintf("%-7s", "Casual")
	if m.isRanked {
		mode = fmt.Sprintf("%-7s", "Ranked")
	}
	modeStr := renderOption(createCursorMode, "Mode", mode)

	submitCursor := "  "
	submitText := "[ Create Lobby ]"
	if m.cursor == createCursorSubmit {
		submitCursor = "> "
		submitText = m.global.Theme.SuccessText.Render(submitText)
	}
	submitStr := fmt.Sprintf("%s%s", submitCursor, submitText)

	form := lg.JoinVertical(lg.Left,
		gameStr,
		playersStr,
		visStr,
		modeStr,
		"",
		submitStr,
	)

	content := form
	if m.err != nil {
		content += "\n\n" + m.global.Theme.ErrorText.Render(fmt.Sprintf("Error: %v", m.err))
	}

	innerWidth := styles.InnerWidth(m.global.Width)
	titleFig := styles.RenderFigureASCII("Create New Lobby", innerWidth)
	header := m.global.Theme.Title.Render(titleFig)

	footerActions := slices.Concat([]string{"enter - Confirm"}, styles.GlobalActions)
	footer := m.global.Theme.RenderActionFooter(footerActions)

	return tea.NewView(views.RenderCenteredLayout(m.global, header, content, footer))
}
