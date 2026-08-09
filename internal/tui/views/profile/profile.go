package profile

import (
	"context"
	"fmt"
	"log/slog"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/Pieczasz/terminal-card/internal/catalog"
	"github.com/Pieczasz/terminal-card/internal/tui/views"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"
)

const (
	historyFetchLimit = 50
	filterAllGames    = "All"
	filterAllResults  = "All"
	filterWins        = "Wins"
	filterLosses      = "Losses"

	// Fixed cells so cycling filters or empty history cannot resize the layout.
	colGame   = 12 // longest catalog name today: "Crazy Eights"
	colElo    = 4
	colPlace  = 10 // "1st place"
	colResult = 14 // "Elo change: +99" / "casual game"
)

type model struct {
	global      router.GlobalContext
	userProfile *db.User
	history     []db.MatchParticipant
	err         error
	historyErr  error

	gameFilters   []string
	gameFilterIdx int
	resultFilters []string
	resultIdx     int
}

func New(global router.GlobalContext) tea.Model {
	gameFilters := make([]string, 0, 1+len(catalog.All))
	gameFilters = append(gameFilters, filterAllGames)
	for _, e := range catalog.All {
		gameFilters = append(gameFilters, e.Name)
	}
	return model{
		global:        global,
		gameFilters:   gameFilters,
		resultFilters: []string{filterAllResults, filterWins, filterLosses},
	}
}

// profileLoadedMsg keeps the two failures apart. They are two queries, and a
// player whose match history could not be read still has a profile worth showing.
type profileLoadedMsg struct {
	user       *db.User
	history    []db.MatchParticipant
	err        error
	historyErr error
}

func loadProfile(ctx context.Context, userRepo db.UserRepository, userID uint) tea.Cmd {
	return func() tea.Msg {
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		user, err := userRepo.UserProfile(reqCtx, userID)
		if err != nil {
			return profileLoadedMsg{err: err}
		}
		history, historyErr := userRepo.UserMatchHistory(reqCtx, userID, historyFetchLimit)
		return profileLoadedMsg{user: user, history: history, historyErr: historyErr}
	}
}

func (m model) Init() tea.Cmd {
	if m.global.User == nil {
		return nil
	}
	return loadProfile(m.global.RequestContext(), m.global.UserRepository, m.global.User.ID)
}

func (m model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if handled, cmd := views.HandleCommonMsg(msg, &m.global); handled {
		return m, cmd
	}
	switch msg := msg.(type) {
	case profileLoadedMsg:
		m.userProfile = msg.user
		m.history = msg.history
		m.err = msg.err
		m.historyErr = msg.historyErr
		if msg.err != nil {
			slog.Error("database error while loading user profile", "error", msg.err)
		}
		if msg.historyErr != nil {
			slog.Error("database error while loading match history", "error", msg.historyErr)
		}
	case tea.KeyPressMsg:
		switch msg.String() {
		case "g":
			m.gameFilterIdx = (m.gameFilterIdx + 1) % len(m.gameFilters)
			return m, nil
		case "r":
			m.resultIdx = (m.resultIdx + 1) % len(m.resultFilters)
			return m, nil
		}
		if cmd, ok := views.NavigateOn(msg.String()); ok {
			return m, cmd
		}
	}
	return m, nil
}

func (m model) View() tea.View {
	titleFig := styles.RenderFigureASCII("User Profile", styles.InnerWidth(m.global.Width))
	titleText := m.global.Theme.Title.Render(titleFig)
	header := titleText
	footer := m.global.Theme.RenderActionFooter(slices.Concat(
		[]string{"g - Game", "r - Result"},
		styles.GlobalActions,
	))

	content := m.renderContent(header, footer)
	return tea.NewView(views.RenderCenteredLayout(m.global, header, content, footer))
}

