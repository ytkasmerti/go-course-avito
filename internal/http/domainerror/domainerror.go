package domainerror

import (
	"errors"
	"net/http"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	api "github.com/ytkasmerti/go-course-avito/internal/generated"
)

func Problem(err error) (int, api.Problem) {
	var ve *domaintrip.ValidationError
	switch {
	case errors.As(err, &ve):
		return http.StatusBadRequest, problem(http.StatusBadRequest, "invalid_request", "Invalid request", ve.Error())
	case errors.Is(err, domaintrip.ErrTripNotFound):
		return http.StatusNotFound, problem(http.StatusNotFound, "trip_not_found", "Trip not found", "Trip not found")
	case errors.Is(err, domaintrip.ErrDriverBusy):
		return http.StatusConflict, problem(http.StatusConflict, "driver_busy", "Driver busy", "Driver already has an active trip")
	case errors.Is(err, domaintrip.ErrTripCompleted):
		return http.StatusConflict, problem(http.StatusConflict, "trip_completed", "Trip completed", "Trip already completed")
	case errors.Is(err, domaintrip.ErrIdempotencyConflict):
		return http.StatusConflict, problem(http.StatusConflict, "idempotency_conflict", "Idempotency conflict", "Idempotency key reused with different body")
	default:
		return http.StatusInternalServerError, problem(http.StatusInternalServerError, "internal_error", "Internal error", "internal error")
	}
}

func problem(status int, code, title, detail string) api.Problem {
	return api.Problem{
		Type:   "https://tripgo.example/problems/" + code,
		Title:  title,
		Status: int32(status),
		Detail: &detail,
		Code:   code,
	}
}
