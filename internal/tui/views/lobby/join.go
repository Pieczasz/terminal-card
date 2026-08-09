package lobby

import (
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/Pieczasz/terminal-card/internal/lobby"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"
	"github.com/Pieczasz/terminal-card/internal/tui/views"

	"charm.land/bubbles/v2/textinput"
	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"
)

// browseRefresh is how often the list re-reads the manager. It matches the
// manager's public-lobby cache window, so however many players are browsing at
// once, the tables are scanned once per window and no more.
const browseRefresh = 2 * time.Second

// visibleRows is how much of the list is on screen at a time.
const visibleRows = 10

type refreshMsg time.Time

func refreshTick() tea.Cmd {
	return tea.Tick(browseRefresh, func(t time.Time) tea.Msg { return refreshMsg(t) })
}

type joinModel struct {
	global      router.GlobalContext
	textInput   textinput.Model
	err         error
	cursor      int
	writingCode bool

	// entries is the rendered list, refreshed on a timer rather than derived per
	// frame: a lobby getter takes the lobby's lock, and a table of them would take
	// one per column per row on every keystroke.
	entries []lobby.BrowseEntry
	filter  lobby.BrowseFilter
	// games is the set of games with a table right now, for cycling the filter.
	games []string
}

func NewJoin(global router.GlobalContext) tea.Model {
	ti := textinput.New()
	ti.Placeholder = "8-character code"
	ti.CharLimit = 8
	ti.SetWidth(20)

	m := &joinModel{
		global:    global,
		textInput: ti,
		// Full tables are hidden by default: the reason to open this screen is to
		// find a seat, and a table with none is not one. Limit is the hard cap on
		// how many matching tables we keep; only visibleRows show at once.
		filter: lobby.BrowseFilter{OnlyWithRoom: true, Limit: lobby.MaxBrowseLimit},
	}
	m.refresh()
	return m
}

func (m *joinModel) Init() tea.Cmd {
	return tea.Batch(textinput.Blink, refreshTick())
}

// refresh re-reads the list and keeps the cursor on a real row. Tables appear and
// disappear underneath it as other players start games, so the cursor is clamped
// every time rather than only when the list shrinks to empty.
func (m *joinModel) refresh() {
	m.entries = m.global.LobbyManager.BrowseLobbies(views.SessionPlayer(m.global), m.filter)
	m.games = m.global.LobbyManager.GameNames()
	if m.cursor >= len(m.entries) {
		m.cursor = max(len(m.entries)-1, 0)
	}
}

func (m *joinModel) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if _, ok := msg.(refreshMsg); ok {
		m.refresh()
		return m, refreshTick()
	}

	// The shared handler claims resizes, the theme switch and ctrl+c, and nothing
	// else, so it is safe to run while a code is being typed: quitting has to work
	// from a focused text field too.
	if handled, cmd := views.HandleCommonMsg(msg, &m.global); handled {
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
	if cmd, ok := views.NavigateOn(key.String()); ok {
		return m, cmd
	}

	switch key.String() {
	case "up", "k":
		if m.cursor > 0 {
			m.cursor--
		}
	case "down", "j":
		if m.cursor < len(m.entries)-1 {
			m.cursor++
		}
	case "g":
		m.cycleGame()
	case "m":
		m.cycleMode()
	case "o":
		m.filter.OnlyWithRoom = !m.filter.OnlyWithRoom
		m.applyFilter()
	case "c":
		m.writingCode = true
		m.textInput.Focus()
		return m, textinput.Blink
	case "enter", " ":
		return m.joinSelected()
	}
	return m, nil
}

// applyFilter re-reads immediately so a filter keypress is visible now rather than
// on the next tick, and puts the cursor back at the top of a list that just changed
// out from under it.
func (m *joinModel) applyFilter() {
	m.cursor = 0
	m.refresh()
}

// cycleGame steps through "any" and each game that currently has a table. A filter
// pinned to a game nobody is playing would show an empty list with no way to tell
// why, so a game that disappears drops the filter back to any.
func (m *joinModel) cycleGame() {
	if len(m.games) == 0 {
		m.filter.GameName = ""
		m.applyFilter()
		return
	}
	current := slices.Index(m.games, m.filter.GameName)
	if current+1 >= len(m.games) {
		m.filter.GameName = ""
	} else {
		m.filter.GameName = m.games[current+1]
	}
	m.applyFilter()
}

func (m *joinModel) cycleMode() {
	switch m.filter.Mode {
	case lobby.BrowseAny:
		m.filter.Mode = lobby.BrowseRanked
	case lobby.BrowseRanked:
		m.filter.Mode = lobby.BrowseCasual
	case lobby.BrowseCasual:
		m.filter.Mode = lobby.BrowseAny
	}
	m.applyFilter()
}

func (m *joinModel) joinSelected() (tea.Model, tea.Cmd) {
	if m.cursor >= len(m.entries) {
		return m, nil
	}
	return m.joinByCode(m.entries[m.cursor].Code)
}

