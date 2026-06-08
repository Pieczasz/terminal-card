package lobby

import (
	"fmt"
	"terminalcard/internal/db"
	"terminalcard/internal/lobby"
	"terminalcard/internal/player"
	"terminalcard/internal/tui/router"
	"terminalcard/internal/tui/styles"

	"terminalcard/internal/tui/views/common"

	tea "github.com/charmbracelet/bubbletea"
	lg "github.com/charmbracelet/lipgloss"
)

type createModel struct {
	global      router.GlobalContext
	err         error
	cursor      int
	isPrivate   bool
	maxPlayers  int
	gameOptions []string
	gameIndex   int
}

func NewCreate(global router.GlobalContext) tea.Model {
	return createModel{
		global:      global,
		cursor:      0, // 0: Game, 1: Players, 2: Visibility, 3: Create Button
		isPrivate:   true,
		maxPlayers:  4,
		gameOptions: []string{"Crazy Eights"}, //TODO: automate this
		gameIndex:   0,
	}
}

func (m createModel) Init() tea.Cmd {
	return nil
}

func (m createModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := common.HandleCommonMsg(msg, &m.global); handled {
		return m, cmd
	}

	switch msg := msg.(type) {
	case tea.KeyMsg:
		switch msg.String() {
		case "esc":
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "home"} }
		case "n":
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "lobby_create"} }
		case "f":
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "lobby_join"} }
		case "p":
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "profile"} }
		case "up", "k":
			if m.cursor > 0 {
				m.cursor--
			}
		case "down", "j":
			if m.cursor < 3 {
				m.cursor++
			}
		case "left", "h":
			switch m.cursor {
			case 0:
				if m.gameIndex > 0 {
					m.gameIndex--
				}
			case 1:
				if m.maxPlayers > 2 {
					m.maxPlayers--
				}
			case 2:
				m.isPrivate = !m.isPrivate
			}
		case "right", "l":
			switch m.cursor {
			case 0:
				if m.gameIndex < len(m.gameOptions)-1 {
					m.gameIndex++
				}
			case 1:
				if m.maxPlayers < 8 { // hard limit example
					m.maxPlayers++
				}
			case 2:
				m.isPrivate = !m.isPrivate
			}
		case "enter":
			if m.cursor == 3 {
				// Submit Create
				p := &player.Player{Id: fmt.Sprint(m.global.User.ID), DatabaseUser: m.global.User}
				gameOpt := lobby.WithCardGame(&db.Game{Name: m.gameOptions[m.gameIndex]})
				maxOpt := lobby.WithMaxPlayers(m.maxPlayers)
				privOpt := lobby.WithPrivate(m.isPrivate)

				l, err := m.global.LobbyManager.New(p, gameOpt, maxOpt, privOpt)
				if err != nil {
					m.err = err
					return m, nil
				}
				return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "lobby", Context: l} }
			}
		}
	}
	return m, nil
}

func (m createModel) View() string {
	renderOption := func(idx int, label, value string) string {
		cursor := "  "
		if m.cursor == idx {
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

	submitCursor := "  "
	submitText := "[ Create Lobby ]"
	if m.cursor == 3 {
		submitCursor = "> "
		submitText = lg.NewStyle().Foreground(lg.Color("42")).Render(submitText)
	}
	submitStr := fmt.Sprintf("%s%s", submitCursor, submitText)

	form := lg.JoinVertical(lg.Left,
		gameStr,
		playersStr,
		visStr,
		"",
		submitStr,
	)

	content := form
	if m.err != nil {
		content += fmt.Sprintf("\n\nError: %v", m.err)
	}

	innerWidth := styles.GetInnerWidth(m.global.Width)
	titleFig := styles.RenderFigureAscii("Create New Lobby", innerWidth)
	titleText := styles.Title.Render(titleFig)
	header := styles.Title.Render(titleText)

	footerActions := append([]string{"enter - Confirm"}, styles.GlobalActions...)
	footer := lg.NewStyle().Render(styles.RenderActionFooter(footerActions))

	return common.RenderCenteredLayout(m.global.Width, m.global.Height, header, content, footer)
}
