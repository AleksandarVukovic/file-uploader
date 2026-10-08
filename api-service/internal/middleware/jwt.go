package middleware

import (
	"context"
	"net/http"
	"strings"

	commonmw "github.com/aleksandarv/file-uploader/common/http/middleware"
	"github.com/golang-jwt/jwt/v5"
)

func UsernameFromCtx(ctx context.Context) (string, bool) {
	return commonmw.UsernameFromCtx(ctx)
}

func JWT(secret []byte) func(next http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			auth := r.Header.Get("Authorization")
			if auth == "" {
				http.Error(w, "Missing authorization header", http.StatusUnauthorized)
				return
			}

			parts := strings.SplitN(auth, " ", 2)
			if len(parts) != 2 || parts[0] != "Bearer" {
				http.Error(w, "Invalid authorization header", http.StatusUnauthorized)
				return
			}

			rawToken := parts[1]
			claims := &jwt.RegisteredClaims{}
			_, err := jwt.ParseWithClaims(rawToken, claims, func(t *jwt.Token) (any, error) {
				return secret, nil
			}, jwt.WithValidMethods([]string{jwt.SigningMethodHS256.Name}), jwt.WithIssuedAt(), jwt.WithExpirationRequired())
			if err != nil {
				http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
				return
			}

			ctx := commonmw.WithUsername(r.Context(), claims.Subject)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
