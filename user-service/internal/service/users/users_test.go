package users

import (
	"context"
	"errors"
	"testing"

	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/bcrypt"
)

const (
	validUsername = "john_doe"
	validEmail    = "john.doe@example.com"
	validPassword = "Correct-Horse-42"
)

type mockRepo struct {
	mock.Mock
}

func (m *mockRepo) GetByUsername(ctx context.Context, username string) (User, error) {
	args := m.Called(ctx, username)
	return args.Get(0).(User), args.Error(1)
}

func (m *mockRepo) Create(ctx context.Context, u User) (User, error) {
	args := m.Called(ctx, u)
	return args.Get(0).(User), args.Error(1)
}

func hashOf(t *testing.T, password string) string {
	t.Helper()
	h, err := bcrypt.GenerateFromPassword([]byte(password), bcrypt.MinCost)
	require.NoError(t, err)
	return string(h)
}

func TestService_GetByUsername(t *testing.T) {
	t.Run("returns the repository's user", func(t *testing.T) {
		want := User{ID: 7, Username: validUsername}
		repo := &mockRepo{}
		repo.On("GetByUsername", mock.Anything, validUsername).Return(want, nil)

		got, err := New(repo).GetByUsername(context.Background(), validUsername)

		require.NoError(t, err)
		require.Equal(t, want, got)
		repo.AssertExpectations(t)
	})

	t.Run("propagates the repository's error", func(t *testing.T) {
		repo := &mockRepo{}
		repo.On("GetByUsername", mock.Anything, validUsername).Return(User{}, ErrNotFound)

		_, err := New(repo).GetByUsername(context.Background(), validUsername)

		require.ErrorIs(t, err, ErrNotFound)
		repo.AssertExpectations(t)
	})
}

func TestService_Create(t *testing.T) {
	t.Run("hashes the password and stores the user", func(t *testing.T) {
		repo := &mockRepo{}
		repo.On("Create", mock.Anything, mock.MatchedBy(func(u User) bool {
			return u.Username == validUsername &&
				u.Email == validEmail &&
				u.PasswordHash != validPassword &&
				bcrypt.CompareHashAndPassword([]byte(u.PasswordHash), []byte(validPassword)) == nil
		})).Return(User{ID: 1, Username: validUsername, Email: validEmail}, nil)

		got, err := New(repo).Create(context.Background(), validUsername, validEmail, validPassword)

		require.NoError(t, err)
		require.Equal(t, int64(1), got.ID)
		repo.AssertExpectations(t)
	})

	t.Run("rejects reserved usernames without touching the repository", func(t *testing.T) {
		for name := range reservedUsernames {
			t.Run(name, func(t *testing.T) {
				repo := &mockRepo{}

				_, err := New(repo).Create(context.Background(), name, validEmail, validPassword)

				require.ErrorIs(t, err, ErrReservedUsername)
				repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
			})
		}
	})

	t.Run("rejects weak passwords without touching the repository", func(t *testing.T) {
		tests := map[string]string{
			"no uppercase": "correct-horse-42",
			"no lowercase": "CORRECT-HORSE-42",
			"no digit":     "Correct-Horse-Battery",
			"no special":   "CorrectHorse42",
		}
		for name, password := range tests {
			t.Run(name, func(t *testing.T) {
				repo := &mockRepo{}

				_, err := New(repo).Create(context.Background(), validUsername, validEmail, password)

				require.ErrorIs(t, err, ErrWeakPassword)
				repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
			})
		}
	})

	t.Run("propagates repository errors", func(t *testing.T) {
		for _, repoErr := range []error{ErrUsernameTaken, ErrEmailTaken, errors.New("db down")} {
			repo := &mockRepo{}
			repo.On("Create", mock.Anything, mock.Anything).Return(User{}, repoErr)

			_, err := New(repo).Create(context.Background(), validUsername, validEmail, validPassword)

			require.ErrorIs(t, err, repoErr)
			repo.AssertExpectations(t)
		}
	})
}

func TestService_Authenticate(t *testing.T) {
	stored := User{ID: 3, Username: validUsername, PasswordHash: hashOf(t, validPassword)}

	tests := []struct {
		name     string
		repoUser User
		repoErr  error
		password string
		wantErr  error
	}{
		{name: "correct password", repoUser: stored, password: validPassword},
		{name: "wrong password", repoUser: stored, password: "Wrong-Password-1", wantErr: ErrInvalidCredentials},
		{
			name:     "unknown user is indistinguishable from a wrong password",
			repoErr:  ErrNotFound,
			password: validPassword,
			wantErr:  ErrInvalidCredentials,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			repo := &mockRepo{}
			repo.On("GetByUsername", mock.Anything, validUsername).Return(tt.repoUser, tt.repoErr)

			got, err := New(repo).Authenticate(context.Background(), validUsername, tt.password)

			repo.AssertExpectations(t)
			if tt.wantErr != nil {
				require.ErrorIs(t, err, tt.wantErr)
				require.Zero(t, got)
				return
			}
			require.NoError(t, err)
			require.Equal(t, stored, got)
		})
	}

	t.Run("propagates unexpected repository errors", func(t *testing.T) {
		repoErr := errors.New("db down")
		repo := &mockRepo{}
		repo.On("GetByUsername", mock.Anything, validUsername).Return(User{}, repoErr)

		_, err := New(repo).Authenticate(context.Background(), validUsername, validPassword)

		require.ErrorIs(t, err, repoErr)
		require.NotErrorIs(t, err, ErrInvalidCredentials)
		repo.AssertExpectations(t)
	})
}
