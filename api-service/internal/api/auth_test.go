package api

import (
	"errors"
	"testing"

	"github.com/aleksandarv/file-uploader/api-service/gen/auth"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
	goa "goa.design/goa/v3/pkg"
)

func TestAuthService_Login_Success(t *testing.T) {
	secret := []byte("test-secret")
	svc := NewAuthSvc(secret)

	res, err := svc.Login(testCtx(), &auth.LoginPayload{
		Username: hardcodedUsername,
		Password: hardcodedPassword,
	})

	require.NoError(t, err)
	require.NotEmpty(t, res.Token)

	claims := &jwt.RegisteredClaims{}
	_, err = jwt.ParseWithClaims(res.Token, claims, func(tok *jwt.Token) (any, error) {
		return secret, nil
	}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}))
	require.NoError(t, err)
	require.Equal(t, hardcodedUsername, claims.Subject)
	require.WithinDuration(t, claims.IssuedAt.Add(tokenTTL), claims.ExpiresAt.Time, 0)
}

func TestAuthService_Login_InvalidCredentials(t *testing.T) {
	tests := []struct {
		name     string
		username string
		password string
	}{
		{"wrong username", "someone-else", hardcodedPassword},
		{"wrong password", hardcodedUsername, "wrong-password"},
		{"both wrong", "someone-else", "wrong-password"},
		{"empty credentials", "", ""},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			svc := NewAuthSvc([]byte("test-secret"))

			res, err := svc.Login(testCtx(), &auth.LoginPayload{
				Username: tt.username,
				Password: tt.password,
			})

			require.Nil(t, res)
			require.Error(t, err)

			var svcErr *goa.ServiceError
			require.True(t, errors.As(err, &svcErr))
			require.Equal(t, "invalid_credentials", svcErr.Name)
		})
	}
}
