package auth

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log/slog"
	"regexp"
	"strings"
	"time"

	jwtTokens "eventix/pkg/jwt"
	pb "eventix/proto/auth/pb"

	"eventix/services/auth/internal/models"

	"github.com/google/uuid"
	"golang.org/x/crypto/argon2"
	"google.golang.org/grpc/codes"
	"google.golang.org/grpc/status"
)

type AuthManager struct {
	repo                   Repository
	logger                 *slog.Logger
	jwtSecret              string
	accessTokenExperation  time.Duration
	refreshTokenExperation time.Duration
	pb.UnimplementedAuthServiceServer
}

type AuthTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
}

type Repository interface {
	CreateUser(ctx context.Context, email string, name string, role string, password []byte) (uuid.UUID, error)
	GetUserByEmail(ctx context.Context, email string) (models.User, error)
	GetUserByID(ctx context.Context, userID uuid.UUID) (models.User, error)
	SaveRefreshToken(ctx context.Context, userID uuid.UUID, tokenID string, tokenExpTime time.Duration) error
	IsRefreshTokenValid(ctx context.Context, tokenID string, userID uuid.UUID) (bool, error)
	RevokeRefreshToken(ctx context.Context, tokenID string, userID uuid.UUID) error
}

type Argon2Params struct {
	Memory      uint32
	Iterations  uint32
	Parallelism uint8
	SaltLength  uint32
	KeyLength   uint32
}

var defaultArgon2Params = Argon2Params{
	Memory:      64 * 1024,
	Iterations:  1,
	Parallelism: 4,
	SaltLength:  8,
	KeyLength:   32,
}

func NewAuthManager(repo Repository, logger *slog.Logger, secret string, accessTokenExp time.Duration, refreshTokenExp time.Duration) *AuthManager {
	return &AuthManager{
		repo:                   repo,
		logger:                 logger,
		jwtSecret:              secret,
		accessTokenExperation:  accessTokenExp,
		refreshTokenExperation: refreshTokenExp,
	}
}

func generateTokenID() string {
	b := make([]byte, 16)
	rand.Read(b)
	return hex.EncodeToString(b)
}

func (m *AuthManager) createToken(ctx context.Context, userID uuid.UUID, email string, role string) (*AuthTokens, error) {
	accessToken, err := jwtTokens.CreateAccessToken(userID, email, role, m.jwtSecret, m.accessTokenExperation)
	if err != nil {
		return nil, fmt.Errorf("failed to create access token: %w", err)
	}

	tokenID := generateTokenID()
	refreshToken, err := jwtTokens.CreateRefreshToken(userID, tokenID, m.jwtSecret, m.refreshTokenExperation)
	if err != nil {
		return nil, fmt.Errorf("failed to create refresh token: %w", err)
	}

	err = m.repo.SaveRefreshToken(ctx, userID, tokenID, m.refreshTokenExperation)
	if err != nil {
		return nil, fmt.Errorf("failed to save refresh token: %w", err)
	}

	return &AuthTokens{
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
		ExpiresIn:    int64(m.accessTokenExperation.Seconds()),
	}, nil
}

func hashPass(password string, params Argon2Params) ([]byte, error) {
	salt := make([]byte, params.SaltLength)
	if _, err := rand.Read(salt); err != nil {
		return nil, err
	}

	hashedPass := argon2.IDKey([]byte(password), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)
	result := append(salt, hashedPass...)
	return result, nil
}

func verifyPass(correctPassword []byte, passwordToVerify string, params Argon2Params) bool {
	salt := correctPassword[0:params.SaltLength]
	hashedPass := argon2.IDKey([]byte(passwordToVerify), salt, params.Iterations, params.Memory, params.Parallelism, params.KeyLength)
	storedHash := correctPassword[params.SaltLength:]
	return bytes.Equal(hashedPass, storedHash)
}

var emailRegex = regexp.MustCompile(`^[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}$`)

func isValidEmail(email string) bool {
	return emailRegex.MatchString(email)
}

func (m *AuthManager) Register(ctx context.Context, req *pb.RegisterRequest) (*pb.RegisterResponse, error) {
	m.logger.Info("Registration attempt", "email", req.Email)

	if req.Email == "" || !isValidEmail(req.Email) {
		m.logger.Warn("Registration failed: invalid email format", "email", req.Email)
		return nil, status.Error(codes.InvalidArgument, "invalid email format")
	}

	if req.Password == "" || len(req.Password) < 10 {
		m.logger.Warn("Registration failed: weak password", "email", req.Email)
		return nil, status.Error(codes.InvalidArgument, "password must be at least 10 characters")
	}

	if req.Name == "" {
		m.logger.Warn("Registration failed: empty name", "email", req.Email)
		return nil, status.Error(codes.InvalidArgument, "name is required")
	}

	pass, err := hashPass(req.Password, defaultArgon2Params)
	if err != nil {
		m.logger.Error("Failed to hash password", "email", req.Email, "error", err)
		return nil, status.Errorf(codes.Internal, "failed to hash password: %v", err)
	}

	id, err := m.repo.CreateUser(ctx, req.Email, req.Name, "user", pass)
	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			m.logger.Warn("Registration failed: email already exists", "email", req.Email)
			return nil, status.Error(codes.AlreadyExists, "email already registered")
		}
		m.logger.Error("Registration failed: database error", "email", req.Email, "error", err)
		return nil, status.Errorf(codes.Internal, "failed to create user: %v", err)
	}

	tokens, err := m.createToken(ctx, id, req.Email, "user")
	if err != nil {
		m.logger.Error("Failed to create tokens after registration", "user_id", id.String(), "error", err)
		return nil, status.Errorf(codes.Internal, "failed to create tokens: %v", err)
	}

	m.logger.Info("Registration successful", "user_id", id.String(), "email", req.Email)
	return &pb.RegisterResponse{
		UserId:       id.String(),
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresIn:    tokens.ExpiresIn,
	}, nil
}

