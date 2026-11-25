package tests

import (
	"context"
	"database/sql"
	"eventix/pkg/helpers"
	jwtTokens "eventix/pkg/jwt"
	"eventix/proto/auth/pb"
	"eventix/services/auth/internal/authctx"
	"eventix/services/auth/internal/repository"
	auth "eventix/services/auth/internal/service"
	"log/slog"
	"testing"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

func TestGRPCServiceAuth(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "auth")
	defer env.Cleanup(t)

	repo := repository.NewPostgresStorage(env.DB)
	logger := slog.New(slog.DiscardHandler)
	authManager := auth.NewAuthManager(repo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)

	ctxReq := context.Background()
	email := "test@example.com"
	password := "testintegrationpassword135"

	t.Run("Register", func(t *testing.T) {
		req := &pb.RegisterRequest{
			Email:    email,
			Password: password,
			Name:     "test user",
		}

		resp, err := authManager.Register(ctxReq, req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.UserId)
		assert.NotEmpty(t, resp.AccessToken)
		assert.NotEmpty(t, resp.RefreshToken)
		assert.NotEmpty(t, resp.ExpiresIn)
	})

	t.Run("InvalidPasswordForLogin", func(t *testing.T) {
		req := &pb.LoginRequest{
			Email:    email,
			Password: "strongpassword145787",
		}

		resp, err := authManager.Login(ctxReq, req)
		assert.Nil(t, resp)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
		assert.Equal(t, "invalid credentials", st.Message())
	})

	var savedRefreshToken string
	t.Run("Login", func(t *testing.T) {
		req := &pb.LoginRequest{
			Email:    email,
			Password: password,
		}

		resp, err := authManager.Login(ctxReq, req)
		require.NoError(t, err)
		assert.NotEmpty(t, resp.UserId)
		assert.NotEmpty(t, resp.AccessToken)
		assert.NotEmpty(t, resp.RefreshToken)
		assert.NotEmpty(t, resp.ExpiresIn)

		savedRefreshToken = resp.RefreshToken
	})

	t.Run("Logout", func(t *testing.T) {
		req := &pb.LogoutRequest{
			RefreshToken: savedRefreshToken,
		}

		resp, err := authManager.Logout(ctxReq, req)
		require.NoError(t, err)
		assert.True(t, resp.Success)
	})

	t.Run("RefreshTokenRevoke", func(t *testing.T) {
		req := &pb.RefreshTokenRequest{
			RefreshToken: savedRefreshToken,
		}

		_, err := authManager.RefreshToken(ctxReq, req)
		require.Error(t, err)
		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
		assert.Equal(t, "invalid refresh token", st.Message())
	})
}

func TestAuthCreateUser_Error(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "auth")
	defer env.Cleanup(t)

	repo := repository.NewPostgresStorage(env.DB)
	logger := slog.New(slog.DiscardHandler)
	authManager := auth.NewAuthManager(repo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)

	ctxReq := context.Background()

	t.Run("RegisterEmailAlreadyExists", func(t *testing.T) {
		req1 := &pb.RegisterRequest{
			Email:    "test@example.com",
			Password: "testintegrationpassword135",
			Name:     "test user",
		}

		resp1, err1 := authManager.Register(ctxReq, req1)
		require.NoError(t, err1)
		assert.NotEmpty(t, resp1.UserId)
		assert.NotEmpty(t, resp1.AccessToken)
		assert.NotEmpty(t, resp1.RefreshToken)
		assert.NotEmpty(t, resp1.ExpiresIn)

		req2 := &pb.RegisterRequest{
			Email:    "test@example.com",
			Password: "testintegrationpassword135",
			Name:     "test user",
		}

		resp2, err2 := authManager.Register(ctxReq, req2)
		assert.Nil(t, resp2)
		require.Error(t, err2)

		st, ok := status.FromError(err2)
		require.True(t, ok)
		assert.Equal(t, codes.InvalidArgument, st.Code())
		assert.Equal(t, "unable to create account", st.Message())
	})

	t.Run("RegisterDbError", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		req := &pb.RegisterRequest{
			Email:    "testemail@test.com",
			Password: "passwordvalue125677",
			Name:     "test user",
		}

		resp, err := authManager.Register(ctx, req)
		assert.Nil(t, resp)
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Internal, st.Code())
		assert.Contains(t, st.Message(), "failed to create user")
	})
}