func (m model) renderContent(header, footer string) string {
	if m.err != nil {
		return "Unable to load profile. Please try again."
	}
	if m.userProfile == nil {
		return "Loading profile..."
	}

	const extraVerticalLines = 5 // userInfo, spacer, filter, spacer, headers
	maxItems := max(styles.AvailableContentHeight(m.global.Height, header, footer)-extraVerticalLines, 1)

	rankingsCol := lg.NewStyle().Align(lg.Left).Width(rankingsTableWidth).MarginRight(4).
		Render(lg.JoinVertical(lg.Left, m.rankingRows(maxItems)...))
	historyCol := lg.NewStyle().Align(lg.Left).Width(historyTableWidth).
		Render(lg.JoinVertical(lg.Left, m.historyRows(maxItems)...))
	tables := lg.JoinHorizontal(lg.Top, rankingsCol, historyCol)

	userInfo := fmt.Sprintf("Profile for: %s", m.userProfile.Username)
	filters := m.global.Theme.Muted.Render(fmt.Sprintf("Game: %s  Result: %s",
		styles.PadTruncate(m.gameFilters[m.gameFilterIdx], colGame),
		styles.PadTruncate(m.resultFilters[m.resultIdx], len(filterLosses)),
	))

	return lg.JoinVertical(lg.Left, userInfo, "", filters, "", tables)
}

const (
	rankingsTableWidth = colGame + 3 + colElo // "Game | Elo"
	historyTableWidth  = colGame + 3 + colPlace + 3 + colResult
)

func (m model) rankingRows(maxItems int) []string {
	rows := make([]string, 0, 2+maxItems)
	rows = append(rows,
		m.global.Theme.SectionHeading.Render(fmt.Sprintf("%-*s | %s", colGame, "Game", "Elo")),
		strings.Repeat("-", rankingsTableWidth),
	)
	if len(m.userProfile.Rankings) == 0 {
		return append(rows, styles.PadTruncate("No games yet.", rankingsTableWidth))
	}
	for i, r := range m.userProfile.Rankings {
		if i >= maxItems {
			return append(rows, "... and more")
		}
		rows = append(rows, fmt.Sprintf("%s | %s",
			styles.PadTruncate(r.Game.Name, colGame),
			styles.PadTruncate(strconv.FormatUint(uint64(r.Elo), 10), colElo),
		))
	}
	return rows
}

func (m model) filteredHistory() []db.MatchParticipant {
	out := make([]db.MatchParticipant, 0, len(m.history))
	wantGame := m.gameFilters[m.gameFilterIdx]
	wantResult := m.resultFilters[m.resultIdx]
	for _, h := range m.history {
		if wantGame != filterAllGames && h.Match.Game.Name != wantGame {
			continue
		}
		won := h.Placement == 1
		switch wantResult {
		case filterWins:
			if !won {
				continue
			}
		case filterLosses:
			if won {
				continue
			}
		}
		out = append(out, h)
	}
	return out
}

func (m model) historyRows(maxItems int) []string {
	rows := make([]string, 0, 2+maxItems)
	rows = append(rows,
		m.global.Theme.SectionHeading.Render(
			fmt.Sprintf("%-*s | %-*s | %s", colGame, "Game", colPlace, "Place", "Result")),
		strings.Repeat("-", historyTableWidth),
	)
	if m.historyErr != nil {
		return append(rows, m.global.Theme.ErrorText.Render("Unable to load match history."))
	}
	filtered := m.filteredHistory()
	if len(filtered) == 0 {
		return append(rows, styles.PadTruncate("No matches for this filter.", historyTableWidth))
	}
	for i, h := range filtered {
		if i >= maxItems {
			return append(rows, "... and more")
		}
		rows = append(rows, fmt.Sprintf("%s | %s | %s",
			styles.PadTruncate(h.Match.Game.Name, colGame),
			styles.PadTruncate(placementPlain(h.Placement), colPlace),
			styles.PadTruncate(resultPlain(h), colResult),
		))
	}
	return rows
}

func resultPlain(h db.MatchParticipant) string {
	if !h.Match.Ranked {
		return "casual game"
	}
	if h.EloDelta >= 0 {
		return fmt.Sprintf("Elo +%d", h.EloDelta)
	}
	return fmt.Sprintf("Elo %d", h.EloDelta)
}

func placementPlain(placement int) string {
	switch placement {
	case 1, 2, 3:
		return placementWords[placement-1]
	default:
		return fmt.Sprintf("%d place", placement)
	}
}

var placementWords = [3]string{"1st place", "2nd place", "3rd place"}
