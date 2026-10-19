package logger

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"testing"
)

func TestWithCtxAndFromCtx(t *testing.T) {
	log := NewLogger(false)
	ctx := WithCtx(context.Background(), log)

	got := FromCtx(ctx)
	if got != log {
		t.Errorf("expected same logger, got different instance")
	}
}

func TestFromCtx_PanicWithoutLogger(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic when no logger in context")
		}
	}()
	_ = FromCtx(context.Background())
}

func TestWithAttrs(t *testing.T) {
	var buf bytes.Buffer
	baseCtx := WithCtx(context.Background(), slog.New(slog.NewJSONHandler(&buf, nil)))

	derivedCtx, derivedLog := WithAttrs(baseCtx, "fileId", "abc")
	derivedLog.Info("from returned logger")
	FromCtx(derivedCtx).Info("from derived context")
	FromCtx(baseCtx).Info("from base context")

	var lines []map[string]any
	for _, raw := range bytes.Split(bytes.TrimSpace(buf.Bytes()), []byte("\n")) {
		var line map[string]any
		if err := json.Unmarshal(raw, &line); err != nil {
			t.Fatalf("invalid log line %q: %v", raw, err)
		}
		lines = append(lines, line)
	}
	if len(lines) != 3 {
		t.Fatalf("expected 3 log lines, got %d", len(lines))
	}
	for _, line := range lines[:2] {
		if got := line["fileId"]; got != "abc" {
			t.Errorf("expected %q to carry fileId=abc, got %v", line["msg"], got)
		}
	}
	if _, ok := lines[2]["fileId"]; ok {
		t.Errorf("expected original logger to be unchanged, got fileId=%v", lines[2]["fileId"])
	}
}

func TestWithAttrs_PanicWithoutLogger(t *testing.T) {
	defer func() {
		if r := recover(); r == nil {
			t.Errorf("expected panic when no logger in context")
		}
	}()
	_, _ = WithAttrs(context.Background(), "fileId", "abc")
}
