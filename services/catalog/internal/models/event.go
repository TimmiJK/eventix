package models

import (
	"time"

	"github.com/google/uuid"
)

type Event struct {
	ID             uuid.UUID
	Title          string
	Description    string
	Venue          string
	EventDate      time.Time
	TotalSeats     int32
	AvailableSeats int32
	PriceMinor     int64
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Currency       string
	Version        int64
}

type EventPatch struct {
	Title       *string
	Description *string
	Venue       *string
	EventDate   *time.Time
	TotalSeats  *int32
	PriceMinor  *int64
	Currency    *string
	Version     *int64
}
