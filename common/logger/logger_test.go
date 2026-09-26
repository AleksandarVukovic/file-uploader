package logger

import (
	"context"
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
