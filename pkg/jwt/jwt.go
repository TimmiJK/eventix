package jwtTokens

import (
	"fmt"
	"strings"
	"time"

	"github.com/golang-jwt/jwt/v5"
	"github.com/google/uuid"
)

type JwtToken struct {
	Secret []byte
}

func NewJwtToken(secret string) (*JwtToken, error) {
	return &JwtToken{Secret: []byte(secret)}, nil
}

type AccessClaims struct {
	UserID   string
	Email    string
	Role     string
	TokenUse string
	jwt.RegisteredClaims
}

type RefreshClaims struct {
	UserID   string
	TokenID  string
	TokenUse string
	jwt.RegisteredClaims
}

func CreateAccessToken(userID uuid.UUID, email string, role string, secret string, tokenExpTime time.Duration) (string, error) {
	claims := &AccessClaims{
		UserID:   userID.String(),
		Email:    email,
		Role:     role,
		TokenUse: "access",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(tokenExpTime)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "eventix",
		},
	}
	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func CreateRefreshToken(userID uuid.UUID, tokenID string, secret string, tokenExpTime time.Duration) (string, error) {
	claims := &RefreshClaims{
		UserID:   userID.String(),
		TokenID:  tokenID,
		TokenUse: "refresh",
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(tokenExpTime)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "eventix",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func parseToken(raw, secret string, claims jwt.Claims) error {
	if secret == "" {
		return fmt.Errorf("JWT secret is required")
	}

	token, err := jwt.ParseWithClaims(
		raw,
		claims,
		func(_ *jwt.Token) (any, error) {
			return []byte(secret), nil
		},
		jwt.WithValidMethods([]string{"HS256"}),
		jwt.WithIssuer("eventix"),
		jwt.WithExpirationRequired(),
	)
	if err != nil {
		return err
	}
	if !token.Valid {
		return jwt.ErrTokenInvalidClaims
	}
	return nil
}

func ValidateAccessToken(accessToken string, secret string) (*AccessClaims, error) {
	claims := &AccessClaims{}
	if err := parseToken(accessToken, secret, claims); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(claims.UserID)
	if err != nil || id == uuid.Nil || claims.TokenUse != "access" || claims.Email == "" || claims.Role == "" {
		return nil, jwt.ErrTokenInvalidClaims
	}

	return claims, nil
}

func ValidateRefreshToken(refreshToken string, secret string) (*RefreshClaims, error) {
	claims := &RefreshClaims{}
	if err := parseToken(refreshToken, secret, claims); err != nil {
		return nil, err
	}

	id, err := uuid.Parse(claims.UserID)
	if err != nil || id == uuid.Nil || claims.TokenUse != "refresh" || strings.TrimSpace(claims.TokenID) == "" {
		return nil, jwt.ErrTokenInvalidClaims
	}

	return claims, nil
}
