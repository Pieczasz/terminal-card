package ssh

import (
	"context"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/lobby"

	"charm.land/ssh"
	"charm.land/wish/v2/testsession"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
	"gorm.io/gorm"
)

// fakeSession is enough of an ssh.Session for the teardown helpers, which only ever
// look the session up in sessionStates. Embedding the interface leaves every other
// method nil on purpose: calling one is a bug in the test, not a silent pass.
type fakeSession struct {
	ssh.Session
	ctx ssh.Context
}

func (f *fakeSession) Context() ssh.Context { return f.ctx }

type recordingCloser struct{ closed *bool }

func (c recordingCloser) Close() { *c.closed = true }

// charm ssh hands one Context per TCP connection to every channel opened on it, so
// session state kept there is shared by sessions that must not see each other: a
// rejected second channel's teardown used to close the first channel's model and
// hand back its tracker slot.
func TestSessionState_IsPerChannelNotPerConnection(t *testing.T) {
	t.Parallel()

	tracker := NewSessionTracker(0)
	user := &db.User{Model: gorm.Model{ID: 11}, Username: "shared"}
	require.NoError(t, tracker.Connect(user.ID))
	deps := ServerDependencies{LobbyManager: lobby.NewManager(context.Background(), nil)}

	// Both channels of one connection, so they would share an ssh.Context.
	accepted := &fakeSession{}
	rejected := &fakeSession{}

	modelClosed := false
	sessionStates.Store(accepted, &sessionState{
		owns:  true,
		user:  user,
		model: recordingCloser{closed: &modelClosed},
	})
	sessionStates.Store(rejected, &sessionState{})
	t.Cleanup(func() {
		sessionStates.Delete(accepted)
		sessionStates.Delete(rejected)
	})

	closeSessionModel(rejected)
	releaseSession(rejected, deps, tracker)

	assert.False(t, modelClosed, "the rejected channel closed the accepted channel's view")
	assert.Equal(t, 1, tracker.Count(), "and freed the accepted channel's session slot")
}

// releaseSession must give up the lobby seat before the tracker slot. The other way
// round leaves a window in which a player who reconnects the instant they drop is
// admitted, takes a seat, and then has that brand-new seat torn down by the session
// that already left.
func TestReleaseSession_GivesUpTheSeatBeforeTheSlot(t *testing.T) {
	t.Parallel()

	manager := lobby.NewManager(context.Background(), nil)
	host := &db.User{Model: gorm.Model{ID: 1}, Username: "host"}
	guest := &db.User{Model: gorm.Model{ID: 2}, Username: "guest"}
	guestPlayer := lobby.NewPlayer(guest)

	table, err := manager.New(lobby.NewPlayer(host), lobby.WithCardGame(&db.Game{Name: "Mock"}))
	require.NoError(t, err)
	require.NoError(t, manager.JoinLobbyByCode(table.Code(), guestPlayer))

	tracker := NewSessionTracker(0)
	require.NoError(t, tracker.Connect(guest.ID))
	deps := ServerDependencies{LobbyManager: manager}

	// The reconnect claims the slot the moment it is free and immediately sits back
	// down. With the seat released first there is no window in which it can be
	// admitted while the old session still has a LeaveLobby left to run.
	reconnected := make(chan struct{})
	reconnect := func() {
		defer close(reconnected)
		for tracker.Connect(guest.ID) != nil {
			runtime.Gosched()
		}
		if err := manager.JoinLobbyByCode(table.Code(), guestPlayer); err != nil {
			t.Errorf("the reconnecting player could not sit back down: %v", err)
		}
	}

	srv := &ssh.Server{
		Handler: sessionLifecycle(deps, tracker)(func(s ssh.Session) {
			st, ok := lookupSessionState(s)
			require.True(t, ok)
			st.owns = true
			st.user = guest
			go reconnect()
		}),
	}
	_, _ = testsession.New(t, srv, nil).Output("")

	<-reconnected
	assert.True(t, table.HasPlayer(guestPlayer), "the reconnected session lost the seat it just took")
	assert.Equal(t, table, manager.FindLobbyByPlayer(guestPlayer), "and the index disagrees with the roster")
}

// One authenticated connection opening channels without limit is a database DoS:
// every channel loads the user with three preloads against a small pool.
func TestSessionLifecycle_CapsChannelsPerConnection(t *testing.T) {
	t.Parallel()

	admitted := make(chan struct{}, maxSessionsPerConnection+1)
	release := make(chan struct{})
	deps := ServerDependencies{LobbyManager: lobby.NewManager(context.Background(), nil)}
	srv := &ssh.Server{
		Handler: sessionLifecycle(deps, NewSessionTracker(0))(func(_ ssh.Session) {
			admitted <- struct{}{}
			<-release
		}),
	}

	addr := testsession.Listen(t, srv)
	client, err := gossh.Dial("tcp", addr, &gossh.ClientConfig{
		User:            "flooder",
		Auth:            []gossh.AuthMethod{gossh.Password("x")},
		HostKeyCallback: gossh.InsecureIgnoreHostKey(),
	})
	require.NoError(t, err)
	t.Cleanup(func() { _ = client.Close() })

	var running sync.WaitGroup
	for range maxSessionsPerConnection {
		session, err := client.NewSession()
		require.NoError(t, err)
		running.Go(func() {
			_, _ = session.Output("")
		})
	}
	require.Eventually(t, func() bool { return len(admitted) == maxSessionsPerConnection },
		2*time.Second, 10*time.Millisecond, "the cap refused a channel it should have admitted")

	over, err := client.NewSession()
	require.NoError(t, err)
	out, _ := over.CombinedOutput("")
	_ = over.Close()

	assert.Contains(t, string(out), "Too many sessions", "the channel over the cap was closed silently")
	assert.Len(t, admitted, maxSessionsPerConnection, "and it ran the handler anyway")

	close(release)
	running.Wait()
}

func TestSessionTracker_RefusesBeyondCapacityWithDistinctError(t *testing.T) {
	t.Parallel()
	tracker := NewSessionTracker(2)
	require.NoError(t, tracker.Connect(1))
	require.NoError(t, tracker.Connect(2))
	require.ErrorIs(t, tracker.Connect(3), ErrServerFull)
	// A duplicate is reported as a duplicate even on a full server: the player
	// action differs ("you are already here" vs "come back later").
	require.ErrorIs(t, tracker.Connect(1), ErrAlreadyConnected)
	tracker.Disconnect(2)
	require.NoError(t, tracker.Connect(3), "capacity frees with the seat")
}
