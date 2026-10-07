package api

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/aleksandarv/file-uploader/user-service/gen/users"
	userssvc "github.com/aleksandarv/file-uploader/user-service/internal/service/users"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
	"golang.org/x/crypto/bcrypt"
)

const (
	testUsername = "john_doe"
	testEmail    = "john.doe@example.com"
	testPassword = "Correct-Horse-42"
)

var (
	testCreatedAt = time.Date(2023, 1, 1, 12, 0, 0, 0, time.UTC)
	testUpdatedAt = time.Date(2023, 1, 2, 12, 0, 0, 0, time.FixedZone("CEST", 2*60*60))
)

type mockRepo struct {
	mock.Mock
}

func (m *mockRepo) GetByUsername(ctx context.Context, username string) (userssvc.User, error) {
	args := m.Called(ctx, username)
	return args.Get(0).(userssvc.User), args.Error(1)
}

func (m *mockRepo) Create(ctx context.Context, u userssvc.User) (userssvc.User, error) {
	args := m.Called(ctx, u)
	return args.Get(0).(userssvc.User), args.Error(1)
}

func testCtx() context.Context {
	return logger.WithCtx(context.Background(), slog.New(slog.NewTextHandler(io.Discard, nil)))
}

func storedUser(t *testing.T) userssvc.User {
	t.Helper()
	hash, err := bcrypt.GenerateFromPassword([]byte(testPassword), bcrypt.MinCost)
	require.NoError(t, err)
	return userssvc.User{
		ID:           9,
		Username:     testUsername,
		Email:        testEmail,
		PasswordHash: string(hash),
		CreatedAt:    testCreatedAt,
		UpdatedAt:    testUpdatedAt,
	}
}

func newHandler(repo *mockRepo) users.Service {
	return NewUsersHandler(userssvc.New(repo))
}

func requireServiceError(t *testing.T, err error, wantName string) {
	t.Helper()
	var svcErr *goa.ServiceError
	require.True(t, errors.As(err, &svcErr), "expected *goa.ServiceError, got %T: %v", err, err)
	require.Equal(t, wantName, svcErr.Name)
}

func returning(u userssvc.User, err error) *mockRepo {
	repo := &mockRepo{}
	repo.On("GetByUsername", mock.Anything, mock.Anything).Return(u, err).Maybe()
	repo.On("Create", mock.Anything, mock.Anything).Return(u, err).Maybe()
	return repo
}

func TestUsersHandler_GetByUsername(t *testing.T) {
	tests := []struct {
		name    string
		repo    *mockRepo
		wantErr string
	}{
		{name: "found", repo: returning(storedUser(t), nil)},
		{name: "not found", repo: returning(userssvc.User{}, userssvc.ErrNotFound), wantErr: "not_found"},
		{name: "repository failure", repo: returning(userssvc.User{}, errors.New("db down")), wantErr: "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newHandler(tt.repo).GetByUsername(testCtx(), &users.GetByUsernamePayload{Username: testUsername})

			if tt.wantErr != "" {
				require.Nil(t, got)
				requireServiceError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, &users.User{
				ID:        9,
				Username:  testUsername,
				Email:     testEmail,
				CreatedAt: "2023-01-01T12:00:00Z",
				UpdatedAt: "2023-01-02T10:00:00Z",
			}, got)
		})
	}
}

func TestUsersHandler_Create(t *testing.T) {
	created := userssvc.User{ID: 1, Username: testUsername, Email: testEmail, CreatedAt: testCreatedAt, UpdatedAt: testCreatedAt}

	tests := []struct {
		name      string
		repo      *mockRepo
		username  string
		password  string
		wantErr   string
		untouched bool
	}{
		{name: "created", repo: returning(created, nil), username: testUsername, password: testPassword},
		{name: "reserved username", repo: returning(created, nil), username: "admin", password: testPassword, wantErr: "bad_request", untouched: true},
		{name: "weak password", repo: returning(created, nil), username: testUsername, password: "alllowercase1!", wantErr: "bad_request", untouched: true},
		{name: "username taken", repo: returning(userssvc.User{}, userssvc.ErrUsernameTaken), username: testUsername, password: testPassword, wantErr: "already_exists"},
		{name: "email taken", repo: returning(userssvc.User{}, userssvc.ErrEmailTaken), username: testUsername, password: testPassword, wantErr: "already_exists"},
		{name: "repository failure", repo: returning(userssvc.User{}, errors.New("db down")), username: testUsername, password: testPassword, wantErr: "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newHandler(tt.repo).Create(testCtx(), &users.CreatePayload{
				Username: tt.username,
				Email:    testEmail,
				Password: tt.password,
			})

			if tt.untouched {
				tt.repo.AssertNotCalled(t, "Create", mock.Anything, mock.Anything)
			}
			if tt.wantErr != "" {
				require.Nil(t, got)
				requireServiceError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, testUsername, got.Username)
			require.Equal(t, testEmail, got.Email)
			require.Equal(t, "2023-01-01T12:00:00Z", got.CreatedAt)
		})
	}
}

func TestUsersHandler_Authenticate(t *testing.T) {
	tests := []struct {
		name     string
		repo     *mockRepo
		password string
		wantErr  string
	}{
		{name: "valid credentials", repo: returning(storedUser(t), nil), password: testPassword},
		{name: "wrong password", repo: returning(storedUser(t), nil), password: "Wrong-Password-1", wantErr: "unauthorized"},
		{name: "unknown user", repo: returning(userssvc.User{}, userssvc.ErrNotFound), password: testPassword, wantErr: "unauthorized"},
		{name: "repository failure", repo: returning(userssvc.User{}, errors.New("db down")), password: testPassword, wantErr: "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := newHandler(tt.repo).Authenticate(testCtx(), &users.AuthenticatePayload{
				Username: testUsername,
				Password: tt.password,
			})

			if tt.wantErr != "" {
				require.Nil(t, got)
				requireServiceError(t, err, tt.wantErr)
				return
			}
			require.NoError(t, err)
			require.Equal(t, int64(9), got.ID)
			require.Equal(t, testUsername, got.Username)
		})
	}
}

func TestHealthHandler_Health(t *testing.T) {
	res, err := NewHealthHandler().Health(context.Background())

	require.NoError(t, err)
	require.Equal(t, "ok", res.Status)
}
