package model

import (
	"time"

	"github.com/google/uuid"
)

type Trip struct {
	ID             uuid.UUID
	UserID         uuid.UUID
	DriverID       uuid.UUID
	StartLatitude  float64
	StartLongitude float64
	EndLatitude    float64
	EndLongitude   float64
	Price          int64
	Status         string
	StartedAt      time.Time
	FinishedAt     *time.Time
	LastPositionAt *time.Time
}

type IdempotencyRecord struct {
	TripID      uuid.UUID
	RequestHash string
}

type Position struct {
	ID         int64
	TripID     uuid.UUID
	Latitude   float64
	Longitude  float64
	RecordedAt time.Time
}

type StaleTrip struct {
	ID       uuid.UUID
	DriverID uuid.UUID
}
