//go:build integration

package api

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authsvr "github.com/aleksandarv/file-uploader/api-service/gen/http/auth/server"
	"github.com/aleksandarv/file-uploader/api-service/internal/userservice"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/stretchr/testify/mock"
	"github.com/stretchr/testify/require"
)

var (
	loginPath    = authsvr.LoginAuthPath()
	registerPath = authsvr.RegisterAuthPath()
)

func newAuthServer(t *testing.T, users *mockUserService) *httptest.Server {
	t.Helper()

	handler := Routes(logger.NewLogger(false), testJWTSecret, panicFilesHandler{}, NewHealthHandler(), NewAuthHandler(testJWTSecret, users))
	return httptest.NewServer(handler)
}

func authenticatingAs(username, password string, user userservice.User, err error) *mockUserService {
	users := &mockUserService{}
	users.On("Authenticate", mock.Anything, username, password).Return(user, err)
	return users
}

func newLoginRequest(t *testing.T, baseURL, username, password string) *http.Request {
	t.Helper()

	body, err := json.Marshal(map[string]string{
		"username": username,
		"password": password,
	})
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, baseURL+loginPath, strings.NewReader(string(body)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func TestLoginEndpoint_Success(t *testing.T) {
	t.Parallel()

	users := authenticatingAs(testUsername, testPassword, userservice.User{ID: 1, Username: testUsername}, nil)
	srv := newAuthServer(t, users)
	defer srv.Close()

	req := newLoginRequest(t, srv.URL, testUsername, testPassword)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.NotEmpty(t, body.Token)
	users.AssertExpectations(t)
}

func TestLoginEndpoint_InvalidCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		username string
		password string
	}{
		{"wrong username", "someone-else", testPassword},
		{"wrong password", testUsername, "wrong-password1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			users := authenticatingAs(tt.username, tt.password, userservice.User{}, userservice.ErrInvalidCredentials)
			srv := newAuthServer(t, users)
			defer srv.Close()

			req := newLoginRequest(t, srv.URL, tt.username, tt.password)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
			users.AssertExpectations(t)
		})
	}
}

func TestLoginEndpoint_RejectsPayloadFailingDesignValidation(t *testing.T) {
	t.Parallel()

	users := &mockUserService{}
	srv := newAuthServer(t, users)
	defer srv.Close()

	req := newLoginRequest(t, srv.URL, testUsername, "short")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	users.AssertNotCalled(t, "Authenticate", mock.Anything, mock.Anything, mock.Anything)
}

func TestLoginEndpoint_IssuedTokenAuthorizesProtectedFilesEndpoint(t *testing.T) {
	t.Parallel()

	users := authenticatingAs(testUsername, testPassword, userservice.User{ID: 1, Username: testUsername}, nil)
	srv := newAuthServer(t, users)
	defer srv.Close()

	loginResp, err := http.DefaultClient.Do(newLoginRequest(t, srv.URL, testUsername, testPassword))
	require.NoError(t, err)
	defer loginResp.Body.Close()
	require.Equal(t, http.StatusOK, loginResp.StatusCode)

	var loginBody struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.NewDecoder(loginResp.Body).Decode(&loginBody))
	require.NotEmpty(t, loginBody.Token)

	uploadReq := newMinimalUploadRequest(t, srv.URL, map[string]string{
		"Authorization": "Bearer " + loginBody.Token,
	})
	uploadResp, err := http.DefaultClient.Do(uploadReq)
	require.NoError(t, err)
	defer uploadResp.Body.Close()

	require.Equal(t, http.StatusInternalServerError, uploadResp.StatusCode)
}

func newRegisterRequest(t *testing.T, baseURL string, payload map[string]string) *http.Request {
	t.Helper()

	body, err := json.Marshal(payload)
	require.NoError(t, err)

	req, err := http.NewRequest(http.MethodPost, baseURL+registerPath, strings.NewReader(string(body)))
	require.NoError(t, err)
	req.Header.Set("Content-Type", "application/json")
	return req
}

func validRegisterPayload() map[string]string {
	return map[string]string{"username": testUsername, "email": "john.doe@example.com", "password": testPassword}
}

func TestRegisterEndpoint_Success(t *testing.T) {
	t.Parallel()

	users := &mockUserService{}
	users.On("Create", mock.Anything, testUsername, "john.doe@example.com", testPassword).
		Return(userservice.User{ID: 5, Username: testUsername, Email: "john.doe@example.com"}, nil)
	srv := newAuthServer(t, users)
	defer srv.Close()

	resp, err := http.DefaultClient.Do(newRegisterRequest(t, srv.URL, validRegisterPayload()))
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusCreated, resp.StatusCode)
	var body struct {
		ID       int64  `json:"id"`
		Username string `json:"username"`
		Email    string `json:"email"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, int64(5), body.ID)
	require.Equal(t, testUsername, body.Username)
	require.Equal(t, "john.doe@example.com", body.Email)
	users.AssertExpectations(t)
}

func TestRegisterEndpoint_MapsUserServiceErrors(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name       string
		err        error
		wantStatus int
	}{
		{"rejected by user-service", fmt.Errorf("%w: password must contain a digit", userservice.ErrInvalidInput), http.StatusBadRequest},
		{"user exists", userservice.ErrUserExists, http.StatusConflict},
		{"user-service unavailable", userservice.ErrUnavailable, http.StatusServiceUnavailable},
		{"unexpected error", errors.New("boom"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			users := &mockUserService{}
			users.On("Create", mock.Anything, testUsername, "john.doe@example.com", testPassword).
				Return(userservice.User{}, tt.err)
			srv := newAuthServer(t, users)
			defer srv.Close()

			resp, err := http.DefaultClient.Do(newRegisterRequest(t, srv.URL, validRegisterPayload()))
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, tt.wantStatus, resp.StatusCode)
			users.AssertExpectations(t)
		})
	}
}

func TestRegisterEndpoint_RejectsPayloadFailingDesignValidation(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name  string
		patch map[string]string
	}{
		{"uppercase username", map[string]string{"username": "John"}},
		{"username starting with a digit", map[string]string{"username": "1john"}},
		{"short username", map[string]string{"username": "jo"}},
		{"invalid email", map[string]string{"email": "not-an-email"}},
		{"short password", map[string]string{"password": "Ab1!"}},
		{"password with a space", map[string]string{"password": "Correct Horse 42"}},
		{"password with non-ASCII characters", map[string]string{"password": "Correct-Horse-42é"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			payload := validRegisterPayload()
			for k, v := range tt.patch {
				payload[k] = v
			}
			users := &mockUserService{}
			srv := newAuthServer(t, users)
			defer srv.Close()

			resp, err := http.DefaultClient.Do(newRegisterRequest(t, srv.URL, payload))
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusBadRequest, resp.StatusCode)
			users.AssertNotCalled(t, "Create", mock.Anything, mock.Anything, mock.Anything, mock.Anything)
		})
	}
}
