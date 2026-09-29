package tests

import (
	"eventix/gateway/internal/middleware"
	jwtTokens "eventix/pkg/jwt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestAuthMiddleware_ValidToken_Success(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	token, err := jwtTokens.CreateAccessToken(
		uuid.New(),
		"test@test.com",
		"user",
		"test-secret",
		15*time.Minute,
	)
	require.NoError(t, err)

	logger := slog.New(slog.DiscardHandler)

	handler := middleware.AuthMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		userID := r.Context().Value(middleware.UserIDContextKey).(string)
		role := r.Context().Value(middleware.RoleContextKey).(string)

		assert.NotEmpty(t, userID)
		assert.Equal(t, "user", role)

		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_NoToken(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)

	handler := middleware.AuthMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))

	req := httptest.NewRequest("GET", "/", nil)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusOK, w.Code)
}

func TestAuthMiddleware_InvalidToken(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)

	handler := middleware.AuthMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("Handler should not be called with invalid token")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer invalid-token")
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_InvalidSecret(t *testing.T) {
	t.Setenv("JWT_SECRET", "new-test-secret")

	token, err := jwtTokens.CreateAccessToken(
		uuid.New(),
		"test@test.com",
		"user",
		"test-secret",
		15*time.Minute,
	)
	require.NoError(t, err)

	logger := slog.New(slog.DiscardHandler)

	handler := middleware.AuthMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("Handler should not be called with invalid token")
	}))

	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()

	handler.ServeHTTP(w, req)

	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_ExpiredToken(t *testing.T) {
	t.Setenv("JWT_SECRET", "test-secret")

	token, err := jwtTokens.CreateAccessToken(
		uuid.New(),
		"test@test.com",
		"user",
		"test-secret",
		1*time.Millisecond,
	)
	require.NoError(t, err)

	time.Sleep(10 * time.Millisecond)

	logger := slog.New(slog.DiscardHandler)
	handler := middleware.AuthMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("Handler should not be called with expired token")
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer "+token)
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_WrongAuthScheme(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	handler := middleware.AuthMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusOK)
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Basic some-token")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}

func TestAuthMiddleware_EmptyBearerToken(t *testing.T) {
	logger := slog.New(slog.DiscardHandler)
	handler := middleware.AuthMiddleware(logger)(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("Handler should not be called with empty bearer token")
	}))
	req := httptest.NewRequest("GET", "/", nil)
	req.Header.Set("Authorization", "Bearer ")
	w := httptest.NewRecorder()
	handler.ServeHTTP(w, req)
	assert.Equal(t, http.StatusUnauthorized, w.Code)
}
