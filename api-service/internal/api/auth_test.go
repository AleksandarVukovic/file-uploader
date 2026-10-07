package api

import (
	"context"
	"errors"
	"testing"

	"github.com/aleksandarv/file-uploader/api-service/gen/auth"
	"github.com/aleksandarv/file-uploader/api-service/internal/userservice"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
)

const (
	testUsername = "john_doe"
	testPassword = "Correct-Horse-42"
)

type mockAuthenticator struct {
	mock.Mock
}

func (m *mockAuthenticator) Authenticate(ctx context.Context, username, password string) (userservice.User, error) {
	args := m.Called(ctx, username, password)
	return args.Get(0).(userservice.User), args.Error(1)
}

func TestAuthService_Login_Success(t *testing.T) {
	secret := []byte("test-secret")
	users := &mockAuthenticator{}
	users.On("Authenticate", mock.Anything, testUsername, testPassword).
		Return(userservice.User{ID: 1, Username: testUsername}, nil)
	svc := NewAuthSvc(secret, users)

	res, err := svc.Login(testCtx(), &auth.LoginPayload{
		Username: testUsername,
		Password: testPassword,
	})

	require.NoError(t, err)
	require.NotEmpty(t, res.Token)

	claims := &jwt.RegisteredClaims{}
	_, err = jwt.ParseWithClaims(res.Token, claims, func(tok *jwt.Token) (any, error) {
		return secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}))
	require.NoError(t, err)
	require.Equal(t, testUsername, claims.Subject)
	require.WithinDuration(t, claims.IssuedAt.Add(tokenTTL), claims.ExpiresAt.Time, 0)
	users.AssertExpectations(t)
}

func TestAuthService_Login_InvalidCredentials(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
	}{
		{"wrong username", "someone-else", testPassword},
		{"wrong password", testUsername, "wrong-password"},
		{"both wrong", "someone-else", "wrong-password"},
		{"empty credentials", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users := &mockAuthenticator{}
			users.On("Authenticate", mock.Anything, tt.username, tt.password).
				Return(userservice.User{}, userservice.ErrInvalidCredentials)
			svc := NewAuthSvc([]byte("test-secret"), users)

			res, err := svc.Login(testCtx(), &auth.LoginPayload{
				Username: tt.username,
				Password: tt.password,
			})

			require.Nil(t, res)
			require.Error(t, err)

			var svcErr *goa.ServiceError
			require.True(t, errors.As(err, &svcErr))
			require.Equal(t, "invalid_credentials", svcErr.Name)
			users.AssertExpectations(t)
		})
	}
}

func TestAuthService_Login_UserServiceFailures(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantName string
	}{
		{"unavailable", errors.Join(userservice.ErrUnavailable, errors.New("connection refused")), "unavailable"},
		{"unexpected error", errors.New("boom"), "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			users := &mockAuthenticator{}
			users.On("Authenticate", mock.Anything, testUsername, testPassword).
				Return(userservice.User{}, tt.err)
			svc := NewAuthSvc([]byte("test-secret"), users)

			res, err := svc.Login(testCtx(), &auth.LoginPayload{Username: testUsername, Password: testPassword})

			require.Nil(t, res)
			var svcErr *goa.ServiceError
			require.True(t, errors.As(err, &svcErr))
			require.Equal(t, tt.wantName, svcErr.Name)
			users.AssertExpectations(t)
		})
	}
}
