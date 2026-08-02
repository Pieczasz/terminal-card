package profile

import (
	"context"
	"fmt"
	"log/slog"
	"strconv"
	"time"

	"github.com/Pieczasz/terminal-card/internal/tui/views"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/tui/router"
	"github.com/Pieczasz/terminal-card/internal/tui/styles"
)

type model struct {
	global      router.GlobalContext
	userProfile *db.User
	history     []db.MatchParticipant
	err         error
}

func New(global router.GlobalContext) tea.Model {
	return model{global: global}
}

type profileLoadedMsg struct {
	user    *db.User
	history []db.MatchParticipant
	err     error
}

func loadProfile(ctx context.Context, userRepo db.UserRepository, userID uint) tea.Cmd {
	return func() tea.Msg {
		reqCtx, cancel := context.WithTimeout(ctx, 5*time.Second)
		defer cancel()
		user, err := userRepo.UserProfile(reqCtx, userID)
		if err != nil {
			return profileLoadedMsg{err: err}
		}
		history, err := userRepo.UserMatchHistory(reqCtx, userID, 10)
		return profileLoadedMsg{user: user, history: history, err: err}
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
		if msg.err != nil {
			slog.Error("database error while loading user profile", "error", msg.err)
		}
	case tea.KeyPressMsg:
		if cmd, ok := views.NavigateOn(msg.String()); ok {
			return m, cmd
		}
	}
	return m, nil
}

func (m model) View() tea.View {
	titleFig := styles.RenderFigureASCII("User Profile", styles.InnerWidth(m.global.Width))
	titleText := styles.Title.Render(titleFig)
	header := styles.Title.Render(titleText)
	footer := lg.NewStyle().Render(styles.RenderActionFooter(styles.GlobalActions))

	content := m.renderContent(header, footer)
	return tea.NewView(views.RenderCenteredLayout(m.global.Width, m.global.Height, header, content, footer))
}

func (m model) renderContent(header, footer string) string {
	if m.err != nil {
		return "Unable to load profile. Please try again."
	}
	if m.userProfile == nil {
		return "Loading profile..."
	}

	const extraVerticalLines = 3 // UserInfo, spacer, header
	maxItems := max(styles.AvailableContentHeight(m.global.Height, header, footer)-extraVerticalLines, 1)

	rankingsCol := lg.NewStyle().Align(lg.Left).MarginRight(6).
		Render(lg.JoinVertical(lg.Left, m.rankingRows(maxItems)...))
	historyCol := lg.NewStyle().Align(lg.Left).
		Render(lg.JoinVertical(lg.Left, m.historyRows(maxItems)...))
	tables := lg.JoinHorizontal(lg.Top, rankingsCol, historyCol)

	userInfo := lg.NewStyle().Align(lg.Center).Width(lg.Width(tables)).
		Render(fmt.Sprintf("Profile for: %s", m.userProfile.Username))

	return lg.NewStyle().Align(lg.Left).Render(lg.JoinVertical(lg.Left, userInfo, "", tables))
}

func (m model) rankingRows(maxItems int) []string {
	rows := make([]string, 0, 1+len(m.userProfile.Rankings))
	rows = append(rows, styles.SectionHeading.Render("Rankings:"))
	if len(m.userProfile.Rankings) == 0 {
		return append(rows, "No games played yet.")
	}
	for i, r := range m.userProfile.Rankings {
		if i >= maxItems {
			return append(rows, "... and more")
		}
		rows = append(rows, fmt.Sprintf("%s: Elo %d", r.Game.Name, r.Elo))
	}
	return rows
}

func (m model) historyRows(maxItems int) []string {
	rows := make([]string, 0, 1+len(m.history))
	rows = append(rows, styles.SectionHeading.Render("Recent Matches:"))
	if len(m.history) == 0 {
		return append(rows, "No recent matches.")
	}
	for i, h := range m.history {
		if i >= maxItems {
			return append(rows, "... and more")
		}
		rows = append(rows, fmt.Sprintf("%s: %s (%s)",
			h.Match.Game.Name, placementLabel(h.Placement), resultLabel(h)))
	}
	return rows
}

// resultLabel says what the match was worth: a rating swing for a ranked game,
// and plainly "casual" for one that was never going to move Elo.
func resultLabel(h db.MatchParticipant) string {
	if !h.Match.Ranked {
		return lg.NewStyle().Foreground(lg.Color("250")).Render("casual game")
	}
	return "Elo change: " + eloDeltaLabel(h.EloDelta)
}

func eloDeltaLabel(delta int) string {
	if delta >= 0 {
		return lg.NewStyle().Foreground(lg.Color("46")).Render(fmt.Sprintf("+%d", delta))
	}
	return lg.NewStyle().Foreground(lg.Color("9")).Render(strconv.Itoa(delta))
}

func placementLabel(placement int) string {
	switch placement {
	case 1:
		return lg.NewStyle().Foreground(lg.Color("226")).Render("1st place")
	case 2:
		return lg.NewStyle().Foreground(lg.Color("250")).Render("2nd place")
	case 3:
		return lg.NewStyle().Foreground(lg.Color("130")).Render("3rd place")
	default:
		return fmt.Sprintf("%d place", placement)
	}
}
