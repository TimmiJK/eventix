package auth

import (
	"fmt"
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
	UserID string
	Email  string
	Role   string
	jwt.RegisteredClaims
}

type RefreshClaims struct {
	UserID  string
	TokenID string
	jwt.RegisteredClaims
}

func CreateAccessToken(userID uuid.UUID, email string, role string, secret string, tokenExpTime time.Duration) (string, error) {
	claims := &AccessClaims{
		UserID: userID.String(),
		Email:  email,
		Role:   role,
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
		UserID:  userID.String(),
		TokenID: tokenID,
		RegisteredClaims: jwt.RegisteredClaims{
			ExpiresAt: jwt.NewNumericDate(time.Now().Add(tokenExpTime)),
			IssuedAt:  jwt.NewNumericDate(time.Now()),
			Issuer:    "eventix",
		},
	}

	token := jwt.NewWithClaims(jwt.SigningMethodHS256, claims)
	return token.SignedString([]byte(secret))
}

func ValidateAccessToken(accessToken string, secret string) (*AccessClaims, error) {
	token, err := jwt.ParseWithClaims(accessToken, &AccessClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*AccessClaims)
	if !ok || !token.Valid {
		return nil, jwt.ErrTokenInvalidClaims
	}

	if claims.Email == "" || claims.Role == "" {
		return nil, fmt.Errorf("invalid access token: missing required claims")
	}

	if claims.UserID == "" {
		return nil, fmt.Errorf("invalid access token: missing user_id")
	}

	if _, err := uuid.Parse(claims.UserID); err != nil {
		return nil, fmt.Errorf("invalid access token: invalid user_id format")
	}

	return claims, nil
}

func ValidateRefreshToken(refreshToken string, secret string) (*RefreshClaims, error) {
	token, err := jwt.ParseWithClaims(refreshToken, &RefreshClaims{}, func(token *jwt.Token) (interface{}, error) {
		return []byte(secret), nil
	})

	if err != nil {
		return nil, err
	}

	claims, ok := token.Claims.(*RefreshClaims)
	if !ok || !token.Valid {
		return nil, jwt.ErrTokenInvalidClaims
	}

	if claims.UserID == "" {
		return nil, fmt.Errorf("invalid refresh token: missing user_id")
	}

	if _, err := uuid.Parse(claims.UserID); err != nil {
		return nil, fmt.Errorf("invalid refresh token: invalid user_id format")
	}

	return claims, nil
}
