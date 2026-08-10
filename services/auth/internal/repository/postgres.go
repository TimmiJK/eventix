package repository

import (
	"context"
	"database/sql"
	"fmt"
	"strings"
	"time"

	"eventix/services/auth/internal/models"

	"github.com/google/uuid"
	//pgx "github.com/jackc/pgx/v5"
)

type PostgresStorage struct {
	db *sql.DB
}

func NewPostgresStorage(db *sql.DB) *PostgresStorage {
	return &PostgresStorage{db: db}
}

func (ps *PostgresStorage) CreateUser(ctx context.Context, email string, name string, role string, password []byte) (uuid.UUID, error) {
	var id uuid.UUID

	err := ps.db.QueryRowContext(
		ctx,
		`
		INSERT INTO users(email, password_hash, name, role) 
		VALUES($1, $2, $3, $4) 
		RETURNING id
		`,
		email,
		password,
		name,
		role,
	).Scan(&id)

	if err != nil {
		if strings.Contains(err.Error(), "duplicate key value violates unique constraint") {
			return uuid.UUID{}, fmt.Errorf("email already exists: %w", err)
		}
		return uuid.UUID{}, fmt.Errorf("db create error: %w", err)
	}

	return id, nil
}

func (ps *PostgresStorage) GetUserByEmail(ctx context.Context, email string) (models.User, error) {
	usr := models.User{}

	err := ps.db.QueryRowContext(
		ctx,
		`SELECT id, email, password_hash, role 
		FROM users 
		WHERE email = $1`,
		email,
	).Scan(
		&usr.ID,
		&usr.Email,
		&usr.Password,
		&usr.Role,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return models.User{}, fmt.Errorf("user not found by email: %w", err)
		}
		return models.User{}, err
	}

	return usr, nil

}

func (ps *PostgresStorage) GetUserByID(ctx context.Context, userID uuid.UUID) (models.User, error) {
	usr := models.User{}

	err := ps.db.QueryRowContext(
		ctx,
		`SELECT id, email, password_hash, role 
		FROM users 
		WHERE id = $1`,
		userID,
	).Scan(
		&usr.ID,
		&usr.Email,
		&usr.Password,
		&usr.Role,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return models.User{}, fmt.Errorf("user not found by ID: %w", err)
		}
		return models.User{}, err
	}

	return usr, nil
}

func (ps *PostgresStorage) SaveRefreshToken(ctx context.Context, userID uuid.UUID, tokenID string, tokenExpTime time.Duration) error {
	expiresAt := time.Now().Add(tokenExpTime)
	_, err := ps.db.ExecContext(
		ctx,
		`
		INSERT INTO refresh_tokens(user_id, token_hash, expires_at) 
		VALUES($1, $2, $3)
		`,
		userID,
		tokenID,
		expiresAt,
	)

	if err != nil {
		if strings.Contains(err.Error(), "duplicate key value violates unique constraint") {
			return fmt.Errorf("email already exists: %w", err)
		}
		return fmt.Errorf("db create error: %w", err)
	}
	return nil
}

func (ps *PostgresStorage) IsRefreshTokenValid(ctx context.Context, tokenID string, userID uuid.UUID) (bool, error) {
	var exists bool
	err := ps.db.QueryRowContext(
		ctx,
		`SELECT EXISTS(
			SELECT 1 FROM refresh_tokens 
            WHERE token_hash = $1 
            AND user_id = $2 
            AND expires_at > NOW() 
            AND revoked_at IS NULL
		)`,
		tokenID, userID,
	).Scan(&exists)

	return exists, err
}

func (ps *PostgresStorage) RevokeRefreshToken(ctx context.Context, tokenID string, userID uuid.UUID) error {
	_, err := ps.db.ExecContext(ctx,
		`
		UPDATE refresh_tokens 
        SET revoked_at = NOW() 
        WHERE token_hash = $1 AND user_id = $2
		 `,
		tokenID, userID,
	)

	return err
}
