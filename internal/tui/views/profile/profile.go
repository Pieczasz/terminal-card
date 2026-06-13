package profile

import (
	"context"
	"fmt"
	"log/slog"
	"terminalcard/internal/tui/views"

	tea "charm.land/bubbletea/v2"
	lg "charm.land/lipgloss/v2"

	"terminalcard/internal/db"
	"terminalcard/internal/tui/router"
	"terminalcard/internal/tui/styles"
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

func loadProfile(userRepo db.UserRepository, userID uint) tea.Cmd {
	return func() tea.Msg {
		user, err := userRepo.GetUserProfile(context.Background(), userID)
		if err != nil {
			return profileLoadedMsg{err: err}
		}
		history, err := userRepo.GetUserMatchHistory(context.Background(), userID, 10)
		return profileLoadedMsg{user: user, history: history, err: err}
	}
}
func (m model) Init() tea.Cmd {
	return loadProfile(m.global.UserRepository, m.global.User.ID)
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
		switch msg.String() {
		case "esc", "q":
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
func (m model) View() tea.View {
	titleFig := styles.RenderFigureASCII("User Profile", styles.GetInnerWidth(m.global.Width))
	titleText := styles.Title.Render(titleFig)
	header := styles.Title.Render(titleText)
	footer := lg.NewStyle().Render(styles.RenderActionFooter(styles.GlobalActions))

	var content string
	if m.err != nil {
		content = fmt.Sprintf("Error loading profile: %v", m.err)
	} else if m.userProfile == nil {
		content = "Loading profile..."
	} else {
		userInfo := fmt.Sprintf("Profile for: %s", m.userProfile.Username)
		contentHeight := styles.GetAvailableContentHeight(m.global.Height, header, footer)

		const extraVerticalLines = 3 // UserInfo, spacer, header
		maxItems := max(contentHeight-extraVerticalLines, 1)

		var rankingsList []string
		rankingsList = append(rankingsList, lg.NewStyle().Foreground(lg.Color("#FFA500")).Bold(true).Render("Rankings:"))
		if len(m.userProfile.Rankings) == 0 {
			rankingsList = append(rankingsList, "No games played yet.")
		} else {
			for i, r := range m.userProfile.Rankings {
				if i >= maxItems {
					rankingsList = append(rankingsList, "... and more")
					break
				}
				rankingsList = append(rankingsList, fmt.Sprintf("%s: Elo %d", r.Game.Name, r.Elo))
			}
		}

		var historyList []string
		historyList = append(historyList, lg.NewStyle().Foreground(lg.Color("#FFA500")).Bold(true).Render("Recent Matches:"))
		if len(m.history) == 0 {
			historyList = append(historyList, "No recent matches.")
		} else {
			for i, h := range m.history {
				if i >= maxItems {
					historyList = append(historyList, "... and more")
					break
				}
				deltaStr := ""
				if h.EloDelta >= 0 {
					deltaStr = lg.NewStyle().Foreground(lg.Color("46")).Render(fmt.Sprintf("+%d", h.EloDelta))
				} else {
					deltaStr = lg.NewStyle().Foreground(lg.Color("9")).Render(fmt.Sprintf("%d", h.EloDelta))
				}

				placementStr := fmt.Sprintf("%d place", h.Placement)
				switch h.Placement {
				case 1:
					placementStr = lg.NewStyle().Foreground(lg.Color("226")).Render("1st place")
				case 2:
					placementStr = lg.NewStyle().Foreground(lg.Color("250")).Render("2nd place")
				case 3:
					placementStr = lg.NewStyle().Foreground(lg.Color("130")).Render("3rd place")
				}

				historyList = append(historyList, fmt.Sprintf("%s: %s (Elo change: %s)", h.Match.Game.Name, placementStr, deltaStr))
			}
		}

		rankingsCol := lg.NewStyle().Align(lg.Left).MarginRight(6).Render(lg.JoinVertical(lg.Left, rankingsList...))
		historyCol := lg.NewStyle().Align(lg.Left).Render(lg.JoinVertical(lg.Left, historyList...))
		tables := lg.JoinHorizontal(lg.Top, rankingsCol, historyCol)

		tablesWidth := lg.Width(tables)
		centeredUserInfo := lg.NewStyle().Align(lg.Center).Width(tablesWidth).Render(userInfo)

		content = lg.NewStyle().Align(lg.Left).Render(lg.JoinVertical(lg.Left, centeredUserInfo, "", tables))
	}
	return tea.NewView(views.RenderCenteredLayout(m.global.Width, m.global.Height, header, content, footer))
}
