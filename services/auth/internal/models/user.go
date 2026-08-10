package models

import "github.com/google/uuid"

type User struct {
	ID       uuid.UUID
	Email    string
	Name     string
	Role     string
	Password []byte
}

type AuthTokens struct {
	AccessToken  string
	RefreshToken string
	ExpiresIn    int64
}
