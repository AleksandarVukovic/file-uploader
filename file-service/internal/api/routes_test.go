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

	"github.com/aleksandarv/file-uploader/file-service/gen/files"
	"github.com/aleksandarv/file-uploader/file-service/gen/health"
	filessvr "github.com/aleksandarv/file-uploader/file-service/gen/http/files/server"
	healthsvr "github.com/aleksandarv/file-uploader/file-service/gen/http/health/server"
	"github.com/stretchr/testify/require"
)

var (
	filesUploadPath = filessvr.UploadFilesPath()
	healthPath      = healthsvr.HealthHealthPath()
)

type panicFilesHandler struct{}

func (panicFilesHandler) Upload(context.Context, *files.UploadPayload, io.ReadCloser) (*files.UploadResult, error) {
	panic("boom: upload handler panicked")
}

type panicHealthHandler struct{}

func (panicHealthHandler) Health(context.Context) (*health.HealthResult, error) {
	panic("boom: health handler panicked")
}

type filesHandlerFunc func(context.Context, *files.UploadPayload, io.ReadCloser) (*files.UploadResult, error)

func (f filesHandlerFunc) Upload(ctx context.Context, p *files.UploadPayload, body io.ReadCloser) (*files.UploadResult, error) {
	return f(ctx, p, body)
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
	req.Header.Set("X-File-Size", strconv.Itoa(len(body)))
	req.Header.Set("Content-Type", "text/csv")
	req.Header.Set("X-Checksum-Sha256", strings.Repeat("a", 64))
	req.Header.Set("X-User-Id", "1")
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

func TestHealthRoutes_HealthPath_Mounted(t *testing.T) {
	t.Parallel()

	log, _ := newBufferLogger()
	srv := httptest.NewServer(HealthRoutes(log, NewHealthHandler()))
	defer srv.Close()

	resp, err := http.Get(srv.URL + healthPath)
	require.NoError(t, err)
	defer resp.Body.Close()
	require.Equal(t, http.StatusOK, resp.StatusCode)
}

func TestHealthRoutes_DoesNotMountFilesEndpoint(t *testing.T) {
	t.Parallel()

	log, _ := newBufferLogger()
	srv := httptest.NewServer(HealthRoutes(log, NewHealthHandler()))
	defer srv.Close()

	req := newMinimalUploadRequest(t, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNotFound, resp.StatusCode, "the health-only handler must not expose the files upload route")
}

func TestRoutes_DoesNotMountHealthEndpoint(t *testing.T) {
	t.Parallel()

	log, _ := newBufferLogger()
	srv := httptest.NewServer(Routes(log, panicFilesHandler{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + healthPath)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNotFound, resp.StatusCode, "the files-only handler must not expose the health route")
}

func TestRoutes_PanicRecovery_FilesEndpoint(t *testing.T) {
	t.Parallel()

	log, logs := newBufferLogger()
	srv := httptest.NewServer(Routes(log, panicFilesHandler{}))
	defer srv.Close()

	req := newMinimalUploadRequest(t, srv.URL, map[string]string{"X-Request-Id": "req-1"})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	require.Contains(t, logs.String(), "panic recovered")
	require.Equal(t, "req-1", lastLoggedReqID(t, logs))
}

func TestHealthRoutes_PanicRecovery(t *testing.T) {
	t.Parallel()

	log, _ := newBufferLogger()
	srv := httptest.NewServer(HealthRoutes(log, panicHealthHandler{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + healthPath)
	if err != nil {
		t.Fatalf("health endpoint panic was not recovered into a response: %v", err)
	}
	defer resp.Body.Close()

	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
}

func TestRoutes_RequestID_RequiredForFilesEndpoint(t *testing.T) {
	t.Parallel()

	log, _ := newBufferLogger()
	fileServiceCalled := false
	h := filesHandlerFunc(func(_ context.Context, _ *files.UploadPayload, body io.ReadCloser) (*files.UploadResult, error) {
		fileServiceCalled = true
		return &files.UploadResult{}, body.Close()
	})
	srv := httptest.NewServer(Routes(log, h))
	defer srv.Close()

	req := newMinimalUploadRequest(t, srv.URL, nil)
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusBadRequest, resp.StatusCode)
	require.False(t, fileServiceCalled, "files service must not be invoked without a request ID")
}

func TestRoutes_RequestID_PropagatesIncomingHeader(t *testing.T) {
	t.Parallel()

	log, logs := newBufferLogger()
	srv := httptest.NewServer(Routes(log, panicFilesHandler{}))
	defer srv.Close()

	req := newMinimalUploadRequest(t, srv.URL, map[string]string{"X-Request-Id": "custom-request-id-123"})
	resp, err := http.DefaultClient.Do(req)
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, "custom-request-id-123", lastLoggedReqID(t, logs))
}
