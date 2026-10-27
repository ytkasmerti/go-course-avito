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
}

type IdempotencyRecord struct {
	TripID      uuid.UUID
	RequestHash string
}
