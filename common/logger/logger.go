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
	panic("logger: context without logger")
}

func RequestMiddleware(log *slog.Logger, reqIDMandatory bool) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rlog := log
			reqID, ok := r.Context().Value(middleware.RequestIDKey).(string)
			if ok {
				rlog = log.With(slog.String("reqID", reqID))
			} else if reqIDMandatory {
				panic("logger: context without request ID")
			}
			next.ServeHTTP(w, r.WithContext(WithCtx(r.Context(), rlog)))
		})
	}
}