func TestAuthSaveRefreshToken_Error(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "auth")
	defer env.Cleanup(t)

	repo := repository.NewPostgresStorage(env.DB)
	logger := slog.New(slog.DiscardHandler)
	authManager := auth.NewAuthManager(repo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)

	ctx := context.Background()
	t.Run("ForeignKeyViolation", func(t *testing.T) {
		fakeUserID := uuid.New()
		tokenID := "test-token-id-123"

		err := repo.SaveRefreshToken(ctx, fakeUserID, tokenID, 24*time.Hour)
		require.Error(t, err)
		assert.Contains(t, err.Error(), "violates foreign key constraint")
	})

	t.Run("DuplicateToken", func(t *testing.T) {
		req := &pb.RegisterRequest{
			Email:    "testemail@test.com",
			Password: "passwordvalue125677",
			Name:     "test user",
		}
		resp, err := authManager.Register(ctx, req)
		assert.NotEmpty(t, resp)
		require.NoError(t, err)

		tokenID := "unique-token-123"
		userID, err := uuid.Parse(resp.UserId)
		require.NoError(t, err)

		err = repo.SaveRefreshToken(ctx, userID, tokenID, 24*time.Hour)
		require.NoError(t, err)

		err = repo.SaveRefreshToken(ctx, userID, tokenID, 24*time.Hour)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "duplicate key value")
	})

	t.Run("ContextCancelled", func(t *testing.T) {
		ctx, cancel := context.WithCancel(context.Background())

		req := &pb.RegisterRequest{
			Email:    "newtestemail@test.com",
			Password: "passwordvalue125677",
			Name:     "test user",
		}
		resp, err := authManager.Register(ctx, req)
		assert.NotEmpty(t, resp)
		require.NoError(t, err)

		userID, err := uuid.Parse(resp.UserId)
		require.NoError(t, err)
		cancel()

		err = repo.SaveRefreshToken(ctx, userID, "token-ctx-test", 24*time.Hour)

		require.Error(t, err)
		assert.Contains(t, err.Error(), "context cancel")
	})
}

func TestAuthLogin_UnknownEmail(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "auth")
	defer env.Cleanup(t)

	repo := repository.NewPostgresStorage(env.DB)
	logger := slog.New(slog.DiscardHandler)
	authManager := auth.NewAuthManager(repo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)

	ctx := context.Background()

	reqReg := &pb.RegisterRequest{
		Email:    "testemail@test.com",
		Password: "passwordvalue125677",
		Name:     "test user",
	}

	respReg, err := authManager.Register(ctx, reqReg)
	require.NoError(t, err)
	assert.NotEmpty(t, respReg.UserId)
	assert.NotEmpty(t, respReg.AccessToken)
	assert.NotEmpty(t, respReg.RefreshToken)
	assert.NotEmpty(t, respReg.ExpiresIn)

	reqLog := &pb.LoginRequest{
		Email:    "newtestemail@test.com",
		Password: "passwordvalue125677",
	}

	respLog, err := authManager.Login(ctx, reqLog)
	assert.Empty(t, respLog)
	require.Error(t, err)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	assert.Equal(t, "invalid credentials", st.Message())
}
func TestAuthGetUserByEmail_Success(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "auth")
	defer env.Cleanup(t)

	repo := repository.NewPostgresStorage(env.DB)
	ctx := context.Background()

	expectedEmail := "success@test.com"
	passHash := []byte("fake_hash_for_test")
	userID, err := repo.CreateUser(ctx, expectedEmail, "Success User", "user", passHash)
	require.NoError(t, err)
	require.NotEqual(t, uuid.Nil, userID)

	usr, err := repo.GetUserByEmail(ctx, expectedEmail)
	require.NoError(t, err)

	assert.Equal(t, userID, usr.ID)
	assert.Equal(t, expectedEmail, usr.Email)
	assert.Equal(t, passHash, usr.Password)
}

