package ssh

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"net"
	"testing"
	"time"

	"terminalcard/internal/config"
	"terminalcard/internal/db"
	"terminalcard/internal/game"
	"terminalcard/internal/lobby"
	"terminalcard/internal/repository"
	"terminalcard/internal/testutil"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	cryptossh "golang.org/x/crypto/ssh"
)

func getFreePort() (int, error) {
	addr, err := net.ResolveTCPAddr("tcp", "localhost:0")
	if err != nil {
		return 0, err
	}
	l, err := net.ListenTCP("tcp", addr)
	if err != nil {
		return 0, err
	}
	defer l.Close()
	return l.Addr().(*net.TCPAddr).Port, nil
}

func generateSigner(t *testing.T) cryptossh.Signer {
	_, privKey, err := ed25519.GenerateKey(rand.Reader)
	require.NoError(t, err)

	signer, err := cryptossh.NewSignerFromKey(privKey)
	require.NoError(t, err)
	return signer
}

type testEnv struct {
	userRepo db.UserRepository
	addr     string
	cleanup  func()
}

func setupTestEnvironment(t *testing.T) testEnv {
	t.Helper()
	gormDB := testutil.SetupTestDB(t, &db.User{}, &db.PublicKey{}, &db.Ranking{})
	userRepo := repository.NewUserRepository(gormDB)
	matchRepo := repository.NewMatchRepository(gormDB)

	port, err := getFreePort()
	require.NoError(t, err)

	deps := ServerDependencies{
		Config: &config.Config{
			ServerPort:      port,
			SSHKeyPath:      t.TempDir() + "/id_ed25519",
			RateLimitCount:  5,
			RateLimitWindow: time.Second,
		},
		UserRepository:  userRepo,
		MatchRepository: matchRepo,
		LobbyManager:    lobby.NewManager(matchRepo),
		GameRegistry:    game.NewRegistry(),
	}

	server, err := SetupServer(deps)
	require.NoError(t, err)

	go func() {
		_ = server.ListenAndServe()
	}()

	addr := fmt.Sprintf("127.0.0.1:%d", port)
	require.Eventually(t, func() bool {
		conn, err := net.DialTimeout("tcp", addr, 50*time.Millisecond)
		if err != nil {
			return false
		}
		_ = conn.Close()
		return true
	}, 2*time.Second, 20*time.Millisecond, "ssh server did not become ready")

	return testEnv{
		userRepo: userRepo,
		addr:     addr,
		cleanup:  func() { server.Close() },
	}
}

func TestServer_NewUserConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SSH integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnvironment(t)
	defer env.cleanup()

	signer := generateSigner(t)
	clientConfig := &cryptossh.ClientConfig{
		User: "testuser_new",
		Auth: []cryptossh.AuthMethod{
			cryptossh.PublicKeys(signer),
		},
		HostKeyCallback: cryptossh.InsecureIgnoreHostKey(),
	}

	client, err := cryptossh.Dial("tcp", env.addr, clientConfig)
	require.NoError(t, err)

	session, err := client.NewSession()
	require.NoError(t, err)
	defer session.Close()

	err = session.RequestPty("xterm", 80, 40, cryptossh.TerminalModes{})
	require.NoError(t, err)
	err = session.Shell()
	require.NoError(t, err)

	var user *db.User
	var key *db.PublicKey
	require.Eventually(t, func() bool {
		user, key, _ = env.userRepo.LoadUserByFingerprint(context.Background(), cryptossh.FingerprintSHA256(signer.PublicKey()))
		return user != nil
	}, 2*time.Second, 50*time.Millisecond, "user was not registered")

	require.NotNil(t, user)
	require.NotNil(t, key)
	require.Equal(t, "testuser_new", user.Username)
}

