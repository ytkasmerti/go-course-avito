package trip

import (
	"errors"
	"time"
	"github.com/google/uuid"
)

type Status string

const (
	StatusActive    Status = "active"
	StatusCompleted Status = "completed"
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
	Status         Status
	StartedAt      time.Time
	FinishedAt     time.Time
}

var (
	ErrTripNotFound   = errors.New("Trip not found")
	ErrDriverBusy     = errors.New("Driver already has an active trip")
	ErrTripCompleted  = errors.New("Trip already completed")
	ErrInvalidRequest = errors.New("Invalid request")
)