func TestAuthGetUserByID_Success(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "auth")
	defer env.Cleanup(t)

	repo := repository.NewPostgresStorage(env.DB)
	ctx := context.Background()

	userID, err := repo.CreateUser(ctx, "idtest@test.com", "ID Test User", "user", []byte("hash"))
	require.NoError(t, err)

	usr, err := repo.GetUserByID(ctx, userID)
	require.NoError(t, err)
	assert.Equal(t, userID, usr.ID)
	assert.Equal(t, "idtest@test.com", usr.Email)
}

func TestAuthGetUserByID_NotFound(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "auth")
	defer env.Cleanup(t)

	repo := repository.NewPostgresStorage(env.DB)
	ctx := context.Background()

	fakeUserID := uuid.New()

	result, err := repo.GetUserByID(ctx, fakeUserID)

	require.Error(t, err)
	assert.Contains(t, err.Error(), "user not found by ID:")
	assert.ErrorIs(t, err, sql.ErrNoRows)

	assert.Equal(t, uuid.Nil, result.ID)
	assert.Empty(t, result.Email)
	assert.Empty(t, result.Password)

}

func TestAuthRefreshToken(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "auth")
	defer env.Cleanup(t)

	repo := repository.NewPostgresStorage(env.DB)
	logger := slog.New(slog.DiscardHandler)
	authManager := auth.NewAuthManager(repo, logger, "test-secret", 1*time.Second, 7*24*time.Hour)

	ctx := context.Background()

	t.Run("InvalidRefreshToken", func(t *testing.T) {

		reqReg := &pb.RegisterRequest{
			Email:    "testemail@test.com",
			Password: "passwordvalue125677",
			Name:     "test user",
		}

		respReg, err := authManager.Register(ctx, reqReg)
		require.NoError(t, err)
		assert.NotEmpty(t, respReg.UserId)
		assert.NotEmpty(t, respReg.AccessToken)
		assert.NotEmpty(t, respReg.RefreshToken)
		assert.NotEmpty(t, respReg.ExpiresIn)

		reqRefresh := &pb.RefreshTokenRequest{
			RefreshToken: "asdfdfsfdsfsdf",
		}
		respRefresh, err := authManager.RefreshToken(ctx, reqRefresh)
		assert.Empty(t, respRefresh)
		require.Error(t, err)

		st, ok := status.FromError(err)
		require.True(t, ok)
		assert.Equal(t, codes.Unauthenticated, st.Code())
		assert.Contains(t, st.Message(), "invalid refresh token")
	})

	t.Run("CreateNewAccessToken", func(t *testing.T) {
		reqReg := &pb.RegisterRequest{
			Email:    "testemail2@test.com",
			Password: "passwordvalue125677",
			Name:     "test user",
		}

		respReg, err := authManager.Register(ctx, reqReg)
		require.NoError(t, err)
		assert.NotEmpty(t, respReg.UserId)
		assert.NotEmpty(t, respReg.AccessToken)
		assert.NotEmpty(t, respReg.RefreshToken)
		assert.NotEmpty(t, respReg.ExpiresIn)

		time.Sleep(2 * time.Second)
		reqRefresh := &pb.RefreshTokenRequest{
			RefreshToken: respReg.RefreshToken,
		}
		respRefresh, err := authManager.RefreshToken(ctx, reqRefresh)
		require.NoError(t, err)
		assert.NotEmpty(t, respRefresh.AccessToken)
		assert.NotEqual(t, respRefresh.AccessToken, respReg.AccessToken)
	})
}

