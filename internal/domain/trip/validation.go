package trip

import (
	"fmt"
	"math"
	"time"

	"github.com/google/uuid"
)

const (
	maxLatitude       = 90
	maxLongitude      = 180
	maxRecordedAtSkew = time.Minute
)

// Данные для создания поездки, правила допустимости живут здесь чтобы транспорт не мог их обойти
type NewTripParams struct {
	UserID         uuid.UUID
	DriverID       uuid.UUID
	StartLatitude  float64
	StartLongitude float64
	EndLatitude    float64
	EndLongitude   float64
	Price          int64
}

func (p NewTripParams) Validate() error {
	if p.UserID == uuid.Nil {
		return &ValidationError{Field: "user_id", Message: "must not be a zero UUID"}
	}
	if p.DriverID == uuid.Nil {
		return &ValidationError{Field: "driver_id", Message: "must not be a zero UUID"}
	}
	if err := validateCoordinate("start_point.latitude", p.StartLatitude, maxLatitude); err != nil {
		return err
	}
	if err := validateCoordinate("start_point.longitude", p.StartLongitude, maxLongitude); err != nil {
		return err
	}
	if err := validateCoordinate("end_point.latitude", p.EndLatitude, maxLatitude); err != nil {
		return err
	}
	if err := validateCoordinate("end_point.longitude", p.EndLongitude, maxLongitude); err != nil {
		return err
	}
	if p.Price < 0 {
		return &ValidationError{Field: "price", Message: "must be >= 0"}
	}
	return nil
}

func validateCoordinate(field string, v, limit float64) error {
	if math.IsNaN(v) || v < -limit || v > limit {
		return &ValidationError{Field: field, Message: fmt.Sprintf("must be in [-%g, %g]", limit, limit)}
	}
	return nil
}

// Данные новой координаты
type NewPositionParams struct {
	Latitude   float64
	Longitude  float64
	RecordedAt time.Time
}

func (p NewPositionParams) Validate(now time.Time) error {
	if err := validateCoordinate("coordinates.latitude", p.Latitude, maxLatitude); err != nil {
		return err
	}
	if err := validateCoordinate("coordinates.longitude", p.Longitude, maxLongitude); err != nil {
		return err
	}
	if p.RecordedAt.IsZero() {
		return &ValidationError{Field: "recorded_at", Message: "required"}
	}
	if p.RecordedAt.After(now.Add(maxRecordedAtSkew)) {
		return &ValidationError{Field: "recorded_at", Message: fmt.Sprintf("must not be more than %s in the future", maxRecordedAtSkew)}
	}
	return nil
}
