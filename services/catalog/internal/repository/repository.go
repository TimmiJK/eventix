package repository

import (
	"context"
	"database/sql"
	"eventix/services/catalog/internal/models"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
)

type Repository interface {
	GetEventByID(ctx context.Context, eventID uuid.UUID) (models.Event, error)
	GetEvents(ctx context.Context, page int32, pageSize int32, searchQuery string) ([]models.Event, int32, error)
	CreateEvent(ctx context.Context, event *models.Event) (models.Event, error)
	UpdateEvent(ctx context.Context, event *models.Event) (models.Event, error)
	DeleteEvent(ctx context.Context, eventID uuid.UUID) error
}

type PostgresStorage struct {
	db *sql.DB
}

func NewPostgresStorage(db *sql.DB) *PostgresStorage {
	return &PostgresStorage{db: db}
}

func escapeILikePattern(s string) string {
	re := regexp.MustCompile(`([%_\\])`)
	return re.ReplaceAllString(s, `\$1`)
}

func (ps *PostgresStorage) GetEventByID(ctx context.Context, eventID uuid.UUID) (models.Event, error) {
	var event models.Event

	err := ps.db.QueryRowContext(
		ctx,
		`
		SELECT id, title, description, venue, event_date, total_seats, available_seats, price, created_at, updated_at 
		FROM events 
		WHERE id = $1
		`,
		eventID,
	).Scan(
		&event.ID,
		&event.Title,
		&event.Description,
		&event.Venue,
		&event.EventDate,
		&event.TotalSeats,
		&event.AvailableSeats,
		&event.Price,
		&event.CreatedAt,
		&event.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return models.Event{}, fmt.Errorf("event not found by ID: %w", err)
		}
		return models.Event{}, fmt.Errorf("db query error: %w", err)
	}

	return event, nil
}

func (ps *PostgresStorage) GetEvents(ctx context.Context, page int32, pageSize int32, searchQuery string) ([]models.Event, int32, error) {
	var whereClause string
	var countArgs []interface{}
	var dataArgs []interface{}
	if strings.TrimSpace(searchQuery) != "" {
		pattern := "%" + escapeILikePattern(strings.TrimSpace(searchQuery)) + "%"
		whereClause = " WHERE title ILIKE $1 OR description ILIKE $1 OR venue ILIKE $1"
		countArgs = []interface{}{pattern}
		dataArgs = []interface{}{pattern}
	}

	var totalCount int32
	countQuery := "SELECT COUNT(*) FROM events" + whereClause
	err := ps.db.QueryRowContext(ctx, countQuery, countArgs...).Scan(&totalCount)
	if err != nil {
		return nil, 0, fmt.Errorf("db count error: %w", err)
	}

	offset := (page - 1) * pageSize
	if offset < 0 {
		offset = 0
	}

	argIndex := 1
	if whereClause != "" {
		argIndex = 2
	}

	dataQuery := fmt.Sprintf(
		`
		SELECT id, title, description, venue, event_date, total_seats, available_seats, price, created_at, updated_at 
		FROM events%s 
		ORDER BY event_date DESC, id DESC 
		OFFSET $%d 
		LIMIT $%d
		`,
		whereClause, argIndex, argIndex+1,
	)

	dataArgs = append(dataArgs, offset, pageSize)

	rows, err := ps.db.QueryContext(ctx, dataQuery, dataArgs...)
	if err != nil {
		return nil, 0, fmt.Errorf("db query error: %w", err)
	}
	defer rows.Close()

	var events []models.Event
	for rows.Next() {
		var event models.Event
		err = rows.Scan(
			&event.ID,
			&event.Title,
			&event.Description,
			&event.Venue,
			&event.EventDate,
			&event.TotalSeats,
			&event.AvailableSeats,
			&event.Price,
			&event.CreatedAt,
			&event.UpdatedAt,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("db scan error: %w", err)
		}
		events = append(events, event)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("db rows iteration error: %w", err)
	}

	if events == nil {
		events = []models.Event{}
	}

	return events, totalCount, nil
}

func (ps *PostgresStorage) CreateEvent(ctx context.Context, event *models.Event) (models.Event, error) {
	var createdEvent models.Event

	err := ps.db.QueryRowContext(
		ctx,
		`
		INSERT INTO events(title, description, venue, event_date, total_seats, available_seats, price) 
		VALUES($1, $2, $3, $4, $5, $6, $7)
		RETURNING id, title, description, venue, event_date, total_seats, available_seats, price, created_at, updated_at
		`,
		event.Title,
		event.Description,
		event.Venue,
		event.EventDate,
		event.TotalSeats,
		event.AvailableSeats,
		event.Price,
	).Scan(
		&createdEvent.ID,
		&createdEvent.Title,
		&createdEvent.Description,
		&createdEvent.Venue,
		&createdEvent.EventDate,
		&createdEvent.TotalSeats,
		&createdEvent.AvailableSeats,
		&createdEvent.Price,
		&createdEvent.CreatedAt,
		&createdEvent.UpdatedAt,
	)

	if err != nil {
		if strings.Contains(err.Error(), "duplicate key") {
			return models.Event{}, fmt.Errorf("event already exists: %w", err)
		}
		return models.Event{}, fmt.Errorf("db create error: %w", err)
	}

	return createdEvent, nil
}

func (ps *PostgresStorage) UpdateEvent(ctx context.Context, event *models.Event) (models.Event, error) {
	var updatedEvent models.Event

	err := ps.db.QueryRowContext(
		ctx,
		`
		UPDATE events
		SET title = $1, description = $2, venue = $3, event_date = $4, total_seats = $5, price = $6, updated_at = NOW()
		WHERE id = $7
		RETURNING id, title, description, venue, event_date, total_seats, available_seats, price, created_at, updated_at
		`,
		event.Title,
		event.Description,
		event.Venue,
		event.EventDate,
		event.TotalSeats,
		event.Price,
		event.ID,
	).Scan(
		&updatedEvent.ID,
		&updatedEvent.Title,
		&updatedEvent.Description,
		&updatedEvent.Venue,
		&updatedEvent.EventDate,
		&updatedEvent.TotalSeats,
		&updatedEvent.AvailableSeats,
		&updatedEvent.Price,
		&updatedEvent.CreatedAt,
		&updatedEvent.UpdatedAt,
	)

	if err != nil {
		if err == sql.ErrNoRows {
			return models.Event{}, fmt.Errorf("event not found by ID: %w", err)
		}
		return models.Event{}, fmt.Errorf("db query error: %w", err)
	}

	return updatedEvent, nil
}

func (ps *PostgresStorage) DeleteEvent(ctx context.Context, eventID uuid.UUID) error {
	res, err := ps.db.ExecContext(ctx, `DELETE FROM events WHERE id = $1`, eventID)
	if err != nil {
		return fmt.Errorf("db delete error: %w", err)
	}

	rowsAffected, err := res.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		return sql.ErrNoRows
	}

	return nil
}