func TestAuthIsRefreshTokenValid_Flows(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "auth")
	defer env.Cleanup(t)

	repo := repository.NewPostgresStorage(env.DB)
	ctx := context.Background()

	userID, err := repo.CreateUser(ctx, "tokenvalid@test.com", "Token User", "user", []byte("hash"))
	require.NoError(t, err)

	tokenID := "valid-token-123"

	err = repo.SaveRefreshToken(ctx, userID, tokenID, 24*time.Hour)
	require.NoError(t, err)

	isValid, err := repo.IsRefreshTokenValid(ctx, tokenID, userID)
	require.NoError(t, err)
	assert.True(t, isValid)

	err = repo.RevokeRefreshToken(ctx, tokenID, userID)
	require.NoError(t, err)

	isValid, err = repo.IsRefreshTokenValid(ctx, tokenID, userID)
	require.NoError(t, err)
	assert.False(t, isValid)

	expiredTokenID := "expired-token-456"
	err = repo.SaveRefreshToken(ctx, userID, expiredTokenID, -1*time.Hour) // Истек час назад
	require.NoError(t, err)

	isValid, err = repo.IsRefreshTokenValid(ctx, expiredTokenID, userID)
	require.NoError(t, err)
	assert.False(t, isValid)
}

func TestAuthRefreshToken_FullFlow(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "auth")
	defer env.Cleanup(t)

	repo := repository.NewPostgresStorage(env.DB)
	logger := slog.New(slog.DiscardHandler)
	authManager := auth.NewAuthManager(repo, logger, "test-secret", 2*time.Second, 7*24*time.Hour)

	ctx := context.Background()

	reqReg := &pb.RegisterRequest{
		Email:    "refresh@test.com",
		Password: "testpassword123",
		Name:     "Test User",
	}
	respReg, err := authManager.Register(ctx, reqReg)
	require.NoError(t, err)

	oldAccessToken := respReg.AccessToken

	time.Sleep(3 * time.Second)

	reqRefresh := &pb.RefreshTokenRequest{
		RefreshToken: respReg.RefreshToken,
	}
	respRefresh, err := authManager.RefreshToken(ctx, reqRefresh)
	require.NoError(t, err)
	assert.NotEmpty(t, respRefresh.AccessToken)
	assert.NotEqual(t, oldAccessToken, respRefresh.AccessToken)
}

func TestAuthValidateToken_FullFlow(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "auth")
	defer env.Cleanup(t)

	repo := repository.NewPostgresStorage(env.DB)
	logger := slog.New(slog.DiscardHandler)
	authManager := auth.NewAuthManager(repo, logger, "test-secret", 2*time.Second, 7*24*time.Hour)

	ctx := context.Background()

	reqReg := &pb.RegisterRequest{
		Email:    "validate@test.com",
		Password: "testpassword1233112",
		Name:     "Test User",
	}
	respReg, err := authManager.Register(ctx, reqReg)
	require.NoError(t, err)

	reqVal := &pb.ValidateTokenRequest{Token: respReg.AccessToken}
	respVal, err := authManager.ValidateToken(ctx, reqVal)

	require.NoError(t, err)
	assert.True(t, respVal.IsValid)
	assert.Equal(t, respReg.UserId, respVal.UserId)
	assert.Equal(t, "validate@test.com", respVal.Email)
	assert.Equal(t, "user", respVal.Role)

	oldAccessToken := respReg.AccessToken
	validateReq := &pb.ValidateTokenRequest{
		Token: respReg.AccessToken,
	}
	respValidate, err := authManager.ValidateToken(ctx, validateReq)
	require.NoError(t, err)
	assert.True(t, respValidate.IsValid)
	time.Sleep(5 * time.Second)

	validateReq = &pb.ValidateTokenRequest{
		Token: oldAccessToken,
	}
	respValidate, err = authManager.ValidateToken(ctx, validateReq)
	require.Error(t, err)
	assert.False(t, respValidate.IsValid)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	assert.Equal(t, "invalid token", st.Message())

	authManager.Logout(ctx, &pb.LogoutRequest{RefreshToken: respReg.RefreshToken})

	validateReq = &pb.ValidateTokenRequest{
		Token: respReg.RefreshToken,
	}
	respValidate, err = authManager.ValidateToken(ctx, validateReq)
	require.Error(t, err)
	assert.False(t, respValidate.IsValid)
	st, ok = status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	assert.Equal(t, "invalid token", st.Message())
}

