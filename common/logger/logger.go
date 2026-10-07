package logger

import (
	"context"
	"log/slog"
	"os"
)

type key int

const (
	loggerKey key = iota + 1
)

func NewLogger(debug bool) *slog.Logger {
	loglvl := slog.LevelInfo
	if debug {
		loglvl = slog.LevelDebug
	}
	return slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: loglvl,
	}))
}

func WithCtx(ctx context.Context, log *slog.Logger) context.Context {
	return context.WithValue(ctx, loggerKey, log)
}

func FromCtx(ctx context.Context) *slog.Logger {
	if log := ctx.Value(loggerKey); log != nil {
		return log.(*slog.Logger)
	}
	panic("logger: context without logger")
}

func WithRequestID(log *slog.Logger, reqID string) *slog.Logger {
	return log.With(slog.String("reqID", reqID))
}