func (m *AuthManager) Login(ctx context.Context, req *pb.LoginRequest) (*pb.LoginResponse, error) {
	m.logger.Info("Login attempt", "email", req.Email)
	if req.Email == "" || req.Password == "" {
		return nil, status.Error(codes.InvalidArgument, "email and password are required")
	}

	usr, err := m.repo.GetUserByEmail(ctx, req.Email)
	if err != nil {
		m.logger.Warn("Login failed: user not found", "email", req.Email)
		return nil, status.Error(codes.InvalidArgument, "invalid credentials")
	}

	if !verifyPass(usr.Password, req.Password, defaultArgon2Params) {
		m.logger.Warn("Login failed: invalid password", "email", req.Email)
		return nil, status.Error(codes.Unauthenticated, "invalid credentials")
	}

	tokens, err := m.createToken(ctx, usr.ID, usr.Email, "user")
	if err != nil {
		m.logger.Error("Login failed: token creation error", "user_id", usr.ID.String(), "error", err)
		return nil, status.Errorf(codes.Internal, "failed to create tokens: %v", err)
	}

	m.logger.Info("Login successful", "user_id", usr.ID.String(), "email", req.Email)
	return &pb.LoginResponse{
		UserId:       usr.ID.String(),
		AccessToken:  tokens.AccessToken,
		RefreshToken: tokens.RefreshToken,
		ExpiresIn:    tokens.ExpiresIn,
	}, nil
}

func (m *AuthManager) RefreshToken(ctx context.Context, req *pb.RefreshTokenRequest) (*pb.RefreshTokenResponse, error) {
	claims, err := jwtTokens.ValidateRefreshToken(req.RefreshToken, m.jwtSecret)
	if err != nil {
		m.logger.Warn("Refresh token validation failed", "error", err)
		return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		m.logger.Error("Failed to parse user ID from token", "error", err)
		return nil, status.Error(codes.Internal, "invalid user id")
	}

	isValid, err := m.repo.IsRefreshTokenValid(ctx, claims.TokenID, userID)
	if err != nil || !isValid {
		m.logger.Warn("Refresh token is revoked or invalid", "user_id", userID.String(), "token_id", claims.TokenID)
		return nil, status.Error(codes.Unauthenticated, "refresh token has been revoked")
	}

	user, err := m.repo.GetUserByID(ctx, userID)
	if err != nil {
		m.logger.Error("Failed to get user by ID during refresh", "user_id", userID.String(), "error", err)
		return nil, status.Error(codes.Internal, "user not found")
	}

	accessToken, err := jwtTokens.CreateAccessToken(
		user.ID,
		user.Email,
		"user",
		m.jwtSecret,
		m.accessTokenExperation,
	)
	if err != nil {
		m.logger.Error("Failed to create new access token", "user_id", userID.String(), "error", err)
		return nil, status.Errorf(codes.Internal, "failed to create access token: %v", err)
	}

	m.logger.Info("Access token refreshed successfully", "user_id", userID.String())
	return &pb.RefreshTokenResponse{
		AccessToken: accessToken,
		ExpiresIn:   int64(m.accessTokenExperation.Seconds()),
	}, nil

}

func (m *AuthManager) Logout(ctx context.Context, req *pb.LogoutRequest) (*pb.LogoutResponse, error) {
	claims, err := jwtTokens.ValidateRefreshToken(req.RefreshToken, m.jwtSecret)
	if err != nil {
		m.logger.Warn("Logout failed: invalid refresh token", "error", err)
		return nil, status.Error(codes.Unauthenticated, "invalid refresh token")
	}

	userID, err := uuid.Parse(claims.UserID)
	if err != nil {
		m.logger.Error("Logout failed: invalid user ID in token", "error", err)
		return nil, status.Error(codes.Internal, "invalid user id")
	}

	err = m.repo.RevokeRefreshToken(ctx, claims.TokenID, userID)
	if err != nil {
		m.logger.Error("Logout failed: database error while revoking token", "user_id", userID.String(), "error", err)
		return nil, status.Errorf(codes.Internal, "failed to revoke token: %v", err)
	}

	m.logger.Info("Logout successful", "user_id", userID.String())
	return &pb.LogoutResponse{Success: true}, nil
}
