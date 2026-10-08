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

func TestRequireContextHeaders(t *testing.T) {
	tests := []struct {
		name       string
		reqID      string
		userID     string
		wantStatus int
	}{
		{"both present", "req-1", "42", http.StatusOK},
		{"missing request id", "", "42", http.StatusBadRequest},
		{"missing user id", "req-1", "", http.StatusBadRequest},
		{"non-numeric user id", "req-1", "alice", http.StatusBadRequest},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var gotReqID any
			var gotUserID int64
			next := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				gotReqID = r.Context().Value(middleware.RequestIDKey)
				gotUserID, _ = UserIDFromCtx(r.Context())
			})
			req := httptest.NewRequest(http.MethodGet, "/", nil)
			if tt.reqID != "" {
				req.Header.Set(RequestIDHeader, tt.reqID)
			}
			if tt.userID != "" {
				req.Header.Set(UserIDHeader, tt.userID)
			}
			rec := httptest.NewRecorder()

			RequireContextHeaders()(next).ServeHTTP(rec, req)

			if rec.Code != tt.wantStatus {
				t.Fatalf("expected status %d, got %d", tt.wantStatus, rec.Code)
			}
			if tt.wantStatus == http.StatusOK && (gotReqID != "req-1" || gotUserID != 42) {
				t.Errorf("expected reqID req-1 and userID 42 in context, got %v and %d", gotReqID, gotUserID)
			}
		})
	}
}
