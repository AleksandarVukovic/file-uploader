package api

import (
	"bytes"
	"context"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/aleksandarv/file-uploader/api-service/gen/files"
	"github.com/aleksandarv/file-uploader/api-service/gen/health"
	filessvr "github.com/aleksandarv/file-uploader/api-service/gen/http/files/server"
	healthsvr "github.com/aleksandarv/file-uploader/api-service/gen/http/health/server"
	"github.com/golang-jwt/jwt/v5"
	"github.com/stretchr/testify/require"
)

var (
	filesUploadPath = filessvr.UploadFilesPath()
	healthPath      = healthsvr.HealthHealthPath()
)

var testJWTSecret = []byte("ut-test-secret")

func validToken(t *testing.T, secret []byte) string {
	t.Helper()

	now := time.Now()
	claims := jwt.RegisteredClaims{
		Subject:   "test-user",
		IssuedAt:  jwt.NewNumericDate(now),
		ExpiresAt: jwt.NewNumericDate(now.Add(time.Minute)),
	}
	signed, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(secret)
	require.NoError(t, err)
	return signed
}

type panicFilesHandler struct{}

func (panicFilesHandler) Upload(context.Context, *files.UploadPayload, io.ReadCloser) (*files.UploadResult, error) {
	panic("boom: upload handler panicked")
}

type panicHealthHandler struct{}

func (panicHealthHandler) Health(context.Context) (*health.HealthResult, error) {
	panic("boom: health handler panicked")
}

func newBufferLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, nil)), &buf
}

func newMinimalUploadRequest(t *testing.T, baseURL string, extraHeaders map[string]string) *http.Request {
	t.Helper()

	body := strings.Repeat("x", 100)
	req, err := http.NewRequest(http.MethodPost, baseURL+filesUploadPath, strings.NewReader(body))
	require.NoError(t, err)

	req.Header.Set("Content-Disposition", "dummy.csv")
	req.Header.Set("Content-Length", strconv.Itoa(len(body)))
	req.Header.Set("Content-Type", "text/csv")
	req.Header.Set("X-Checksum-Sha256", strings.Repeat("a", 64))
	req.Header.Set("Authorization", "Bearer "+validToken(t, testJWTSecret))
	for k, v := range extraHeaders {
		req.Header.Set(k, v)
	}
	return req
}

func lastLoggedReqID(t *testing.T, logs *bytes.Buffer) string {
	t.Helper()

	lines := strings.Split(strings.TrimSpace(logs.String()), "\n")
	require.NotEmpty(t, lines)

	var entry struct {
		ReqID string `json:"reqID"`
	}
	require.NoError(t, json.Unmarshal([]byte(lines[len(lines)-1]), &entry))
	return entry.ReqID
}

func TestRoutes_HealthPath_Mounted(t *testing.T) {
	t.Parallel()

	log, _ := newBufferLogger()
	srv := httptest.NewServer(Routes(log, testJWTSecret, panicFilesHandler{}, NewHealthHandler(), NewAuthHandler(testJWTSecret, &mockUserService{})))
	defer srv.Close()

	resp, err := http.Get(srv.URL + healthPath)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestRoutes_PanicRecovery_FilesEndpoint(t *testing.T) {
	t.Parallel()

	log, logs := newBufferLogger()
	srv := httptest.NewServer(Routes(log, testJWTSecret, panicFilesHandler{}, NewHealthHandler(), NewAuthHandler(testJWTSecret, &mockUserService{})))
	defer srv.Close()

	req := newMinimalUploadRequest(t, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	require.Contains(t, logs.String(), "panic recovered")
	require.NotEmpty(t, lastLoggedReqID(t, logs))
}

func TestRoutes_PanicRecovery_HealthEndpoint(t *testing.T) {
	t.Parallel()

	log, _ := newBufferLogger()
	srv := httptest.NewServer(Routes(log, testJWTSecret, panicFilesHandler{}, panicHealthHandler{}, NewAuthHandler(testJWTSecret, &mockUserService{})))
	defer srv.Close()

	resp, err := http.Get(srv.URL + healthPath)
	if err != nil {
		t.Fatalf("health endpoint panic was not recovered into a response: %v", err)
	}
	defer resp.Body.Close()

	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestRoutes_RequestID_HonorsIncomingHeader(t *testing.T) {
	t.Parallel()

	log, logs := newBufferLogger()
	srv := httptest.NewServer(Routes(log, testJWTSecret, panicFilesHandler{}, NewHealthHandler(), NewAuthHandler(testJWTSecret, &mockUserService{})))
	defer srv.Close()

	req := newMinimalUploadRequest(t, srv.URL, map[string]string{"X-Request-Id": "custom-request-id-123"})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, "custom-request-id-123", lastLoggedReqID(t, logs))
}

func TestRoutes_RequestID_TruncatesLongIncomingHeader(t *testing.T) {
	t.Parallel()

	log, logs := newBufferLogger()
	srv := httptest.NewServer(Routes(log, testJWTSecret, panicFilesHandler{}, NewHealthHandler(), NewAuthHandler(testJWTSecret, &mockUserService{})))
	defer srv.Close()

	longID := strings.Repeat("a", 100)
	req := newMinimalUploadRequest(t, srv.URL, map[string]string{"X-Request-Id": longID})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, longID[:64], lastLoggedReqID(t, logs))
}

func TestRoutes_RequestID_GeneratesUniqueIDsWhenHeaderAbsent(t *testing.T) {
	t.Parallel()

	log, logs := newBufferLogger()
	srv := httptest.NewServer(Routes(log, testJWTSecret, panicFilesHandler{}, NewHealthHandler(), NewAuthHandler(testJWTSecret, &mockUserService{})))
	defer srv.Close()

	resp1, err := http.DefaultClient.Do(newMinimalUploadRequest(t, srv.URL, nil))
	require.NoError(t, err)
	resp1.Body.Close()
	id1 := lastLoggedReqID(t, logs)
	require.NotEmpty(t, id1)

	resp2, err := http.DefaultClient.Do(newMinimalUploadRequest(t, srv.URL, nil))
	require.NoError(t, err)
	resp2.Body.Close()
	id2 := lastLoggedReqID(t, logs)
	require.NotEmpty(t, id2)

	require.NotEqual(t, id1, id2)
}
