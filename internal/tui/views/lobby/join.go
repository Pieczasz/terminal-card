package lobby

import (
	"fmt"
	"strings"
	"terminalcard/internal/player"
	"terminalcard/internal/tui/router"
	"terminalcard/internal/tui/styles"
	"terminalcard/internal/tui/views"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

type joinModel struct {
	global      router.GlobalContext
	textInput   textinput.Model
	err         error
	cursor      int
	writingCode bool
}

func NewJoin(global router.GlobalContext) tea.Model {
	ti := textinput.New()
	ti.Placeholder = "6-character code"
	ti.CharLimit = 6
	ti.SetWidth(20)

	return joinModel{
		global:      global,
		textInput:   ti,
		cursor:      0,
		writingCode: false,
	}
}

func (m joinModel) Init() tea.Cmd {
	return textinput.Blink
}

func (m joinModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	var cmd tea.Cmd

	p := &player.Player{ID: fmt.Sprint(m.global.User.ID), DatabaseUser: m.global.User}
	lobbies := m.global.LobbyManager.PublicLobbies(p)

	if !m.writingCode {
		if handled, commonCmd := views.HandleCommonMsg(msg, &m.global); handled {
			return m, commonCmd
		}
	} else if msg, ok := msg.(tea.WindowSizeMsg); ok {
		m.global.Width = msg.Width
		m.global.Height = msg.Height
	}

	switch msg := msg.(type) {
	case tea.KeyPressMsg:
		if m.writingCode {
			switch msg.String() {
			case "esc":
				m.writingCode = false
				m.textInput.Blur()
				return m, nil
			case "enter":
				code := strings.ToUpper(m.textInput.Value())
				if code == "" {
					return m, nil
				}
				p := &player.Player{ID: fmt.Sprint(m.global.User.ID), DatabaseUser: m.global.User}
				err := m.global.LobbyManager.JoinLobbyByCode(code, p)
				if err != nil {
					m.err = err
					return m, nil
				}
				lobby, _ := m.global.LobbyManager.FindLobbyByCode(code)
				return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "lobby", Context: lobby} }
			default:
				m.textInput, cmd = m.textInput.Update(msg)
				m.textInput.SetValue(strings.ToUpper(m.textInput.Value()))
				return m, cmd
			}
		} else {
			switch msg.String() {
			case "esc":
				return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "home"} }
			case "n":
				return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "lobby_create"} }
			case "f":
				return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "lobby_join"} }
			case "p":
				return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "profile"} }
			case "t":
				return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "leaderboard"} }
			case "up", "k":
				if m.cursor > 0 {
					m.cursor--
				}
			case "down", "j":
				if m.cursor < len(lobbies)-1 {
					m.cursor++
				}
			case "c":
				m.writingCode = true
				m.textInput.Focus()
				return m, textinput.Blink
			case "enter", " ":
				if len(lobbies) > 0 && m.cursor < len(lobbies) {
					l := lobbies[m.cursor]
					p := &player.Player{ID: fmt.Sprint(m.global.User.ID), DatabaseUser: m.global.User}
					err := m.global.LobbyManager.JoinLobbyByCode(l.Code(), p)
					if err != nil {
						m.err = err
						return m, nil
					}
					return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "lobby", Context: l} }
				}
			}
		}
	}

	if m.writingCode {
		m.textInput, cmd = m.textInput.Update(msg)
	}
	return m, cmd
}

func (m joinModel) View() tea.View {
	p := &player.Player{ID: fmt.Sprint(m.global.User.ID), DatabaseUser: m.global.User}
	lobbies := m.global.LobbyManager.PublicLobbies(p)

	var publicLobbiesStr strings.Builder
	publicLobbiesStr.WriteString("Public Lobbies:\n")
	if len(lobbies) == 0 {
		publicLobbiesStr.WriteString("No public lobbies available right now.\n")
	} else {
		for i, l := range lobbies {
			if i >= 10 {
				break // TODO: primitive pagination / limit to 10 for now
			}
			cursor := "  "
			if !m.writingCode && m.cursor == i {
				cursor = "> "
			}
			itemStr := fmt.Sprintf("%s[%s] %s (%d/%d players)",
				cursor, l.Code(), l.GameName(), l.CurrentPlayers(), l.MaxPlayers())

			if !m.writingCode && m.cursor == i {
				itemStr = lg.NewStyle().Foreground(lg.Color("205")).Render(itemStr)
			}
			publicLobbiesStr.WriteString(itemStr + "\n")
		}
	}

	codeInputStr := "\nOr press 'c' to enter a private lobby code:"
	if m.writingCode {
		codeInputStr = "\nEntering private lobby code (press ESC to cancel):"
	}

	content := lg.JoinVertical(lg.Left,
		publicLobbiesStr.String(),
		codeInputStr,
		m.textInput.View(),
	)

	if m.err != nil {
		content += lg.NewStyle().Foreground(lg.Color("9")).Render(fmt.Sprintf("\nError: %v", m.err))
	}

	innerWidth := styles.GetInnerWidth(m.global.Width)
	titleFig := styles.RenderFigureASCII("Join Game", innerWidth)
	titleText := styles.Title.Render(titleFig)

	footerActions := append([]string{"c - Enter Code"}, styles.GlobalActions...)
	footer := lg.NewStyle().Render(styles.RenderActionFooter(footerActions))

	return tea.NewView(views.RenderCenteredLayout(m.global.Width, m.global.Height, titleText, content, footer))
}
