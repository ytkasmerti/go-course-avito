package trip_test

import (
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"

	. "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
)

func TestTrip_Finish(t *testing.T) {
	now := time.Now()

	tests := []struct {
		name      string
		trip      Trip
		wantErr   error
		wantState Status
	}{
		{
			name:      "active → completed",
			trip:      Trip{ID: uuid.New(), Status: StatusActive},
			wantErr:   nil,
			wantState: StatusCompleted,
		},
		{
			name:      "completed → error",
			trip:      Trip{ID: uuid.New(), Status: StatusCompleted},
			wantErr:   ErrTripCompleted,
			wantState: StatusCompleted,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := tt.trip.Finish(now)
			if !errors.Is(err, tt.wantErr) {
				t.Fatalf("err: got %v, want %v", err, tt.wantErr)
			}
			if tt.trip.Status != tt.wantState {
				t.Errorf("status: got %v, want %v", tt.trip.Status, tt.wantState)
			}
			if tt.wantErr == nil && tt.trip.FinishedAt == nil {
				t.Errorf("FinishedAt must be set on success")
			}
		})
	}
}
