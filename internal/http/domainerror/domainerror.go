package domainerror

import (
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

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

func problem(status int32, code, title, detail string) api.Problem {
	return api.Problem{
		Type:   "https://tripgo.example/problems/" + code,
		Title:  title,
		Status: status,
		Detail: &detail,
		Code:   code,
	}
}

// Ошибки разбора tripId/Idempotency-Key отдаются как problem+json invalid_request
func InvalidParam(w http.ResponseWriter, _ *http.Request, err error) {
	field, message := "parameter", "invalid value"

	var invalid *api.InvalidParamFormatError
	var many *api.TooManyValuesForParamError
	switch {
	case errors.As(err, &invalid):
		field = invalid.ParamName
	case errors.As(err, &many):
		field, message = many.ParamName, "expected a single value"
	}

	status, p := Problem(&domaintrip.ValidationError{Field: field, Message: message})
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

// Превращает ошибку в problem+json и пишет в лог с контекстом запроса
func Write(w http.ResponseWriter, r *http.Request, log *slog.Logger, err error) {
	status, p := Problem(err)
	if status >= http.StatusInternalServerError {
		log.ErrorContext(r.Context(), "request failed",
			slog.String("method", r.Method),
			slog.String("route", routePattern(r)),
			slog.Any("err", err),
		)
	}
	WriteProblem(w, status, p)
}

func WriteProblem(w http.ResponseWriter, status int, p api.Problem) {
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

func routePattern(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); p != "" {
			return p
		}
	}
	return "unmatched"
}
