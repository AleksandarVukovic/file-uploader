package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

var testSecret = []byte("test-secret")

func signToken(t *testing.T, secret []byte, method jwt.SigningMethod, ttl time.Duration, subject string) string {
	t.Helper()

	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   subject,
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(ttl)),
	}
	signed, err := jwt.NewWithClaims(method, claims).SignedString(secret)
	require.NoError(t, err)
	return signed
}

func newRequestWithAuth(authHeader string) *http.Request {
	req := httptest.NewRequest(http.MethodGet, "/protected", nil)
	if authHeader != "" {
		req.Header.Set("Authorization", authHeader)
	}
	return req
}

func TestJWT_RejectsMissingHeader(t *testing.T) {
	t.Parallel()

	called := false
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })

	rec := httptest.NewRecorder()
	JWT(testSecret)(next).ServeHTTP(rec, newRequestWithAuth(""))

	require.Equal(t, http.StatusUnauthorized, rec.Code)
	require.False(t, called, "next handler must not be called when the header is missing")
}

func TestJWT_RejectsMalformedHeader(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name   string
		header string
	}{
		{"wrong scheme", "Basic dXNlcjpwYXNz"},
		{"no scheme separator", "Bearer"},
		{"empty token", "Bearer "},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })

			rec := httptest.NewRecorder()
			JWT(testSecret)(next).ServeHTTP(rec, newRequestWithAuth(tt.header))

			require.Equal(t, http.StatusUnauthorized, rec.Code)
			require.False(t, called, "next handler must not be called for a malformed header")
		})
	}
}

func TestJWT_RejectsInvalidToken(t *testing.T) {
	t.Parallel()

	wrongSecret := []byte("not-the-right-secret")

	tests := []struct {
		name  string
		token string
	}{
		{"wrong signing secret", signToken(t, wrongSecret, jwt.SigningMethodHS256, time.Minute, "admin123")},
		{"expired token", signToken(t, testSecret, jwt.SigningMethodHS256, -time.Minute, "admin123")},
		{"unexpected signing method", signToken(t, testSecret, jwt.SigningMethodHS384, time.Minute, "admin123")},
		{"garbage token", "not-a-jwt-at-all"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			called := false
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { called = true })

			rec := httptest.NewRecorder()
			JWT(testSecret)(next).ServeHTTP(rec, newRequestWithAuth("Bearer "+tt.token))

			require.Equal(t, http.StatusUnauthorized, rec.Code)
			require.False(t, called, "next handler must not be called for an invalid token")
		})
	}
}

func TestJWT_AcceptsValidToken_AndPropagatesUsername(t *testing.T) {
	t.Parallel()

	var gotUsername string
	var gotOK bool
	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotUsername, gotOK = UsernameFromCtx(r.Context())
		w.WriteHeader(http.StatusOK)
	})

	token := signToken(t, testSecret, jwt.SigningMethodHS256, time.Minute, "admin123")

	rec := httptest.NewRecorder()
	JWT(testSecret)(next).ServeHTTP(rec, newRequestWithAuth("Bearer "+token))

	require.Equal(t, http.StatusOK, rec.Code)
	require.True(t, gotOK)
	require.Equal(t, "admin123", gotUsername)
}

func TestUsernameFromCtx_AbsentWhenNotSet(t *testing.T) {
	t.Parallel()

	username, ok := UsernameFromCtx(t.Context())
	require.False(t, ok)
	require.Empty(t, username)
}
