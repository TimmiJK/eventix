package repository

import (
	"context"
	"database/sql"
	"errors"
	"eventix/services/catalog/internal/models"
	"fmt"
	"regexp"
	"strings"

	"github.com/google/uuid"
	"github.com/lib/pq"
)

type Repository interface {
	GetEventByID(ctx context.Context, eventID uuid.UUID) (models.Event, error)
	GetEvents(ctx context.Context, page int32, pageSize int32, searchQuery string) ([]models.Event, int32, error)
	CreateEvent(ctx context.Context, event *models.Event, eventOwnerID uuid.UUID) (models.Event, error)
	UpdateEvent(ctx context.Context, id uuid.UUID, event *models.EventPatch, expectedVersion int64, userID uuid.UUID, roleAdmin bool) (models.Event, error)
	DeleteEvent(ctx context.Context, eventID uuid.UUID, expectedVersion int64, actorID uuid.UUID, isAdmin bool) error
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

var (
	ErrEventTitleTaken       = errors.New("event title is already taken")
	ErrEventVersionConflict  = errors.New("event version conflict")
	ErrEventNotEditable      = errors.New("event is not editable")
	ErrCapacityBelowOccupied = errors.New("capacity cannot be below occupied seats")
	ErrEventNotDeletable     = errors.New("event can't be deleted")
)

func (ps *PostgresStorage) GetEventByID(ctx context.Context, eventID uuid.UUID) (models.Event, error) {
	var event models.Event

	err := ps.db.QueryRowContext(
		ctx,
		`
		SELECT id, title, description, venue, event_date, total_seats, available_seats, price_minor, currency, created_at, updated_at, version 
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
		&event.PriceMinor,
		&event.Currency,
		&event.CreatedAt,
		&event.UpdatedAt,
		&event.Version,
	)

	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
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
		SELECT id, title, description, venue, event_date, total_seats, available_seats, price_minor, currency, created_at, updated_at, version 
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

	events := make([]models.Event, 0)
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
			&event.PriceMinor,
			&event.Currency,
			&event.CreatedAt,
			&event.UpdatedAt,
			&event.Version,
		)
		if err != nil {
			return nil, 0, fmt.Errorf("db scan error: %w", err)
		}
		events = append(events, event)
	}

	if err = rows.Err(); err != nil {
		return nil, 0, fmt.Errorf("db rows iteration error: %w", err)
	}

	return events, totalCount, nil
}

func (ps *PostgresStorage) CreateEvent(ctx context.Context, event *models.Event, eventOwnerID uuid.UUID) (models.Event, error) {
	var createdEvent models.Event

	err := ps.db.QueryRowContext(
		ctx,
		`
		INSERT INTO events(title, description, venue, event_date, total_seats, available_seats, price_minor, currency, organizer_id) 
		VALUES($1, $2, $3, $4, $5, $6, $7, $8, $9)
		RETURNING id, title, description, venue, event_date, total_seats, available_seats, price_minor, currency, created_at, updated_at, version
		`,
		event.Title,
		event.Description,
		event.Venue,
		event.EventDate,
		event.TotalSeats,
		event.AvailableSeats,
		event.PriceMinor,
		event.Currency,
		eventOwnerID,
	).Scan(
		&createdEvent.ID,
		&createdEvent.Title,
		&createdEvent.Description,
		&createdEvent.Venue,
		&createdEvent.EventDate,
		&createdEvent.TotalSeats,
		&createdEvent.AvailableSeats,
		&createdEvent.PriceMinor,
		&createdEvent.Currency,
		&createdEvent.CreatedAt,
		&createdEvent.UpdatedAt,
		&createdEvent.Version,
	)

	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23505" && pqErr.Constraint == "events_title_key" {
			return models.Event{}, fmt.Errorf("%w: %w", ErrEventTitleTaken, err)
		}
		return models.Event{}, fmt.Errorf("db create error: %w", err)
	}

	return createdEvent, nil
}

func (ps *PostgresStorage) UpdateEvent(ctx context.Context, eventID uuid.UUID, patch *models.EventPatch, expectedVersion int64, actorID uuid.UUID, isAdmin bool) (models.Event, error) {
	var updatedEvent models.Event

	err := ps.db.QueryRowContext(
		ctx,
		`
		UPDATE events
		SET title   = COALESCE($2::text, title),
    	description = COALESCE($3::text, description),
    	venue       = COALESCE($4::text, venue),
    	event_date  = COALESCE($5::timestamptz, event_date),
    	total_seats = COALESCE($6::integer, total_seats),
		available_seats = CASE
    		WHEN $6::integer IS NULL THEN available_seats
    		ELSE $6::integer - (total_seats - available_seats)
		END,
    	price_minor = COALESCE($7::bigint, price_minor),
		currency = COALESCE($8::text, currency),
    	version = version + 1,
    	updated_at  = NOW()
		WHERE id = $1
			AND version = $9
			AND ($11::boolean OR organizer_id = $10::uuid)
			AND status IN ('DRAFT', 'PUBLISHED')
		RETURNING id, title, description, venue, event_date, total_seats,
          available_seats, price_minor, currency, created_at, updated_at, version
		`,
		eventID,
		patch.Title,
		patch.Description,
		patch.Venue,
		patch.EventDate,
		patch.TotalSeats,
		patch.PriceMinor,
		patch.Currency,
		expectedVersion,
		actorID,
		isAdmin,
	).Scan(
		&updatedEvent.ID,
		&updatedEvent.Title,
		&updatedEvent.Description,
		&updatedEvent.Venue,
		&updatedEvent.EventDate,
		&updatedEvent.TotalSeats,
		&updatedEvent.AvailableSeats,
		&updatedEvent.PriceMinor,
		&updatedEvent.Currency,
		&updatedEvent.CreatedAt,
		&updatedEvent.UpdatedAt,
		&updatedEvent.Version,
	)

	if err != nil {
		var pqErr *pq.Error
		if errors.As(err, &pqErr) && pqErr.Code == "23514" && pqErr.Constraint == "events_seat_counts_valid" {
			return models.Event{}, ErrCapacityBelowOccupied
		}

		if errors.As(err, &pqErr) && pqErr.Code == "23505" && pqErr.Constraint == "events_title_key" {
			return models.Event{}, fmt.Errorf("%w: %w", ErrEventTitleTaken, err)
		}

		if errors.Is(err, sql.ErrNoRows) {
			var currentStatus string
			lookupErr := ps.db.QueryRowContext(ctx,
				`
			SELECT status
			FROM events
			WHERE id = $1 AND ($3::boolean OR organizer_id = $2::uuid)
			`,
				eventID,
				actorID,
				isAdmin,
			).Scan(&currentStatus)

			switch {
			case errors.Is(lookupErr, sql.ErrNoRows):
				return models.Event{}, sql.ErrNoRows
			case lookupErr != nil:
				return models.Event{}, fmt.Errorf("check event after update: %w", lookupErr)
			case currentStatus == "CANCELLED":
				return models.Event{}, ErrEventNotEditable
			default:
				return models.Event{}, ErrEventVersionConflict
			}
		}
		return models.Event{}, fmt.Errorf("db err update event: %w", err)
	}

	return updatedEvent, nil
}

func (ps *PostgresStorage) DeleteEvent(ctx context.Context, eventID uuid.UUID, expectedVersion int64, actorID uuid.UUID, isAdmin bool) error {
	result, err := ps.db.ExecContext(
		ctx,
		`
    	DELETE FROM events
    	WHERE id = $1
			AND version = $2
			AND ($4::boolean OR organizer_id = $3::uuid)
			AND status = 'DRAFT'
	  	`,
		eventID,
		expectedVersion,
		actorID,
		isAdmin,
	)

	if err != nil {
		return fmt.Errorf("db err delete event: %w", err)
	}

	rowsAffected, err := result.RowsAffected()
	if err != nil {
		return fmt.Errorf("failed to get rows affected: %w", err)
	}

	if rowsAffected == 0 {
		var currentStatus string
		lookupErr := ps.db.QueryRowContext(ctx,
			`
			SELECT status
			FROM events
			WHERE id = $1 AND ($3::boolean OR organizer_id = $2::uuid)
			`,
			eventID,
			actorID,
			isAdmin,
		).Scan(&currentStatus)

		switch {
		case errors.Is(lookupErr, sql.ErrNoRows):
			return sql.ErrNoRows
		case lookupErr != nil:
			return fmt.Errorf("check event after delete: %w", lookupErr)
		case currentStatus == "CANCELLED" || currentStatus == "PUBLISHED":
			return ErrEventNotDeletable
		default:
			return ErrEventVersionConflict
		}
	}

	return nil
}
