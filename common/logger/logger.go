package logger

import (
	"context"
	"log/slog"
	"net/http"
	"os"

	"goa.design/goa/v3/middleware"
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
	panic("context without logger")
}

func RequestMiddleware(log *slog.Logger) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			if reqID := r.Context().Value(middleware.RequestIDKey); reqID != nil {
				rlog := log.With(slog.String("reqID", reqID.(string)))
				ctx := context.WithValue(r.Context(), loggerKey, rlog)
				next.ServeHTTP(w, r.WithContext(ctx))
			} else {
				next.ServeHTTP(w, r)
			}
		})
	}
}
