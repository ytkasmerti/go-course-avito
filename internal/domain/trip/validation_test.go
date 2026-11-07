package trip_test

import (
	"math"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/ytkasmerti/go-course-avito/internal/domain/trip"
)

const (
	fieldStartLatitude = "start_point.latitude"
	fieldEndLongitude  = "end_point.longitude"
)

func validParams() trip.NewTripParams {
	return trip.NewTripParams{
		UserID: uuid.New(), DriverID: uuid.New(),
		StartLatitude: 59.9, StartLongitude: 30.3,
		EndLatitude: 59.8, EndLongitude: 30.2,
		Price: 1450,
	}
}

// Проверяет диапазоны координат, цену и UUID
func TestNewTripParams_Validate(t *testing.T) {
	tests := []struct {
		name      string
		mutate    func(p *trip.NewTripParams)
		wantField string
	}{
		{"valid", func(*trip.NewTripParams) {}, ""},
		{"explicit zeros are valid", func(p *trip.NewTripParams) {
			p.StartLatitude, p.StartLongitude, p.EndLatitude, p.EndLongitude, p.Price = 0, 0, 0, 0, 0
		}, ""},
		{"boundary values are valid", func(p *trip.NewTripParams) {
			p.StartLatitude, p.EndLatitude = 90, -90
			p.StartLongitude, p.EndLongitude = 180, -180
		}, ""},
		{"zero user_id", func(p *trip.NewTripParams) { p.UserID = uuid.Nil }, "user_id"},
		{"zero driver_id", func(p *trip.NewTripParams) { p.DriverID = uuid.Nil }, "driver_id"},
		{"negative price", func(p *trip.NewTripParams) { p.Price = -1 }, "price"},
		{"start latitude too big", func(p *trip.NewTripParams) { p.StartLatitude = 90.000001 }, fieldStartLatitude},
		{"start latitude too small", func(p *trip.NewTripParams) { p.StartLatitude = -90.000001 }, fieldStartLatitude},
		{"start longitude too big", func(p *trip.NewTripParams) { p.StartLongitude = 180.000001 }, "start_point.longitude"},
		{"start longitude too small", func(p *trip.NewTripParams) { p.StartLongitude = -180.000001 }, "start_point.longitude"},
		{"end latitude too big", func(p *trip.NewTripParams) { p.EndLatitude = 91 }, "end_point.latitude"},
		{"end latitude too small", func(p *trip.NewTripParams) { p.EndLatitude = -91 }, "end_point.latitude"},
		{"end longitude too big", func(p *trip.NewTripParams) { p.EndLongitude = 181 }, fieldEndLongitude},
		{"end longitude too small", func(p *trip.NewTripParams) { p.EndLongitude = -181 }, fieldEndLongitude},
		{"NaN latitude", func(p *trip.NewTripParams) { p.StartLatitude = math.NaN() }, fieldStartLatitude},
		{"infinite longitude", func(p *trip.NewTripParams) { p.EndLongitude = math.Inf(1) }, fieldEndLongitude},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			p := validParams()
			tt.mutate(&p)
			err := p.Validate()
			if tt.wantField == "" {
				require.NoError(t, err)
				return
			}
			var ve *trip.ValidationError
			require.ErrorAs(t, err, &ve)
			require.Equal(t, tt.wantField, ve.Field)
		})
	}
}
