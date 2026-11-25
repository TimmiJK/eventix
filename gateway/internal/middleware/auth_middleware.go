package middleware

import (
	"context"
	jwtTokens "eventix/pkg/jwt"
	"log/slog"
	"net/http"
	"os"
	"strings"
)

type contextKey string

const (
	UserIDContextKey contextKey = "user_id"
	RoleContextKey   contextKey = "role"
)

func AuthMiddleware(logger *slog.Logger) func(http.Handler) http.Handler {
	jwttoken := os.Getenv("JWT_SECRET")

	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			authHeader := r.Header.Get("Authorization")

			if authHeader == "" {
				next.ServeHTTP(w, r)
				return
			}

			parts := strings.Fields(authHeader)
			if len(parts) != 2 || !strings.EqualFold(parts[0], "Bearer") {
				http.Error(w, "Unauthorized: invalid or expired token", http.StatusUnauthorized)
				return
			}

			token := parts[1]
			claims, err := jwtTokens.ValidateAccessToken(token, jwttoken)
			if err != nil {
				logger.Warn("Invalid access token in middleware", "error", err)
				http.Error(w, "Unauthorized: invalid or expired token", http.StatusUnauthorized)
				return
			}

			ctx := context.WithValue(r.Context(), UserIDContextKey, claims.UserID)
			ctx = context.WithValue(ctx, RoleContextKey, claims.Role)
			ctx = context.WithValue(ctx, "accessTokenKey", token)

			next.ServeHTTP(w, r.WithContext(ctx))
		})
	}
}