func TestServer_UserAlreadyConnected(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SSH integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnvironment(t)
	defer env.cleanup()

	signer := generateSigner(t)
	clientConfig := &cryptossh.ClientConfig{
		User:            "testuser_dup",
		Auth:            []cryptossh.AuthMethod{cryptossh.PublicKeys(signer)},
		HostKeyCallback: cryptossh.InsecureIgnoreHostKey(),
	}

	client1, err := cryptossh.Dial("tcp", env.addr, clientConfig)
	require.NoError(t, err)
	defer client1.Close()
	session1, err := client1.NewSession()
	require.NoError(t, err)
	defer session1.Close()

	_ = session1.RequestPty("xterm", 80, 40, cryptossh.TerminalModes{})
	_ = session1.Shell()

	require.Eventually(t, func() bool {
		user, _, _ := env.userRepo.LoadUserByFingerprint(context.Background(), cryptossh.FingerprintSHA256(signer.PublicKey()))
		return user != nil
	}, 2*time.Second, 50*time.Millisecond)

	client2, err := cryptossh.Dial("tcp", env.addr, clientConfig)
	require.NoError(t, err)
	defer client2.Close()

	session2, err := client2.NewSession()
	require.NoError(t, err)
	defer session2.Close()

	_ = session2.RequestPty("xterm", 80, 40, cryptossh.TerminalModes{})
	_ = session2.Shell()

	err = session2.Wait()
	assert.Error(t, err, "expected error because user is already connected")
}

func TestServer_ExistingUserConnection(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SSH integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnvironment(t)
	defer env.cleanup()

	signer := generateSigner(t)
	clientConfig := &cryptossh.ClientConfig{
		User:            "testuser_exist",
		Auth:            []cryptossh.AuthMethod{cryptossh.PublicKeys(signer)},
		HostKeyCallback: cryptossh.InsecureIgnoreHostKey(),
	}

	client1, err := cryptossh.Dial("tcp", env.addr, clientConfig)
	require.NoError(t, err)
	session1, err := client1.NewSession()
	require.NoError(t, err)
	_ = session1.RequestPty("xterm", 80, 40, cryptossh.TerminalModes{})
	_ = session1.Shell()

	require.Eventually(t, func() bool {
		user, _, _ := env.userRepo.LoadUserByFingerprint(context.Background(), cryptossh.FingerprintSHA256(signer.PublicKey()))
		return user != nil
	}, 2*time.Second, 50*time.Millisecond)

	session1.Close()
	client1.Close()

	require.Eventually(t, func() bool {
		client2, err := cryptossh.Dial("tcp", env.addr, clientConfig)
		if err != nil {
			return false
		}
		session2, err := client2.NewSession()
		if err != nil {
			client2.Close()
			return false
		}
		err = session2.Shell()
		session2.Close()
		client2.Close()
		return err == nil
	}, 2*time.Second, 50*time.Millisecond, "reconnect after disconnect failed")
}

func TestServer_RateLimit(t *testing.T) {
	if testing.Short() {
		t.Skip("skipping SSH integration test in short mode")
	}
	t.Parallel()

	env := setupTestEnvironment(t)
	defer env.cleanup()

	signer := generateSigner(t)
	clientConfig := &cryptossh.ClientConfig{
		User:            "testuser_limit",
		Auth:            []cryptossh.AuthMethod{cryptossh.PublicKeys(signer)},
		HostKeyCallback: cryptossh.InsecureIgnoreHostKey(),
	}

	// Rate limiter allows 5 per second. 6th should fail.
	var failed bool
	for i := 0; i < 6; i++ {
		client, err := cryptossh.Dial("tcp", env.addr, clientConfig)
		if err != nil {
			failed = true
			break
		}
		session, err := client.NewSession()
		if err != nil {
			failed = true
			break
		}

		err = session.Shell()
		if err != nil {
			failed = true
			break
		}
		session.Close()
		client.Close()
	}

	assert.True(t, failed, "Expected rate limit to block connection")
}

func TestSetupServer_Errors(t *testing.T) {
	t.Parallel()

	deps := ServerDependencies{
		Config: &config.Config{SSHKeyPath: "/invalid/path/that/doesnt/exist"},
	}
	_, err := SetupServer(deps)
	assert.ErrorContains(t, err, "error while saving keypair")
}
