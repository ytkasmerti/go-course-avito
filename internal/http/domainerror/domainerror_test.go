package domainerror_test

import (
	"errors"
	"net/http"
	"testing"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	"github.com/ytkasmerti/go-course-avito/internal/http/domainerror"
)

func TestProblem(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation", &domaintrip.ValidationError{Field: "x", Message: "y"}, http.StatusBadRequest, "invalid_request"},
		{"not found", domaintrip.ErrTripNotFound, http.StatusNotFound, "trip_not_found"},
		{"driver busy", domaintrip.ErrDriverBusy, http.StatusConflict, "driver_busy"},
		{"completed", domaintrip.ErrTripCompleted, http.StatusConflict, "trip_completed"},
		{"idempotency conflict", domaintrip.ErrIdempotencyConflict, http.StatusConflict, "idempotency_conflict"},
		{"unknown", errors.New("boom"), http.StatusInternalServerError, "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, p := domainerror.Problem(tt.err)
			if status != tt.wantStatus {
				t.Errorf("status: got %d, want %d", status, tt.wantStatus)
			}
			if p.Code != tt.wantCode {
				t.Errorf("code: got %s, want %s", p.Code, tt.wantCode)
			}
		})
	}
}
