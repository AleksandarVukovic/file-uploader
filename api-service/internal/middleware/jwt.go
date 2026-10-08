package middleware

import (
	"context"
	"net/http"
	"strconv"
	"strings"

	commonmw "github.com/aleksandarv/file-uploader/common/http/middleware"
	"github.com/golang-jwt/jwt/v5"
)

func UserIDFromCtx(ctx context.Context) (int64, bool) {
	return commonmw.UserIDFromCtx(ctx)
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

			userID, err := strconv.ParseInt(claims.Subject, 10, 64)
			if err != nil {
				http.Error(w, "Invalid or expired token", http.StatusUnauthorized)
				return
			}

			ctx := commonmw.WithUserID(r.Context(), userID)
			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
