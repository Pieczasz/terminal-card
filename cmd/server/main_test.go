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

type fakeServer struct {
	serveErr    error
	shutdownErr error
	closeErr    error
	closed      bool
}

func (f *fakeServer) Serve(net.Listener) error       { return f.serveErr }
func (f *fakeServer) Shutdown(context.Context) error { return f.shutdownErr }

func (f *fakeServer) Close() error {
	f.closed = true
	return f.closeErr
}

func testConfig() *config.Config {
	return &config.Config{ServerHost: "127.0.0.1", ServerPort: 0, MaxConnections: 4}
}

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

// Sessions outliving the drain window is what a card game does, so it must not stop
// the shutdown: the listener and the connections have to be let go either way.
func TestStopServer_ClosesOnEveryPath(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		server *fakeServer
	}{
		{name: "drained in time", server: &fakeServer{}},
		{name: "drain deadline passed", server: &fakeServer{shutdownErr: context.DeadlineExceeded}},
		{name: "close itself fails", server: &fakeServer{shutdownErr: context.DeadlineExceeded, closeErr: net.ErrClosed}},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()
			stopServer(tt.server)

			assert.True(t, tt.server.closed, "the server was never closed")
		})
	}
}

func TestServe_UnexpectedCleanStopIsReturned(t *testing.T) {
	t.Parallel()
	err := runServe(t, &fakeServer{serveErr: charmssh.ErrServerClosed})

	require.Error(t, err)
	assert.NotContains(t, err.Error(), "accept loop failed")
	assert.Contains(t, err.Error(), "unexpectedly")
}
