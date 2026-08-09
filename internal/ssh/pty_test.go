package ssh

import (
	"context"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/lobby"

	tea "charm.land/bubbletea/v2"
	"charm.land/ssh"
	"charm.land/wish/v2/testsession"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"
)

// The client picks these numbers and the renderer allocates from them, so an
// unbounded pty-req is a remote out-of-memory rather than a cosmetic problem.
func TestBoundedPty_RefusesGeometryNoTerminalHas(t *testing.T) {
	t.Parallel()

	srv := &ssh.Server{}
	require.NoError(t, srv.SetOption(boundedPty()))

	tests := []struct {
		name   string
		window ssh.Window
		want   bool
	}{
		{name: "an ordinary terminal", window: ssh.Window{Width: 80, Height: 24}, want: true},
		{name: "a very wide one", window: ssh.Window{Width: maxTerminalWidth, Height: maxTerminalHeight}, want: true},
		{name: "zero is left to the pty layer", window: ssh.Window{}, want: true},
		{name: "one column too many", window: ssh.Window{Width: maxTerminalWidth + 1, Height: 24}, want: false},
		{name: "one row too many", window: ssh.Window{Width: 80, Height: maxTerminalHeight + 1}, want: false},
		{name: "a whole uint32 of columns", window: ssh.Window{Width: 4_294_967_295, Height: 24}, want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, srv.PtyCallback(nil, ssh.Pty{Window: tt.window}))
		})
	}
}

// window-change carries the same numbers as pty-req and arrives at any time, so the
// bound has to hold for every resize and not just for the first one.
func TestClampWindowSize(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name string
		msg  tea.Msg
		want tea.Msg
	}{
		{
			name: "a real resize is untouched",
			msg:  tea.WindowSizeMsg{Width: 120, Height: 40},
			want: tea.WindowSizeMsg{Width: 120, Height: 40},
		},
		{
			name: "an absurd resize is bounded",
			msg:  tea.WindowSizeMsg{Width: 4_000_000, Height: 3_000_000},
			want: tea.WindowSizeMsg{Width: maxTerminalWidth, Height: maxTerminalHeight},
		},
		{
			name: "only the dimension out of range moves",
			msg:  tea.WindowSizeMsg{Width: 4_000_000, Height: 40},
			want: tea.WindowSizeMsg{Width: maxTerminalWidth, Height: 40},
		},
		{
			// The filter this one replaces owned that answer; there is no shell
			// behind an ssh session to suspend into.
			name: "suspend is still answered with resume",
			msg:  tea.SuspendMsg{},
			want: tea.ResumeMsg{},
		},
		{
			name: "anything else passes through",
			msg:  tea.QuitMsg{},
			want: tea.QuitMsg{},
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			assert.Equal(t, tt.want, clampWindowSize(nil, tt.msg))
		})
	}
}

type panickyModel struct{}

func (panickyModel) Close() { panic("closing the view exploded") }

// Cleanup that panics must not cost the player their session slot: the tracker entry
// is what refuses their next login, so it is released by a defer of its own rather
// than sharing one with the view teardown.
func TestSessionLifecycle_PanicClosingTheViewStillReleasesTheSession(t *testing.T) {
	t.Parallel()
	tracker := NewSessionTracker()
	user := &db.User{Model: gorm.Model{ID: 7}}
	require.True(t, tracker.Connect(user.ID))

	deps := ServerDependencies{LobbyManager: lobby.NewManager(context.Background(), nil)}
	srv := &ssh.Server{
		Handler: sessionLifecycle(deps, tracker)(func(s ssh.Session) {
			s.Context().SetValue(ctxKeyOwnsConnection, true)
			s.Context().SetValue(ctxKeyUser, user)
			s.Context().SetValue(ctxKeyModel, panickyModel{})
		}),
	}

	// The panic is expected to be contained, so the session ending badly is fine;
	// what matters is everything the session held being handed back.
	_ = testsession.New(t, srv, nil).Run("")

	require.Eventually(t, func() bool { return tracker.Count() == 0 }, 2*time.Second, 10*time.Millisecond,
		"the session slot was stranded, so this player can never reconnect")
}