func TestAuthValidateToken_WrongSecret(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "auth")
	defer env.Cleanup(t)

	repo := repository.NewPostgresStorage(env.DB)
	logger := slog.New(slog.DiscardHandler)
	manager := auth.NewAuthManager(repo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)
	ctx := context.Background()

	_, err := repo.CreateUser(ctx, "wrongsecret@test.com", "Test", "user", []byte("hash"))
	require.NoError(t, err)

	claims := &jwtTokens.AccessClaims{
		TokenUse: "access",
		UserID:   uuid.New().String(),
		Email:    "wrongsecret@test.com",
		Role:     "user",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(15 * time.Minute)),
			Issuer:    "eventix",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	badToken, _ := token.SignedString([]byte("wrong-secret"))

	req := &pb.ValidateTokenRequest{Token: badToken}
	resp, err := manager.ValidateToken(ctx, req)

	require.Error(t, err)
	assert.False(t, resp.IsValid)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
}

func TestAuthValidateToken_RefreshTokenNotInDB(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "auth")
	defer env.Cleanup(t)

	repo := repository.NewPostgresStorage(env.DB)
	logger := slog.New(slog.DiscardHandler)
	manager := auth.NewAuthManager(repo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)
	ctx := context.Background()

	userID, err := repo.CreateUser(ctx, "notindb@test.com", "Test", "user", []byte("hash"))
	require.NoError(t, err)

	tokenID := "ghost-token-123"
	claims := &jwtTokens.RefreshClaims{
		TokenUse: "refresh",
		UserID:   userID.String(),
		TokenID:  tokenID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(24 * time.Hour)),
			Issuer:    "eventix",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	refreshToken, _ := token.SignedString([]byte("test-secret"))

	req := &pb.ValidateTokenRequest{Token: refreshToken}
	resp, err := manager.ValidateToken(ctx, req)

	require.Error(t, err)
	assert.False(t, resp.IsValid)
	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.Unauthenticated, st.Code())
	assert.Equal(t, "invalid token", st.Message())
}

func TestAuthGetMe_Success(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "auth")
	defer env.Cleanup(t)

	repo := repository.NewPostgresStorage(env.DB)
	logger := slog.New(slog.DiscardHandler)
	authManager := auth.NewAuthManager(repo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)

	ctxReq := context.Background()
	email := "test@example.com"
	password := "testintegrationpassword135"
	name := "username"

	registerReq := &pb.RegisterRequest{
		Email:    email,
		Password: password,
		Name:     name,
	}

	registerResp, err := authManager.Register(ctxReq, registerReq)
	require.NoError(t, err)
	require.NotNil(t, registerResp)

	userID, err := uuid.Parse(registerResp.UserId)
	require.NoError(t, err)
	ctxReq = authctx.WithUserID(ctxReq, userID)
	getMeResp, err := authManager.GetMe(ctxReq, &pb.GetMeRequest{})
	require.NoError(t, err)
	require.NotNil(t, getMeResp)

	assert.Equal(t, registerResp.UserId, getMeResp.UserId)
	assert.Equal(t, email, getMeResp.Email)
	assert.Equal(t, name, getMeResp.Name)
	assert.Equal(t, "user", getMeResp.Role)

	createdAt, err := time.Parse(time.RFC3339, getMeResp.CreatedAt)
	require.NoError(t, err)
	assert.False(t, createdAt.IsZero())
}

func TestAuthGetMe_Errors(t *testing.T) {
	env := helpers.SetupTestEnvironment(t, "auth")
	defer env.Cleanup(t)

	repo := repository.NewPostgresStorage(env.DB)
	logger := slog.New(slog.DiscardHandler)
	authManager := auth.NewAuthManager(repo, logger, "test-secret", 15*time.Minute, 7*24*time.Hour)

	ctxReq := context.Background()

	ctxReq = authctx.WithUserID(ctxReq, uuid.New())
	resp, err := authManager.GetMe(ctxReq, &pb.GetMeRequest{})
	require.Error(t, err)
	require.Nil(t, resp)

	st, ok := status.FromError(err)
	require.True(t, ok)
	assert.Equal(t, codes.NotFound, st.Code())
	assert.Equal(t, "user not found", st.Message())
}
