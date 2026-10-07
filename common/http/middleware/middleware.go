package middleware

import (
	"context"
	"log/slog"
	"net/http"

	"github.com/aleksandarv/file-uploader/common/logger"
	goam "goa.design/goa/v3/http/middleware"
	"goa.design/goa/v3/middleware"
)

const RequestIDHeader = "X-Request-Id"

func RequireRequestID() func(http.Handler) http.Handler {
	return func(h http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ctx := r.Context()
			reqID := ctx.Value(goam.RequestXRequestIDKey)
			if reqID == "" {
				http.Error(w, "missing "+RequestIDHeader+" header", http.StatusBadRequest)
				return
			}
			ctx = context.WithValue(ctx, middleware.RequestIDKey, reqID)
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
