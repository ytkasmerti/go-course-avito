package trip

import (
	"fmt"
	"math"

	"github.com/google/uuid"
)

const (
	maxLatitude  = 90
	maxLongitude = 180
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

// Правила допустимости живут в домене, чтобы их не обошел другой транспорт
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
