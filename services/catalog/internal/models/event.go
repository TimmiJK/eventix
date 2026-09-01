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
	Price          float64
	CreatedAt      time.Time
	UpdatedAt      time.Time
}
