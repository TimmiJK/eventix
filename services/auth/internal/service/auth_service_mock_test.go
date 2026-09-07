package auth

import (
	"context"
	"crypto/rand"
	auth "eventix/pkg/jwt"
	pb "eventix/proto/auth/pb"
	"eventix/services/auth/internal/models"
	"fmt"
	"log/slog"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"golang.org/x/crypto/argon2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type MockRepository struct {
	MockCreateUser          func(ctx context.Context, email string, name string, password []byte) (uuid.UUID, error)
	MockGetUserByEmail      func(ctx context.Context, email string) (models.User, error)
	MockGetUserByID         func(ctx context.Context, userID uuid.UUID) (models.User, error)
	MockSaveRefreshToken    func(ctx context.Context, userID uuid.UUID, tokenID string, tokenExpTime time.Duration) error
	MockIsRefreshTokenValid func(ctx context.Context, tokenID string, userID uuid.UUID) (bool, error)
	MockRevokeRefreshToken  func(ctx context.Context, tokenID string, userID uuid.UUID) error
}

func (m *MockRepository) CreateUser(ctx context.Context, email, name string, role string, password []byte) (uuid.UUID, error) {
	return m.MockCreateUser(ctx, email, name, password)
}
func (m *MockRepository) GetUserByEmail(ctx context.Context, email string) (models.User, error) {
	return m.MockGetUserByEmail(ctx, email)
}
func (m *MockRepository) GetUserByID(ctx context.Context, userID uuid.UUID) (models.User, error) {
	return m.MockGetUserByID(ctx, userID)
}
func (m *MockRepository) SaveRefreshToken(ctx context.Context, userID uuid.UUID, tokenID string, tokenExpTime time.Duration) error {
	return m.MockSaveRefreshToken(ctx, userID, tokenID, tokenExpTime)
}
func (m *MockRepository) IsRefreshTokenValid(ctx context.Context, tokenID string, userID uuid.UUID) (bool, error) {
	return m.MockIsRefreshTokenValid(ctx, tokenID, userID)
}
func (m *MockRepository) RevokeRefreshToken(ctx context.Context, tokenID string, userID uuid.UUID) error {
	return m.MockRevokeRefreshToken(ctx, tokenID, userID)
}

func TestAuthManager_Register(t *testing.T) {
	mockRepo := &MockRepository{
		MockCreateUser: func(ctx context.Context, email, name string, password []byte) (uuid.UUID, error) {
			return uuid.New(), nil
		},
		MockSaveRefreshToken: func(ctx context.Context, userID uuid.UUID, tokenID string, tokenExpTime time.Duration) error {
			return fmt.Errorf("Some Bad Error")
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewAuthManager(mockRepo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)

	t.Run("InvalidEmail", func(t *testing.T) {
		tests := []struct {
			name    string
			email   string
			message string
		}{
			{"EmptyEmail", "", "invalid email format"},
			{"InvalidEmailFormat_1", "sasassadafdsfsg", "invalid email format"},
			{"InvalidEmailFormat_2", "saas.com", "invalid email format"},
			{"InvalidEmailFormat_3", "saas@.com", "invalid email format"},
			{"InvalidEmailFormat_4", "saas@com", "invalid email format"},
			{"InvalidEmailFormat_5", "@.", "invalid email format"},
			{"InvalidEmailFormat_6", "a@b.c", "invalid email format"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := &pb.RegisterRequest{
					Email:    tt.email,
					Password: "wrongpassword123",
					Name:     "TestUser",
				}
				resp, err := manager.Register(context.Background(), req)
				assert.Nil(t, resp)
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, codes.InvalidArgument, st.Code())
				assert.Equal(t, tt.message, st.Message())
			})
		}
	})

	t.Run("InvalidPassword", func(t *testing.T) {
		tests := []struct {
			name     string
			password string
			message  string
		}{
			{"EmptyPassword", "", "password must be at least 10 characters"},
			{"Length_8Symbols", "password", "password must be at least 10 characters"},
			{"Length_1Symbol", "a", "password must be at least 10 characters"},
			{"Length_9Symbols", "123456789", "password must be at least 10 characters"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := &pb.RegisterRequest{
					Email:    "testemail@test.com",
					Password: tt.password,
					Name:     "TestUser",
				}
				resp, err := manager.Register(context.Background(), req)
				assert.Nil(t, resp)
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, codes.InvalidArgument, st.Code())
				assert.Equal(t, tt.message, st.Message())
			})
		}
	})

	t.Run("EmptyName", func(t *testing.T) {
		req := &pb.RegisterRequest{
			Email:    "testemail@test.com",
			Password: "strongpassword1255",
			Name:     "",
		}
		resp, err := manager.Register(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "name is required", st.Message())
	})

	t.Run("SaveRefreshTokenError", func(t *testing.T) {
		req := &pb.RegisterRequest{
			Email:    "testemail@test.com",
			Password: "strongpassword1255",
			Name:     "Test",
		}
		resp, err := manager.Register(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Internal, st.Code())
		assert.Contains(t, st.Message(), "failed to create tokens:")
	})
}

