package trip

import (
	"context"
	"encoding/json"
	"errors"
	"log"
	"net/http"
	api "github.com/ytkasmerti/go-course-avito/internal/generated"
)

type Pinger interface{ 
	Ping(ctx context.Context) error 
}

type Handler struct {
	api.Unimplemented
	svc  *Service
	pool Pinger
	log  *log.Logger
}

func NewHandler(svc *Service, pool interface{ Ping(ctx context.Context) error }, log *log.Logger) *Handler {
 return &Handler{svc: svc, pool: pool, log: log}
}

// POST /api/v1/trips
func (h *Handler) CreateTrip(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	var body api.CreateTripJSONRequestBody
	if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
		h.problem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", "failed to decode body")
		return
	}

	in := CreateInput{
		UserID:         body.UserId,
		DriverID:       body.DriverId,
		StartLatitude:  body.StartPoint.Latitude,
		StartLongitude: body.StartPoint.Longitude,
		EndLatitude:    body.EndPoint.Latitude,
		EndLongitude:   body.EndPoint.Longitude,
		Price:          body.Price,
	}

	t, err := h.svc.Create(r.Context(), in)
	if err != nil {
		h.handleError(w, r, err)
		return
	}

	w.Header().Set("Location", "/api/v1/trips/"+t.ID.String())
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusCreated)
	_ = json.NewEncoder(w).Encode(toAPI(t))
}

// GET /api/v1/trips/{tripId}
func (h *Handler) GetTrip(w http.ResponseWriter, r *http.Request, tripId api.TripId) {
	t, err := h.svc.Get(r.Context(), tripId)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toAPI(t))
}

// POST /api/v1/trips/{tripId}/finish
func (h *Handler) FinishTrip(w http.ResponseWriter, r *http.Request, tripId api.TripId) {
	t, err := h.svc.Finish(r.Context(), tripId)
	if err != nil {
		h.handleError(w, r, err)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(toAPI(t))
}

// GET /health
func (h *Handler) Health(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(api.HealthResponse{Status: "ok"})
}

// GET /ready
func (h *Handler) Ready(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	if err := h.pool.Ping(r.Context()); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(api.HealthResponse{Status: "unavailable"})
		return
	}
	w.WriteHeader(http.StatusOK)
	_ = json.NewEncoder(w).Encode(api.HealthResponse{Status: "ok"})
}

func (h *Handler) handleError(w http.ResponseWriter, r *http.Request, err error) {
	switch {
	case errors.Is(err, ErrTripNotFound):
		h.problem(w, r, http.StatusNotFound, "trip_not_found", "Trip not found", err.Error())
	case errors.Is(err, ErrDriverBusy):
		h.problem(w, r, http.StatusConflict, "driver_busy", "Driver busy", "Driver already has an active trip")
	case errors.Is(err, ErrTripCompleted):
		h.problem(w, r, http.StatusConflict, "trip_completed", "Trip completed", "Trip already completed")
	case errors.Is(err, ErrInvalidRequest):
		h.problem(w, r, http.StatusBadRequest, "invalid_request", "Invalid request", err.Error())
	default:
		h.log.Printf("Internal error: %v", err)
		h.problem(w, r, http.StatusInternalServerError, "internal_error", "Internal error", "internal error")
	}
}

func (h *Handler) problem(w http.ResponseWriter, r *http.Request, status int, code, title, detail string) {
	instance := r.URL.Path
	p := api.Problem{
		Code:     code,
		Detail:   &detail,
		Instance: &instance,
		Status:   int32(status),
		Title:    title,
		Type:     "https://tripgo.example/problems/" + code,
	}
	w.Header().Set("Content-Type", "application/problem+json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(p)
}

func toAPI(t *Trip) api.Trip {
	return api.Trip{
		Id:         t.ID,
		UserId:     t.UserID,
		DriverId:   t.DriverID,
		StartPoint: api.Coordinates{Latitude: t.StartLatitude, Longitude: t.StartLongitude},
		EndPoint:   api.Coordinates{Latitude: t.EndLatitude, Longitude: t.EndLongitude},
		Price:      t.Price,
		Status:     api.TripStatus(t.Status),
		StartedAt:  t.StartedAt,
		FinishedAt: t.FinishedAt,
	}
}