func (m *joinModel) joinByCode(code string) (tea.Model, tea.Cmd) {
	if code == "" {
		return m, nil
	}
	if err := m.global.LobbyManager.JoinLobbyByCode(code, views.SessionPlayer(m.global)); err != nil {
		m.err = err
		// The table may have filled or started while the list was on screen, so show
		// the player what is actually joinable now instead of a stale row.
		m.refresh()
		return m, nil
	}
	joined, err := m.global.LobbyManager.FindLobbyByCode(code)
	if err != nil || joined == nil {
		m.err = errors.New("joined lobby but failed to open it")
		return m, nil
	}
	return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: router.RouteLobby, Context: joined} }
}

// modeLabel names the current mode filter for the status line.
func (m *joinModel) modeLabel() string {
	switch m.filter.Mode {
	case lobby.BrowseRanked:
		return "ranked"
	case lobby.BrowseCasual:
		return "casual"
	case lobby.BrowseAny:
	}
	return "any"
}

func (m *joinModel) filterLine() string {
	game := "any"
	if m.filter.GameName != "" {
		game = m.filter.GameName
	}
	seats := "all tables"
	if m.filter.OnlyWithRoom {
		seats = "with seats"
	}
	return m.global.Theme.Muted.Render(fmt.Sprintf("game: %s   mode: %s   showing: %s", game, m.modeLabel(), seats))
}

// Column widths match the leaderboard table style (fixed cells + " | " separators).
// Codes stay off this list: JoinLobbyByCode never checks IsPrivate.
const (
	colGame   = 16
	colSeats  = 7
	colMode   = 6
	colRating = 4
	// tableWidth is the printable width of one data row without the cursor gutter.
	tableWidth = colGame + 3 + colSeats + 3 + colMode + 3 + colRating
)

func (m *joinModel) renderHeader() string {
	header := m.global.Theme.SectionHeading.Render(
		fmt.Sprintf(" %-*s | %-*s | %-*s | %s", colGame, "Game", colSeats, "Seats", colMode, "Mode", "Elo"),
	)
	rule := m.global.Theme.Dim.Render(" " + strings.Repeat("-", tableWidth))
	return header + "\n" + rule
}

func (m *joinModel) renderRow(entry lobby.BrowseEntry, selected bool) string {
	theme := m.global.Theme

	mode := styles.PadTruncate("casual", colMode)
	modeRendered := theme.Muted.Render(mode)
	if entry.Ranked {
		modeRendered = theme.Accented.Render(styles.PadTruncate("ranked", colMode))
	}

	seats := styles.PadTruncate(fmt.Sprintf("%d/%d", entry.Players, entry.MaxPlayers), colSeats)
	cells := fmt.Sprintf("%s | %s | %s | %s",
		styles.PadTruncate(entry.GameName, colGame),
		seats,
		modeRendered,
		styles.PadTruncate(fmt.Sprint(entry.AvgElo), colRating),
	)

	marker := " "
	if selected {
		marker = theme.PlayerItemSelected.Render(">")
	}
	return marker + cells
}

// visibleWindow is the slice of rows on screen, scrolled to keep the cursor in view.
func (m *joinModel) visibleWindow() (start, end int) {
	start = 0
	if m.cursor >= visibleRows {
		start = m.cursor - visibleRows + 1
	}
	return start, min(start+visibleRows, len(m.entries))
}

func (m *joinModel) renderList() string {
	if len(m.entries) == 0 {
		return m.global.Theme.Muted.Render("No tables match right now - press g, m or o to widen the filters.")
	}

	start, end := m.visibleWindow()
	rows := make([]string, 0, visibleRows+3)
	rows = append(rows, m.renderHeader())
	for i := start; i < end; i++ {
		rows = append(rows, m.renderRow(m.entries[i], !m.writingCode && m.cursor == i))
	}
	// Keep the list height stable so filters and the code field do not jump.
	for pad := end - start; pad < visibleRows; pad++ {
		rows = append(rows, strings.Repeat(" ", tableWidth+1))
	}
	rows = append(rows, m.global.Theme.Dim.Render(
		fmt.Sprintf(" %d-%d of %d  ↑/↓ scroll", start+1, end, len(m.entries))))
	return strings.Join(rows, "\n")
}

func (m *joinModel) View() tea.View {
	codeInputStr := "Or press 'c' to enter a private lobby code:"
	if m.writingCode {
		codeInputStr = "Entering private lobby code (press ESC to cancel):"
	}

	content := lg.JoinVertical(lg.Left,
		m.filterLine(),
		"",
		m.renderList(),
		"",
		m.global.Theme.Muted.Render(codeInputStr),
		m.textInput.View(),
	)

	if m.err != nil {
		content += m.global.Theme.ErrorText.Render(fmt.Sprintf("\nError: %v", m.err))
	}

	innerWidth := styles.InnerWidth(m.global.Width)
	titleFig := styles.RenderFigureASCII("Join Game", innerWidth)
	titleText := m.global.Theme.Title.Render(titleFig)

	footerActions := slices.Concat(
		[]string{"c - Enter Code", "g - Game", "m - Mode", "o - Seats"},
		styles.GlobalActions,
	)
	footer := m.global.Theme.RenderActionFooter(footerActions)

	return tea.NewView(views.RenderCenteredLayout(m.global, titleText, content, footer))
}
