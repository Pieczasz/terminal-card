package lobby

import (
	"context"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/game"
	"github.com/Pieczasz/terminal-card/internal/game/crazyeight"
	"github.com/Pieczasz/terminal-card/internal/lobby"
	"github.com/Pieczasz/terminal-card/internal/player"
	"github.com/Pieczasz/terminal-card/internal/tui/router"

	tea "charm.land/bubbletea/v2"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

const testGameName = "Crazy Eights"

func testUser(id uint, name string) *db.User {
	return &db.User{Model: gorm.Model{ID: id}, Username: name}
}

func testRegistry() *game.Registry {
	r := game.NewRegistry()
	r.RegisterModule(game.Module{
		Name:    testGameName,
		Slug:    "crazy_eights",
		Factory: func() game.Rules { return &crazyeight.Rules{} },
	})
	return r
}

// leaderView returns the lobby view as seen by the lobby's leader.
func leaderView(t *testing.T) (*model, *lobby.Lobby) {
	t.Helper()
	manager := lobby.NewManager(context.Background(), nil)
	leaderUser := testUser(1, "alice")
	leader := &player.Player{ID: "1", DatabaseUser: leaderUser}

	l, err := manager.New(leader,
		lobby.WithCardGame(&db.Game{Name: testGameName}),
		lobby.WithMaxPlayers(4),
		lobby.WithPrivate(false),
	)
	require.NoError(t, err)

	global := router.GlobalContext{
		User:         leaderUser,
		LobbyManager: manager,
		GameRegistry: testRegistry(),
		Width:        120,
		Height:       40,
	}
	m, ok := New(global, l).(*model)
	require.True(t, ok)
	return m, l
}

// keyMsg builds the message Bubble Tea would deliver for a keystroke. Named keys
// need their own Code and carry no Text; building one from key[0] would turn "esc"
// into the letter 'e'.
func keyMsg(key string) tea.KeyPressMsg {
	switch key {
	case "esc":
		return tea.KeyPressMsg{Code: tea.KeyEscape}
	case "enter":
		return tea.KeyPressMsg{Code: tea.KeyEnter}
	case "space":
		return tea.KeyPressMsg{Code: tea.KeySpace, Text: " "}
	default:
		return tea.KeyPressMsg{Code: rune(key[0]), Text: key}
	}
}

func press(m *model, key string) (tea.Model, tea.Cmd) {
	return m.Update(keyMsg(key))
}

// routeOf runs a returned command and reports the route it navigates to.
func routeOf(t *testing.T, cmd tea.Cmd) string {
	t.Helper()
	require.NotNil(t, cmd)
	change, ok := cmd().(router.ChangeViewMsg)
	require.True(t, ok, "command did not produce a ChangeViewMsg")
	return change.ViewName
}

func TestHandleKey_Navigation(t *testing.T) {
	t.Parallel()

	cases := map[string]string{
		"n": router.RouteLobbyCreate,
		"f": router.RouteLobbyJoin,
		"p": router.RouteProfile,
		"t": router.RouteLeaderboard,
	}

	for key, want := range cases {
		t.Run("key_"+key, func(t *testing.T) {
			t.Parallel()
			m, _ := leaderView(t)
			_, cmd := press(m, key)
			assert.Equal(t, want, routeOf(t, cmd))
			assert.Nil(t, m.lobbyChan, "navigating away releases the lobby subscription")
		})
	}
}

func TestHandleKey_LeaveConfirmFlow(t *testing.T) {
	t.Parallel()
	m, _ := leaderView(t)

	_, cmd := press(m, "x")
	assert.True(t, m.showLeaveConfirm)
	assert.Nil(t, cmd, "asking for confirmation does not navigate")

	// Declining returns to the lobby and keeps the subscription.
	_, cmd = press(m, "n")
	assert.False(t, m.showLeaveConfirm)
	assert.Nil(t, cmd)
	assert.NotNil(t, m.lobbyChan)

	// Confirming leaves and goes home.
	press(m, "x")
	_, cmd = press(m, "y")
	assert.Equal(t, router.RouteHome, routeOf(t, cmd))
	assert.Nil(t, m.lobbyChan)
}

func TestHandleKey_CursorStaysInBounds(t *testing.T) {
	t.Parallel()
	m, _ := leaderView(t)

	for range 10 {
		press(m, "k")
	}
	assert.Zero(t, m.cursor, "cursor never moves above the first row")

	for range 10 {
		press(m, "j")
	}
	assert.Equal(t, cursorMode, m.cursor, "with no guests the last row is the mode row")
}

func TestAdjustSetting_MaxPlayersRespectsRulesBounds(t *testing.T) {
	t.Parallel()
	m, l := leaderView(t)

	rulesMin, rulesMax := m.gamePlayerBounds()
	require.Equal(t, 2, rulesMin)
	require.Equal(t, 6, rulesMax)

	m.cursor = cursorMaxPlayers
	for range 10 {
		press(m, "l")
	}
	assert.Equal(t, rulesMax, m.maxPlayers)
	assert.Equal(t, rulesMax, l.MaxPlayers(), "the lobby is updated, not just the view")

	for range 10 {
		press(m, "h")
	}
	assert.Equal(t, rulesMin, m.maxPlayers)
}

func TestAdjustSetting_TogglesVisibilityAndMode(t *testing.T) {
	t.Parallel()
	m, l := leaderView(t)

	m.cursor = cursorVisibility
	press(m, "l")
	assert.True(t, m.isPrivate)
	assert.True(t, l.IsPrivate())

	m.cursor = cursorMode
	press(m, "l")
	assert.True(t, m.isRanked)
	assert.True(t, l.IsRanked())
}

// The game row is deliberately fixed once a lobby exists.
func TestAdjustSetting_GameRowIsNoOp(t *testing.T) {
	t.Parallel()
	m, _ := leaderView(t)

	m.cursor = cursorGame
	before := m.gameIndex
	press(m, "l")
	press(m, "h")
	assert.Equal(t, before, m.gameIndex)
}

func TestHandleLobbyEvent_ClosedGoesHome(t *testing.T) {
	t.Parallel()
	m, _ := leaderView(t)

	_, cmd := m.Update(lobbyMsg(lobby.Event{Type: lobby.EventLobbyClosed}))
	assert.Equal(t, router.RouteHome, routeOf(t, cmd))
	assert.Nil(t, m.lobbyChan)
}

// An unregistered game must not eject the player, and must leave the listener
// armed so the view keeps receiving lobby events.
func TestHandleLobbyEvent_UnknownGameKeepsListening(t *testing.T) {
	t.Parallel()
	m, _ := leaderView(t)
	m.global.GameRegistry = game.NewRegistry() // game no longer registered

	engine := game.NewEngine(&crazyeight.Rules{},
		[]*player.Player{{ID: "1"}, {ID: "2"}}, nil)

	_, cmd := m.Update(lobbyMsg(lobby.Event{Type: lobby.EventGameStarted, Payload: engine}))
	require.NotNil(t, cmd, "listener must stay armed")
	assert.NotNil(t, m.lobbyChan, "subscription is retained")
}

func TestView_RendersLobbyAndConfirmPopup(t *testing.T) {
	t.Parallel()
	m, _ := leaderView(t)

	out := m.View().Content
	assert.Contains(t, out, "alice")
	assert.Contains(t, out, "Max Players")
	assert.Contains(t, out, "Leave Lobby")

	m.showLeaveConfirm = true
	assert.Contains(t, m.View().Content, "Are you sure")
}

func TestView_NoActiveLobby(t *testing.T) {
	t.Parallel()
	m, ok := New(router.GlobalContext{}, nil).(*model)
	require.True(t, ok)
	assert.Contains(t, m.View().Content, "No active lobby")
}
