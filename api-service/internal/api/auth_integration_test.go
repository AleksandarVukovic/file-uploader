//go:build integration

package api

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	authsvr "github.com/aleksandarv/file-uploader/api-service/gen/http/auth/server"
	"github.com/aleksandarv/file-uploader/common/logger"
	"github.com/stretchr/testify/require"
)

var loginPath = authsvr.LoginAuthPath()

func newAuthServer(t *testing.T) *httptest.Server {
	t.Helper()

	handler := Routes(logger.NewLogger(false), testJWTSecret, panicFilesService{}, NewHealthSvc(), NewAuthSvc(testJWTSecret))
	return httptest.NewServer(handler)
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

	srv := newAuthServer(t)
	defer srv.Close()

	req := newLoginRequest(t, srv.URL, hardcodedUsername, hardcodedPassword)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusOK, resp.StatusCode)

	var body struct {
		Token string `json:"token"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.NotEmpty(t, body.Token)
}

func TestLoginEndpoint_InvalidCredentials(t *testing.T) {
	t.Parallel()

	tests := []struct {
		name     string
		username string
		password string
	}{
		{"wrong username", "someone-else", hardcodedPassword},
		{"wrong password", hardcodedUsername, "wrong-password1"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			t.Parallel()

			srv := newAuthServer(t)
			defer srv.Close()

			req := newLoginRequest(t, srv.URL, tt.username, tt.password)
			resp, err := http.DefaultClient.Do(req)
			require.NoError(t, err)
			defer resp.Body.Close()

			require.Equal(t, http.StatusUnauthorized, resp.StatusCode)
		})
	}
}

func TestLoginEndpoint_RejectsPayloadFailingDesignValidation(t *testing.T) {
	t.Parallel()

	srv := newAuthServer(t)
	defer srv.Close()

	// password shorter than the design's MinLength(8) constraint
	req := newLoginRequest(t, srv.URL, hardcodedUsername, "short")
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
}

func TestLoginEndpoint_IssuedTokenAuthorizesProtectedFilesEndpoint(t *testing.T) {
	t.Parallel()

	srv := newAuthServer(t)
	defer srv.Close()

	loginResp, err := http.DefaultClient.Do(newLoginRequest(t, srv.URL, hardcodedUsername, hardcodedPassword))
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

	// panicFilesService panics on Upload; reaching the panic (500, recovered
	// by PanicHandler) rather than a 401 proves the issued token cleared the
	// JWT middleware.
	require.Equal(t, http.StatusInternalServerError, uploadResp.StatusCode)
}
