package lobby

import (
	"errors"
	"fmt"
	"slices"
	"strings"

	"github.com/Pieczasz/terminal-card/internal/lobby"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"
	"github.com/Pieczasz/terminal-card/internal/tui/views"

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
	ti.Placeholder = "8-character code"
	ti.CharLimit = 8
	ti.SetWidth(20)

	return &joinModel{
		global:      global,
		textInput:   ti,
		cursor:      0,
		writingCode: false,
	}
}

func (m *joinModel) Init() tea.Cmd {
	return textinput.Blink
}

// publicLobbies lists the lobbies this player may join. It queries the manager on
// demand: the result is only needed for cursor movement and joining, and the
// manager sorts by Elo on every call.
func (m *joinModel) publicLobbies() []*lobby.Lobby {
	return m.global.LobbyManager.PublicLobbies(views.SessionPlayer(m.global))
}

func (m *joinModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	// While typing a code the view keeps the keyboard to itself and only tracks
	// resizes; otherwise the shared global handler gets first refusal.
	if m.writingCode {
		if size, ok := msg.(tea.WindowSizeMsg); ok {
			m.global.Width = size.Width
			m.global.Height = size.Height
		}
	} else if handled, cmd := views.HandleCommonMsg(msg, &m.global); handled {
		return m, cmd
	}

	key, isKey := msg.(tea.KeyPressMsg)
	if !isKey {
		if m.writingCode {
			var cmd tea.Cmd
			m.textInput, cmd = m.textInput.Update(msg)
			return m, cmd
		}
		return m, nil
	}

	if m.writingCode {
		return m.handleCodeEntry(key)
	}
	return m.handleBrowsing(key)
}

func (m *joinModel) handleCodeEntry(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	switch key.String() {
	case "esc":
		m.writingCode = false
		m.textInput.Blur()
		return m, nil
	case "enter":
		return m.joinByCode(strings.ToUpper(m.textInput.Value()))
	default:
		var cmd tea.Cmd
		m.textInput, cmd = m.textInput.Update(key)
		m.textInput.SetValue(strings.ToUpper(m.textInput.Value()))
		return m, cmd
	}
}

func (m *joinModel) handleBrowsing(key tea.KeyPressMsg) (tea.Model, tea.Cmd) {
	if route, ok := views.GlobalRoute(key.String()); ok {
		return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: route} }
	}

	switch key.String() {
	case "esc", "q":
		return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteHome} }
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.publicLobbies())-1 {
			m.cursor++
		}
	case "c":
		m.writingCode = true
		m.textInput.Focus()
		return m, textinput.Blink
	case "enter", " ":
		return m.joinSelected()
	}
	return m, nil
}

func (m *joinModel) joinSelected() (tea.Model, tea.Cmd) {
	lobbies := m.publicLobbies()
	if m.cursor >= len(lobbies) {
		return m, nil
	}
	l := lobbies[m.cursor]
	if err := m.global.LobbyManager.JoinLobbyByCode(l.Code(), views.SessionPlayer(m.global)); err != nil {
		m.err = err
		return m, nil
	}
	return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteLobby, Context: l} }
}

func (m *joinModel) joinByCode(code string) (tea.Model, tea.Cmd) {
	if code == "" {
		return m, nil
	}
	if err := m.global.LobbyManager.JoinLobbyByCode(code, views.SessionPlayer(m.global)); err != nil {
		m.err = err
		return m, nil
	}
	joined, err := m.global.LobbyManager.FindLobbyByCode(code)
	if err != nil || joined == nil {
		m.err = errors.New("joined lobby but failed to open it")
		return m, nil
	}
	return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteLobby, Context: joined} }
}

func (m *joinModel) View() tea.View {
	lobbies := m.publicLobbies()

	var publicLobbiesStr strings.Builder
	publicLobbiesStr.WriteString("Public Lobbies:\n")
	if len(lobbies) == 0 {
		publicLobbiesStr.WriteString("No public lobbies available right now.\n")
	} else {
		const windowSize = 10
		start := 0
		if m.cursor >= windowSize {
			start = m.cursor - windowSize + 1
		}
		end := min(start+windowSize, len(lobbies))
		for i := start; i < end; i++ {
			l := lobbies[i]
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

	innerWidth := styles.InnerWidth(m.global.Width)
	titleFig := styles.RenderFigureASCII("Join Game", innerWidth)
	titleText := styles.Title.Render(titleFig)

	footerActions := slices.Concat([]string{"c - Enter Code"}, styles.GlobalActions)
	footer := lg.NewStyle().Render(styles.RenderActionFooter(footerActions))

	return tea.NewView(views.RenderCenteredLayout(m.global.Width, m.global.Height, titleText, content, footer))
}
