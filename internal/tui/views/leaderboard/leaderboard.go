package leaderboard

import (
	"context"
	"fmt"
	"log/slog"
	"strings"
	"terminalcard/internal/tui/views"

	tea "github.com/charmbracelet/bubbletea"
	lg "github.com/charmbracelet/lipgloss"

	"terminalcard/internal/db"
	"terminalcard/internal/tui/router"
	"terminalcard/internal/tui/styles"
)

const (
	maxLeaderboardPlayers = 20
	extraVerticalLines    = 2
	minPlayerWidth        = 10
	fixedDataWidth        = 34 // Rank(5) + Game(15) + Elo(5) + dividers(9)
	colMargin             = 4
	minColWidth           = fixedDataWidth + minPlayerWidth
)

type model struct {
	global   router.GlobalContext
	rankings []db.Ranking
	err      error
}

func New(global router.GlobalContext) tea.Model {
	return model{global: global}
}

type loadedMsg struct {
	rankings []db.Ranking
	err      error
}

func (m model) Init() tea.Cmd {
	return func() tea.Msg {
		rankings, err := m.global.UserRepository.GetBestPlayers(context.Background(), maxLeaderboardPlayers)
		return loadedMsg{rankings: rankings, err: err}
	}
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := views.HandleCommonMsg(msg, &m.global); handled {
		return m, cmd
	}
	switch msg := msg.(type) {
	case loadedMsg:
		m.rankings = msg.rankings
		m.err = msg.err
		if msg.err != nil {
			slog.Error("database error while fetching leaderboard", "error", msg.err)
		}
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
		case "t":
			return m, func() tea.Msg { return router.ChangeViewMsg{ViewName: "leaderboard"} }
		}
	}
	return m, nil
}

func (m model) View() string {
	innerWidth := styles.GetInnerWidth(m.global.Width)
	titleFig := styles.RenderFigureASCII("Leaderboard", innerWidth)
	titleText := styles.Title.Render(titleFig)
	footer := lg.NewStyle().Render(styles.RenderActionFooter(styles.GlobalActions))

	var content string

	if m.err != nil {
		content = m.renderError()
	} else if m.rankings == nil {
		content = m.renderLoading()
	} else if len(m.rankings) == 0 {
		content = m.renderEmpty()
	} else {
		contentHeight := styles.GetAvailableContentHeight(m.global.Height, titleText, footer)
		contentWidth := styles.GetAvailableContentWidth(m.global.Width)
		content = m.renderRankings(contentWidth, contentHeight)
	}

	return views.RenderCenteredLayout(m.global.Width, m.global.Height, titleText, content, footer)
}

func (m model) renderError() string {
	return lg.JoinVertical(lg.Center, fmt.Sprintf("Error loading leaderboard: %v", m.err))
}

func (m model) renderLoading() string {
	return lg.JoinVertical(lg.Center, "Loading leaderboard.")
}

func (m model) renderEmpty() string {
	return lg.JoinVertical(lg.Center, "No players have ranked yet.")
}

func (m model) renderRankings(contentWidth, contentHeight int) string {
	maxItemsPerCol := max(contentHeight-extraVerticalLines, 1)
	neededCols := max((len(m.rankings)+maxItemsPerCol-1)/maxItemsPerCol, 1)

	maxFitCols := max(contentWidth/(minColWidth+colMargin), 1)
	colsToRender := min(neededCols, maxFitCols)

	availableColWidth := (contentWidth - (colsToRender-1)*colMargin) / colsToRender
	playerWidth := min(max(availableColWidth-fixedDataWidth, minPlayerWidth), 16)

	var allCols []string
	var currentRows []string

	for i, r := range m.rankings {
		if len(currentRows) == 0 {
			currentRows = append(currentRows, m.renderHeaderRow(playerWidth))
			currentRows = append(currentRows, strings.Repeat("-", fixedDataWidth+playerWidth))
		}

		currentRows = append(currentRows, m.renderPlayerRow(i, r, playerWidth))

		if len(currentRows) >= maxItemsPerCol+2 || i == len(m.rankings)-1 {
			col := lg.JoinVertical(lg.Left, currentRows...)
			if len(allCols) > 0 {
				col = lg.NewStyle().MarginLeft(colMargin).Render(col)
			}
			allCols = append(allCols, col)
			currentRows = nil
			if len(allCols) >= colsToRender {
				break
			}
		}
	}

	return lg.JoinVertical(lg.Center, lg.JoinHorizontal(lg.Top, allCols...))
}

func (m model) renderHeaderRow(playerWidth int) string {
	return lg.NewStyle().Foreground(lg.Color("#FFA500")).Bold(true).Render(
		fmt.Sprintf("%-5s | %-*s | %-15s | %s", "Rank", playerWidth, "Player", "Game", "Elo"),
	)
}

func (m model) renderPlayerRow(index int, r db.Ranking, playerWidth int) string {
	userStr := r.User.Username
	if len(userStr) > playerWidth {
		userStr = userStr[:playerWidth-3] + "..."
	} else {
		userStr = fmt.Sprintf("%-*s", playerWidth, userStr)
	}

	if r.User.ID == m.global.User.ID {
		userStr = lg.NewStyle().Foreground(lg.Color("205")).Bold(true).Render(userStr)
	}
	return fmt.Sprintf("%-5d | %s | %-15s | %d", index+1, userStr, r.Game.Name, r.Elo)
}
