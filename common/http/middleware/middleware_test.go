package middleware

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/aleksandarv/file-uploader/common/logger"
	"goa.design/goa/v3/middleware"
)

func TestLogger_AttachesReqIDWhenPresent(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	mw := Logger(log, true)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.FromCtx(r.Context()).Info("handled")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	req = req.WithContext(context.WithValue(req.Context(), middleware.RequestIDKey, "abc-123")) //nolint:staticcheck

	mw(next).ServeHTTP(httptest.NewRecorder(), req)

	if !strings.Contains(buf.String(), `"reqID":"abc-123"`) {
		t.Errorf("expected reqID in log output, got: %s", buf.String())
	}
}

func TestLogger_AttachesLoggerWithoutReqIDWhenNotMandatory(t *testing.T) {
	var buf bytes.Buffer
	log := slog.New(slog.NewJSONHandler(&buf, nil))
	mw := Logger(log, false)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		logger.FromCtx(r.Context()).Info("handled")
	})

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	mw(next).ServeHTTP(httptest.NewRecorder(), req)

	if strings.Contains(buf.String(), "reqID") {
		t.Errorf("expected no reqID field, got: %s", buf.String())
	}
}

func TestLogger_PanicsWhenMandatoryReqIDMissing(t *testing.T) {
	log := logger.NewLogger(false)
	mw := Logger(log, true)

	next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("handler should not run when mandatory request ID is missing")
	})

	defer func() {
		if r := recover(); r == nil {
			t.Error("expected panic when request ID is mandatory but missing")
		}
	}()

	req := httptest.NewRequest(http.MethodGet, "/", nil)
	mw(next).ServeHTTP(httptest.NewRecorder(), req)
}
