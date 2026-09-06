package ssh

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/lobby"

	tea "charm.land/bubbletea/v2"
	"charm.land/ssh"
	"charm.land/wish/v2/testsession"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	gossh "golang.org/x/crypto/ssh"
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
	user := &db.User{ID: 11, Username: "shared"}
	gen, err := tracker.Connect(user.ID)
	require.NoError(t, err)
	deps := ServerDependencies{LobbyManager: lobby.NewManager(context.Background(), nil)}

	// Both channels of one connection, so they would share an ssh.Context.
	accepted := &fakeSession{}
	rejected := &fakeSession{}

	modelClosed := false
	sessionStates.Store(accepted, &sessionState{
		owns:  true,
		user:  user,
		gen:   gen,
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

// releaseSession must give up the lobby seat before the tracker slot when the
// session still owns its generation. A displaced reconnect must not tear down the
// seat the replacement is about to resume.
func TestReleaseSession_GivesUpTheSeatBeforeTheSlot(t *testing.T) {
	t.Parallel()

	manager := lobby.NewManager(context.Background(), nil)
	host := &db.User{ID: 1, Username: "host"}
	guest := &db.User{ID: 2, Username: "guest"}
	guestPlayer := lobby.NewPlayer(guest)

	table, err := manager.New(lobby.NewPlayer(host), lobby.WithCardGame("Mock"))
	require.NoError(t, err)
	require.NoError(t, manager.JoinLobbyByCode(table.Code(), guestPlayer))

	tracker := NewSessionTracker(0)
	oldGen, err := tracker.Connect(guest.ID)
	require.NoError(t, err)
	deps := ServerDependencies{LobbyManager: manager}

	// The reconnect displaces the zombie session before teardown runs, so
	// releaseSession sees a stale generation and leaves the seat alone.
	reconnected := make(chan struct{})
	displaced := make(chan struct{})
	reconnect := func() {
		defer close(reconnected)
		_, err := tracker.Connect(guest.ID)
		if err != nil {
			t.Errorf("displace reconnect failed: %v", err)
			return
		}
		close(displaced)
		if resumed := manager.ResumePlayer(guestPlayer); resumed != table {
			t.Errorf("takeover did not resume the waiting seat: got %v", resumed)
		}
	}

	srv := &ssh.Server{
		Handler: sessionLifecycle(deps, tracker)(func(s ssh.Session) {
			st, ok := lookupSessionState(s)
			require.True(t, ok)
			st.owns = true
			st.user = guest
			st.gen = oldGen
			go reconnect()
			<-displaced
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
	_, err := tracker.Connect(1)
	require.NoError(t, err)
	_, err = tracker.Connect(2)
	require.NoError(t, err)
	_, err = tracker.Connect(3)
	require.ErrorIs(t, err, ErrServerFull)

	// A second session for an already-connected account displaces rather than
	// failing: half-open TCP otherwise blocks the mid-game reconnect grace.
	gen1, err := tracker.Connect(1)
	require.NoError(t, err)
	gen2, err := tracker.Connect(1)
	require.NoError(t, err)
	assert.NotEqual(t, gen1, gen2)
	assert.False(t, tracker.Release(1, gen1), "stale generation must not free the slot")
	assert.Equal(t, 2, tracker.Count())
	assert.True(t, tracker.Release(1, gen2))

	tracker.Disconnect(2)
	_, err = tracker.Connect(3)
	require.NoError(t, err, "capacity frees with the seat")
}

// panicModel panics from whichever method the test asks for.
type panicModel struct{ on string }

func (m panicModel) Init() tea.Cmd {
	if m.on == "init" {
		panic("init boom")
	}
	return nil
}

func (m panicModel) Update(tea.Msg) (tea.Model, tea.Cmd) {
	if m.on == "update" {
		panic("update boom")
	}
	return m, nil
}

func (m panicModel) View() tea.View {
	if m.on == "view" {
		panic("view boom")
	}
	return tea.NewView("")
}

// A TUI panic has to be reported against the session's own span before it leaves
// our code: bubbletea's recover is what keeps it off the process, but it knows
// nothing about the span, the metric or the trace.
func TestReportingModel_RecordsPanicsAndLetsThemUnwind(t *testing.T) {
	t.Parallel()

	for _, method := range []string{"init", "update", "view"} {
		t.Run(method, func(t *testing.T) {
			t.Parallel()

			s := &fakeSession{}
			st := &sessionState{traceCtx: context.Background()}
			sessionStates.Store(s, st)
			t.Cleanup(func() { sessionStates.Delete(s) })

			m := reportingModel{Model: panicModel{on: method}, session: s}
			require.Panics(t, func() {
				switch method {
				case "init":
					m.Init()
				case "update":
					m.Update(nil)
				case "view":
					m.View()
				}
			}, "the panic still unwinds, so bubbletea ends the program")

			assert.True(t, st.panicked, "the session is marked as having panicked")
		})
	}
}

// A model that does not panic must pass its command and view through untouched.
func TestReportingModel_PassesThroughWhenNothingPanics(t *testing.T) {
	t.Parallel()

	s := &fakeSession{}
	sessionStates.Store(s, &sessionState{traceCtx: context.Background()})
	t.Cleanup(func() { sessionStates.Delete(s) })

	m := reportingModel{Model: panicModel{on: "none"}, session: s}

	assert.Nil(t, m.Init())
	got, cmd := m.Update(nil)
	assert.Nil(t, cmd)
	assert.IsType(t, reportingModel{}, got, "the wrapper survives an update")
}
