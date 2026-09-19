package ssh_test

import (
	"context"
	"errors"
	"fmt"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/ssh"

	charmssh "charm.land/ssh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

type MockSession struct {
	charmssh.Session
	mock.Mock
}

func (m *MockSession) PublicKey() charmssh.PublicKey {
	args := m.Called()
	if args.Get(0) == nil {
		return nil
	}
	return args.Get(0).(charmssh.PublicKey)
}

type MockUserRepository struct {
	mock.Mock
}

func (m *MockUserRepository) LoadUserByFingerprint(ctx context.Context, fingerprint string) (*db.User, *db.PublicKey, error) {
	args := m.Called(ctx, fingerprint)
	if args.Get(0) == nil && args.Get(1) == nil {
		return nil, nil, args.Error(2)
	}
	if args.Get(0) == nil {
		return nil, args.Get(1).(*db.PublicKey), args.Error(2)
	}
	if args.Get(1) == nil {
		return args.Get(0).(*db.User), nil, args.Error(2)
	}
	return args.Get(0).(*db.User), args.Get(1).(*db.PublicKey), args.Error(2)
}

func (m *MockUserRepository) RegisterUserWithKey(ctx context.Context, username, fingerprint string) (*db.User, *db.PublicKey, error) {
	args := m.Called(ctx, username, fingerprint)
	if args.Get(0) == nil && args.Get(1) == nil {
		return nil, nil, args.Error(2)
	}
	return args.Get(0).(*db.User), args.Get(1).(*db.PublicKey), args.Error(2)
}

func (m *MockUserRepository) BestPlayers(_ context.Context, _ int, _ string) ([]db.Ranking, error) {
	return nil, nil
}

func (m *MockUserRepository) UserProfile(_ context.Context, _ uint) (*db.User, error) {
	return nil, nil
}

func (m *MockUserRepository) UpdateUserActivity(_ context.Context, _ *db.User, _ *db.PublicKey) error {
	return nil
}

func (m *MockUserRepository) UserMatchHistory(_ context.Context, _ uint, _ int) ([]db.MatchParticipant, error) {
	return nil, nil
}

func TestAuthenticateSession_NoPublicKey(t *testing.T) {
	t.Parallel()
	m := new(MockSession)
	m.On("PublicKey").Return(nil)

	_, err := ssh.AuthenticateSession(m)
	assert.ErrorIs(t, err, ssh.ErrNoPublicKey)
}

func TestLoadOrRegisterUser_LoadError(t *testing.T) {
	t.Parallel()
	repo := new(MockUserRepository)
	repo.On("LoadUserByFingerprint", mock.Anything, "fp").Return(nil, nil, errors.New("db error"))

	_, err := ssh.LoadOrRegisterUser(context.Background(), repo, "user", "fp", nil)
	assert.ErrorIs(t, err, ssh.ErrInternal)
}

func TestLoadOrRegisterUser_RegisterError(t *testing.T) {
	t.Parallel()
	repo := new(MockUserRepository)
	repo.On("LoadUserByFingerprint", mock.Anything, "fp").Return(nil, nil, nil)
	repo.On("RegisterUserWithKey", mock.Anything, "user", "fp").Return(nil, nil, errors.New("reg error"))

	_, err := ssh.LoadOrRegisterUser(context.Background(), repo, "user", "fp", nil)
	assert.ErrorIs(t, err, ssh.ErrRegistrationFailed)
}

func TestLoadOrRegisterUser_PassesThroughActionableSentinels(t *testing.T) {
	t.Parallel()

	sentinels := []error{
		db.ErrUsernameTaken,
		db.ErrInvalidUsername,
		db.ErrKeyAlreadyRegistered,
	}

	for _, sentinel := range sentinels {
		t.Run(sentinel.Error(), func(t *testing.T) {
			t.Parallel()
			repo := new(MockUserRepository)
			repo.On("LoadUserByFingerprint", mock.Anything, "fp").Return(nil, nil, nil)
			repo.On("RegisterUserWithKey", mock.Anything, "user", "fp").Return(nil, nil, sentinel)

			_, err := ssh.LoadOrRegisterUser(context.Background(), repo, "user", "fp", nil)
			require.ErrorIs(t, err, sentinel)
			assert.NotErrorIs(t, err, ssh.ErrRegistrationFailed)
		})
	}
}

func TestLoadOrRegisterUser_PreservesWrappedCause(t *testing.T) {
	t.Parallel()
	repo := new(MockUserRepository)
	cause := fmt.Errorf("%w: must be 3-16 characters", db.ErrInvalidUsername)
	repo.On("LoadUserByFingerprint", mock.Anything, "fp").Return(nil, nil, nil)
	repo.On("RegisterUserWithKey", mock.Anything, "bad", "fp").Return(nil, nil, cause)

	_, err := ssh.LoadOrRegisterUser(context.Background(), repo, "bad", "fp", nil)
	require.ErrorIs(t, err, db.ErrInvalidUsername)
	assert.ErrorContains(t, err, "must be 3-16 characters")
}

// Registration is the one unauthenticated write a stranger can drive in a loop, so
// its gate has to actually refuse - and has to leave returning players alone, since
// a shared NAT would otherwise lock out a whole office once five accounts existed.
func TestLoadOrRegisterUser_RegistrationGate(t *testing.T) {
	t.Parallel()

	t.Run("a refused gate registers nobody", func(t *testing.T) {
		t.Parallel()
		repo := new(MockUserRepository)
		repo.On("LoadUserByFingerprint", mock.Anything, "fp").Return(nil, nil, nil)

		_, err := ssh.LoadOrRegisterUser(context.Background(), repo, "user", "fp",
			func() bool { return false })

		assert.ErrorIs(t, err, ssh.ErrTooManyRegistrations)
		repo.AssertNotCalled(t, "RegisterUserWithKey", mock.Anything, mock.Anything, mock.Anything)
	})

	t.Run("a returning player never spends the budget", func(t *testing.T) {
		t.Parallel()
		existing := &db.User{ID: 1, Username: "known"}
		repo := new(MockUserRepository)
		repo.On("LoadUserByFingerprint", mock.Anything, "fp").Return(existing, nil, nil)
		repo.On("UpdateUserActivity", mock.Anything, mock.Anything, mock.Anything).Return(nil)

		asked := 0
		user, err := ssh.LoadOrRegisterUser(context.Background(), repo, "known", "fp",
			func() bool { asked++; return false })

		require.NoError(t, err, "an existing account is not a registration")
		assert.Equal(t, existing, user)
		assert.Zero(t, asked, "the gate is not even consulted")
	})

	t.Run("an allowed gate registers", func(t *testing.T) {
		t.Parallel()
		fresh := &db.User{ID: 2, Username: "new"}
		repo := new(MockUserRepository)
		repo.On("LoadUserByFingerprint", mock.Anything, "fp").Return(nil, nil, nil)
		repo.On("RegisterUserWithKey", mock.Anything, "new", "fp").Return(fresh, &db.PublicKey{}, nil)

		user, err := ssh.LoadOrRegisterUser(context.Background(), repo, "new", "fp",
			func() bool { return true })

		require.NoError(t, err)
		assert.Equal(t, fresh, user)
	})
}
