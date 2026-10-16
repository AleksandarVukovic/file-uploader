package api

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/aleksandarv/file-uploader/file-processing-service/gen/health"
	healthsvr "github.com/aleksandarv/file-uploader/file-processing-service/gen/http/health/server"
	"github.com/stretchr/testify/require"
)

var healthPath = healthsvr.HealthHealthPath()

type panicHealthHandler struct{}

func (panicHealthHandler) Health(context.Context) (*health.HealthResult, error) {
	panic("boom: health handler panicked")
}

func newBufferLogger() (*slog.Logger, *bytes.Buffer) {
	var buf bytes.Buffer
	return slog.New(slog.NewJSONHandler(&buf, nil)), &buf
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
	var body struct {
		Status string `json:"status"`
	}
	require.NoError(t, json.NewDecoder(resp.Body).Decode(&body))
	require.Equal(t, "ok", body.Status)
}

func TestHealthRoutes_UnknownPath_NotFound(t *testing.T) {
	t.Parallel()

	log, _ := newBufferLogger()
	srv := httptest.NewServer(HealthRoutes(log, NewHealthHandler()))
	defer srv.Close()

	resp, err := http.Get(srv.URL + "/api/v1/files/upload")
	require.NoError(t, err)
	defer resp.Body.Close()

	require.Equal(t, http.StatusNotFound, resp.StatusCode)
}

func TestHealthRoutes_PanicRecovery(t *testing.T) {
	t.Parallel()

	log, logs := newBufferLogger()
	srv := httptest.NewServer(HealthRoutes(log, panicHealthHandler{}))
	defer srv.Close()

	resp, err := http.Get(srv.URL + healthPath)
	if err != nil {
		t.Fatalf("health endpoint panic was not recovered into a response: %v", err)
	}
	defer resp.Body.Close()

	require.Equal(t, http.StatusInternalServerError, resp.StatusCode)
	require.Contains(t, logs.String(), "panic recovered")
}
