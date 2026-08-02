package main

import (
	"context"
	"errors"
	"net"
	"testing"
	"time"

	"github.com/Pieczasz/terminal-card/internal/config"

	charmssh "charm.land/ssh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// fakeServer stands in for the wish server so serve's accept-loop failure paths are
// reachable; a real *charmssh.Server blocks in Accept and never takes them.
type fakeServer struct {
	serveErr error
}

func (f *fakeServer) Serve(net.Listener) error       { return f.serveErr }
func (f *fakeServer) Shutdown(context.Context) error { return nil }

func testConfig() *config.Config {
	return &config.Config{ServerHost: "127.0.0.1", ServerPort: 0, MaxConnections: 4}
}

// runServe calls serve and fails the test if it blocks, which is the regression
// being guarded: the old code logged the error and then waited on the signal
// channel forever, leaving a live process nobody could log in to.
func runServe(t *testing.T, server sshServer) error {
	t.Helper()
	errCh := make(chan error, 1)
	go func() { errCh <- serve(context.Background(), testConfig(), server) }()

	select {
	case err := <-errCh:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("serve blocked after the accept loop ended instead of returning")
		return nil
	}
}

func TestServe_AcceptLoopFailureIsReturned(t *testing.T) {
	t.Parallel()
	boom := errors.New("listener exploded")
	err := runServe(t, &fakeServer{serveErr: boom})

	require.Error(t, err)
	require.ErrorIs(t, err, boom, "the cause must survive so operators can see it")
	assert.Contains(t, err.Error(), "accept loop failed")
}

// ErrServerClosed without a shutdown signal still means the server stopped serving,
// but it must not be dressed up as an accept-loop failure.
func TestServe_UnexpectedCleanStopIsReturned(t *testing.T) {
	t.Parallel()
	err := runServe(t, &fakeServer{serveErr: charmssh.ErrServerClosed})

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "accept loop failed")
	assert.Contains(t, err.Error(), "unexpectedly")
}
