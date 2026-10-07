//go:build integration

package api

import (
	"encoding/json"
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

var loginPath = authsvr.LoginAuthPath()

func newAuthServer(t *testing.T, users *mockAuthenticator) *httptest.Server {
	t.Helper()

	handler := Routes(logger.NewLogger(false), testJWTSecret, panicFilesService{}, NewHealthSvc(), NewAuthSvc(testJWTSecret, users))
	return httptest.NewServer(handler)
}

func authenticatingAs(username, password string, user userservice.User, err error) *mockAuthenticator {
	users := &mockAuthenticator{}
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

	users := &mockAuthenticator{}
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
