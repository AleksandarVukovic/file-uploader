package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/aleksandarv/file-uploader/common/logger"
	"goa.design/goa/v3/middleware"
)

const (
	RequestIDHeader = "X-Request-Id"
	UsernameHeader  = "X-Username"
)

type ctxKey int

const usernameCtxKey ctxKey = iota

func WithUsername(ctx context.Context, username string) context.Context {
	return context.WithValue(ctx, usernameCtxKey, username)
}

func UsernameFromCtx(ctx context.Context) (string, bool) {
	username, ok := ctx.Value(usernameCtxKey).(string)
	return username, ok
}

func RequireContextHeaders() func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			for _, header := range []string{RequestIDHeader, UsernameHeader} {
				if r.Header.Get(header) == "" {
					http.Error(w, "missing "+header+" header", http.StatusBadRequest)
					return
				}
			}
			ctx := context.WithValue(r.Context(), middleware.RequestIDKey, r.Header.Get(RequestIDHeader))
			ctx = WithUsername(ctx, r.Header.Get(UsernameHeader))
			h.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}

func Logger(log *slog.Logger, reqIDMandatory bool) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			rlog := log
			reqID, ok := r.Context().Value(middleware.RequestIDKey).(string)
			if ok {
				rlog = logger.WithRequestID(log, reqID)
			} else if reqIDMandatory {
				panic("logger: context without request ID")
			}
			next.ServeHTTP(w, r.WithContext(logger.WithCtx(r.Context(), rlog)))
		})
	}
}

func PanicHandler() func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if err := recover(); err != nil {
					log := logger.FromCtx(r.Context())
					// TODO: add alert
					log.Error("panic recovered", "err", err)
					http.Error(w, "Internal Server Error", http.StatusInternalServerError)
				}
			}()

			h.ServeHTTP(w, r)
		})
	}
}
