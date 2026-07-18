package ssh_test

import (
	"context"
	"errors"
	"testing"

	"github.com/Pieczasz/terminal-card/internal/db"
	"github.com/Pieczasz/terminal-card/internal/repository"
	"github.com/Pieczasz/terminal-card/internal/ssh"

	charmssh "github.com/charmbracelet/ssh"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/mock"
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

func (m *MockUserRepository) GetBestPlayers(ctx context.Context, limit int) ([]db.Ranking, error) {
	return nil, nil
}
func (m *MockUserRepository) GetUserProfile(ctx context.Context, userID uint) (*db.User, error) {
	return nil, nil
}
func (m *MockUserRepository) UpdateUserActivity(ctx context.Context, user *db.User, key *db.PublicKey) {
}
func (m *MockUserRepository) GetUserMatchHistory(ctx context.Context, userID uint, limit int) ([]db.MatchParticipant, error) {
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

	_, err := ssh.LoadOrRegisterUser(context.Background(), repo, "user", "fp")
	assert.ErrorIs(t, err, ssh.ErrInternal)
}

func TestLoadOrRegisterUser_RegisterError(t *testing.T) {
	t.Parallel()
	repo := new(MockUserRepository)
	repo.On("LoadUserByFingerprint", mock.Anything, "fp").Return(nil, nil, nil)
	repo.On("RegisterUserWithKey", mock.Anything, "user", "fp").Return(nil, nil, errors.New("reg error"))

	_, err := ssh.LoadOrRegisterUser(context.Background(), repo, "user", "fp")
	assert.ErrorIs(t, err, ssh.ErrRegistrationFail)
}

func TestLoadOrRegisterUser_MapsSentinels(t *testing.T) {
	t.Parallel()

	cases := []struct {
		name string
		err  error
		want error
	}{
		{name: "username taken", err: repository.ErrUsernameTaken, want: ssh.ErrUsernameTaken},
		{name: "invalid username", err: repository.ErrInvalidUsername, want: ssh.ErrInvalidUsername},
		{name: "key used", err: repository.ErrKeyAlreadyRegistered, want: ssh.ErrKeyAlreadyUsed},
	}

	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()
			repo := new(MockUserRepository)
			repo.On("LoadUserByFingerprint", mock.Anything, "fp").Return(nil, nil, nil)
			repo.On("RegisterUserWithKey", mock.Anything, "user", "fp").Return(nil, nil, tc.err)

			_, err := ssh.LoadOrRegisterUser(context.Background(), repo, "user", "fp")
			assert.ErrorIs(t, err, tc.want)
		})
	}
}