func TestAuthManager_Register_ValidPassword_10Symbols(t *testing.T) {
	mockRepo := &MockRepository{
		MockCreateUser: func(ctx context.Context, email, name string, password []byte) (uuid.UUID, error) {
			return uuid.New(), nil
		},
		MockSaveRefreshToken: func(ctx context.Context, userID uuid.UUID, tokenID string, tokenExpTime time.Duration) error {
			return nil
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewAuthManager(mockRepo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)

	req := &pb.RegisterRequest{
		Email:    "testemail@test.com",
		Password: "1234567890",
		Name:     "TestUser",
	}
	resp, err := manager.Register(context.Background(), req)
	assert.NotEmpty(t, resp)
	require.NoError(t, err)
}

func TestAuthManager_Register_Success(t *testing.T) {
	expectedID := uuid.New()
	mockRepo := &MockRepository{
		MockCreateUser: func(ctx context.Context, email, name string, password []byte) (uuid.UUID, error) {
			return expectedID, nil
		},
		MockSaveRefreshToken: func(ctx context.Context, userID uuid.UUID, tokenID string, tokenExpTime time.Duration) error {
			return nil
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewAuthManager(mockRepo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)

	req := &pb.RegisterRequest{
		Email:    "test@example.com",
		Password: "strongpassword123",
		Name:     "Test User",
	}

	resp, err := manager.Register(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, expectedID.String(), resp.UserId)
	assert.NotEmpty(t, resp.AccessToken)
	assert.NotEmpty(t, resp.RefreshToken)
	assert.Equal(t, int64(900), resp.ExpiresIn)
}

func TestAuthManager_Login(t *testing.T) {
	mockRepo := &MockRepository{
		MockGetUserByEmail: func(ctx context.Context, email string) (models.User, error) {
			return models.User{
				ID:       uuid.New(),
				Email:    "test@example.com",
				Password: []byte("wrong_hash"),
			}, nil
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewAuthManager(mockRepo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)

	t.Run("EmptyFields", func(t *testing.T) {
		tests := []struct {
			name     string
			email    string
			password string
			message  string
		}{
			{"EmptyEmail", "", "wrongpassword123", "email and password are required"},
			{"EmptyPassword", "test@example.com", "", "email and password are required"},
			{"EmptyBoth", "", "", "email and password are required"},
		}

		for _, tt := range tests {
			t.Run(tt.name, func(t *testing.T) {
				req := &pb.LoginRequest{
					Email:    tt.email,
					Password: tt.password,
				}
				resp, err := manager.Login(context.Background(), req)
				assert.Nil(t, resp)
				require.Error(t, err)
				st, ok := status.FromError(err)
				require.True(t, ok)
				assert.Equal(t, codes.InvalidArgument, st.Code())
				assert.Equal(t, tt.message, st.Message())
			})
		}
	})

	t.Run("InvalidCredentials", func(t *testing.T) {
		req := &pb.LoginRequest{
			Email:    "test@example.com",
			Password: "wrongpassword123",
		}
		resp, err := manager.Login(context.Background(), req)
		assert.Nil(t, resp)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
		assert.Equal(t, "invalid credentials", st.Message())
	})
}

func TestAuthManager_Login_Success(t *testing.T) {
	userID := uuid.New()
	salt := make([]byte, 8)
	rand.Read(salt)
	hashedPass := argon2.IDKey([]byte("strongpassword123"), salt, 1, 64*1024, 4, 32)
	storedPass := append(salt, hashedPass...)

	mockRepo := &MockRepository{
		MockGetUserByEmail: func(ctx context.Context, email string) (models.User, error) {
			return models.User{
				ID:       userID,
				Email:    "test@example.com",
				Password: storedPass,
			}, nil
		},
		MockSaveRefreshToken: func(ctx context.Context, userID uuid.UUID, tokenID string, tokenExpTime time.Duration) error {
			return nil
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewAuthManager(mockRepo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)

	req := &pb.LoginRequest{
		Email:    "test@example.com",
		Password: "strongpassword123",
	}

	resp, err := manager.Login(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.Equal(t, userID.String(), resp.UserId)
	assert.NotEmpty(t, resp.AccessToken)
	assert.NotEmpty(t, resp.RefreshToken)
}

func TestAuthManager_Logout(t *testing.T) {
	mockRepo := &MockRepository{
		MockRevokeRefreshToken: func(ctx context.Context, tokenID string, userID uuid.UUID) error {
			return fmt.Errorf("bad error")
		},
	}

	logger := slog.New(slog.DiscardHandler)

	manager := NewAuthManager(mockRepo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)
	t.Run("InvalidRefreshToken", func(t *testing.T) {
		reqLogout := &pb.LogoutRequest{
			RefreshToken: "bad token",
		}

		respLogout, err := manager.Logout(context.Background(), reqLogout)
		assert.Empty(t, respLogout)
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
		assert.Equal(t, "invalid refresh token", st.Message())
	})
}

func TestAuthManager_Logout_Success(t *testing.T) {
	userID := uuid.New()
	tokenID := "test-token-id"
	claims := &auth.RefreshClaims{
		UserID:  userID.String(),
		TokenID: tokenID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	refreshToken, _ := token.SignedString([]byte("test-secret"))

	mockRepo := &MockRepository{
		MockRevokeRefreshToken: func(ctx context.Context, tokenID string, userID uuid.UUID) error {
			return nil
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewAuthManager(mockRepo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)

	req := &pb.LogoutRequest{
		RefreshToken: refreshToken,
	}

	resp, err := manager.Logout(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.True(t, resp.Success)
}

func TestAuthManager_RefreshToken_Success(t *testing.T) {
	userID := uuid.New()
	tokenID := "test-token-id"

	claims := &auth.RefreshClaims{
		UserID:  userID.String(),
		TokenID: tokenID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	refreshToken, _ := token.SignedString([]byte("test-secret"))

	mockRepo := &MockRepository{
		MockIsRefreshTokenValid: func(ctx context.Context, tokenID string, userID uuid.UUID) (bool, error) {
			return true, nil
		},
		MockGetUserByID: func(ctx context.Context, userID uuid.UUID) (models.User, error) {
			return models.User{
				ID:    userID,
				Email: "test@example.com",
				Role:  "user",
			}, nil
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewAuthManager(mockRepo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)

	req := &pb.RefreshTokenRequest{
		RefreshToken: refreshToken,
	}

	resp, err := manager.RefreshToken(context.Background(), req)

	require.NoError(t, err)
	require.NotNil(t, resp)
	assert.NotEmpty(t, resp.AccessToken)
	assert.Equal(t, int64(900), resp.ExpiresIn)
}

func TestAuthManager_RefreshToken_RevokedToken(t *testing.T) {
	userID := uuid.New()
	tokenID := "test-token-id"

	claims := &auth.RefreshClaims{
		UserID:  userID.String(),
		TokenID: tokenID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	refreshToken, _ := token.SignedString([]byte("test-secret"))

	mockRepo := &MockRepository{
		MockIsRefreshTokenValid: func(ctx context.Context, tokenID string, userID uuid.UUID) (bool, error) {
			return false, nil
		},
	}

	logger := slog.New(slog.DiscardHandler)
	manager := NewAuthManager(mockRepo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)

	req := &pb.RefreshTokenRequest{
		RefreshToken: refreshToken,
	}

	resp, err := manager.RefreshToken(context.Background(), req)

	assert.Nil(t, resp)
	require.Error(t, err)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	assert.Equal(t, "refresh token has been revoked", st.Message())
}

func TestAuthManager_ValidateToken(t *testing.T) {
	userID := uuid.New()
	tokenID := "test-token-id"
	email := "testuser@gmail.com"
	role := "user"
	claimsAccess := &auth.AccessClaims{
		UserID: userID.String(),
		Email:  email,
		Role:   role,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "eventix",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claimsAccess)
	accessToken, _ := token.SignedString([]byte("test-secret"))

	claimsRefresh := &auth.RefreshClaims{
		UserID:  userID.String(),
		TokenID: tokenID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "eventix",
		},
	}
	token = jwt.NewWithClaims(jwt.SigningMethodHS256, claimsRefresh)
	refreshToken, _ := token.SignedString([]byte("test-secret"))

	mockRepo := &MockRepository{
		MockGetUserByID: func(ctx context.Context, userID uuid.UUID) (models.User, error) {
			return models.User{ID: userID, Email: email, Role: role}, nil
		},
		MockIsRefreshTokenValid: func(ctx context.Context, tokenID string, userID uuid.UUID) (bool, error) {
			return true, nil
		},
	}

	logger := slog.New(slog.DiscardHandler)

	manager := NewAuthManager(mockRepo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)
	ctx := context.Background()
	t.Run("ValidAccessToken", func(t *testing.T) {
		req := &pb.ValidateTokenRequest{
			Token: accessToken,
		}
		resp, err := manager.ValidateToken(ctx, req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp)

		assert.True(t, resp.IsValid)
		assert.Equal(t, userID.String(), resp.UserId)
		assert.Equal(t, email, resp.Email)
		assert.Equal(t, role, resp.Role)
	})

	t.Run("ValidRefreshToken", func(t *testing.T) {
		req := &pb.ValidateTokenRequest{
			Token: refreshToken,
		}
		resp, err := manager.ValidateToken(ctx, req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp)

		assert.True(t, resp.IsValid)
		assert.Equal(t, userID.String(), resp.UserId)
		assert.Equal(t, email, resp.Email)
		assert.Equal(t, role, resp.Role)
	})

	t.Run("InvalidAccessToken", func(t *testing.T) {
		req := &pb.ValidateTokenRequest{
			Token: "invalid-token",
		}
		resp, err := manager.ValidateToken(ctx, req)
		require.Error(t, err)
		assert.Empty(t, resp)

		assert.False(t, resp.IsValid)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
		assert.Equal(t, "invalid token", st.Message())
	})

	t.Run("InvalidRefreshToken", func(t *testing.T) {
		req := &pb.ValidateTokenRequest{
			Token: "invalid-token",
		}
		resp, err := manager.ValidateToken(ctx, req)
		require.Error(t, err)
		assert.Empty(t, resp)

		assert.False(t, resp.IsValid)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
		assert.Equal(t, "invalid token", st.Message())
	})
}

func TestAuthManager_ValidateTokenErrors(t *testing.T) {
	userID := "invalid-user-id"
	tokenID := "test-token-id"
	email := "testuser@gmail.com"
	role := "user"

	mockRepo := &MockRepository{
		MockGetUserByID: func(ctx context.Context, userID uuid.UUID) (models.User, error) {
			return models.User{}, fmt.Errorf("no user found")
		},
		MockIsRefreshTokenValid: func(ctx context.Context, tokenID string, userID uuid.UUID) (bool, error) {
			return false, fmt.Errorf("token revoke")
		},
	}

	logger := slog.New(slog.DiscardHandler)

	manager := NewAuthManager(mockRepo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)
	ctx := context.Background()

	t.Run("InvalidAccessToken_WrongIDFormat", func(t *testing.T) {
		claimsAccess := &auth.AccessClaims{
			UserID: userID,
			Email:  email,
			Role:   role,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claimsAccess)
		accessToken, _ := token.SignedString([]byte("test-secret"))

		req := &pb.ValidateTokenRequest{
			Token: accessToken,
		}
		resp, err := manager.ValidateToken(ctx, req)
		require.Error(t, err)
		assert.Empty(t, resp)

		assert.False(t, resp.IsValid)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
		assert.Equal(t, "invalid token", st.Message())
	})

	t.Run("InvalidAccessToken_NoEmailAndRole", func(t *testing.T) {
		claimsAccess := &auth.AccessClaims{
			UserID: userID,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claimsAccess)
		accessToken, _ := token.SignedString([]byte("test-secret"))

		req := &pb.ValidateTokenRequest{
			Token: accessToken,
		}
		resp, err := manager.ValidateToken(ctx, req)
		require.Error(t, err)
		assert.Empty(t, resp)

		assert.False(t, resp.IsValid)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
		assert.Equal(t, "invalid token", st.Message())
	})

	t.Run("InvalidRefreshToken_WrongIDFormat", func(t *testing.T) {
		claimsRefresh := &auth.RefreshClaims{
			UserID:  userID,
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claimsRefresh)
		refreshToken, _ := token.SignedString([]byte("test-secret"))

		req := &pb.ValidateTokenRequest{
			Token: refreshToken,
		}
		resp, err := manager.ValidateToken(ctx, req)
		require.Error(t, err)
		assert.Empty(t, resp)

		assert.False(t, resp.IsValid)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
		assert.Equal(t, "invalid token", st.Message())
	})

	t.Run("InvalidRefreshToken_RevokeToken", func(t *testing.T) {
		claimsRefresh := &auth.RefreshClaims{
			UserID:  uuid.NewString(),
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claimsRefresh)
		refreshToken, _ := token.SignedString([]byte("test-secret"))

		req := &pb.ValidateTokenRequest{
			Token: refreshToken,
		}
		resp, err := manager.ValidateToken(ctx, req)
		require.Error(t, err)
		assert.Empty(t, resp)

		assert.False(t, resp.IsValid)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Internal, st.Code())
		assert.Equal(t, "token was revoked or invalid", st.Message())
	})

	t.Run("InvalidRefreshToken_UserNotFound", func(t *testing.T) {
		mockRepo := &MockRepository{
			MockGetUserByID: func(ctx context.Context, userID uuid.UUID) (models.User, error) {
				return models.User{}, fmt.Errorf("no user found")
			},
			MockIsRefreshTokenValid: func(ctx context.Context, tokenID string, userID uuid.UUID) (bool, error) {
				return true, nil
			},
		}

		logger := slog.New(slog.DiscardHandler)

		manager := NewAuthManager(mockRepo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)
		ctx := context.Background()

		claimsRefresh := &auth.RefreshClaims{
			UserID:  uuid.NewString(),
			TokenID: tokenID,
			RegisteredClaims: jwt.RegisteredClaims{
				ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			},
		}
		token := jwt.NewWithClaims(jwt.SigningMethodHS256, claimsRefresh)
		refreshToken, _ := token.SignedString([]byte("test-secret"))

		req := &pb.ValidateTokenRequest{
			Token: refreshToken,
		}
		resp, err := manager.ValidateToken(ctx, req)
		require.Error(t, err)
		assert.Empty(t, resp)

		assert.False(t, resp.IsValid)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Internal, st.Code())
		assert.Equal(t, "failed to get user", st.Message())
	})

	t.Run("ValidateToken_MalformedJWT", func(t *testing.T) {
		mockRepo := &MockRepository{}
		logger := slog.New(slog.DiscardHandler)
		manager := NewAuthManager(mockRepo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)

		req := &pb.ValidateTokenRequest{Token: "this.is.not.a.valid.jwt.string.at.all"}
		resp, err := manager.ValidateToken(context.Background(), req)

		require.Error(t, err)
		assert.False(t, resp.IsValid)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
		assert.Equal(t, "invalid token", st.Message())
	})
}
