package server

import (
	"context"
	"encoding/json"
	"net/http"
	"time"

	api "github.com/ytkasmerti/go-course-avito/internal/generated"
)

// Реализует api.ServerInterface, маршруты и разбор параметров делает сгенерированный адаптер
// Здесь только вызов хендлеров с уже разобранными параметрами
type Server struct {
	api.Unimplemented

	create      createHandler
	get         tripHandler
	finish      tripHandler
	db          pinger
	pingTimeout time.Duration
}

func New(create createHandler, get, finish tripHandler, db pinger, pingTimeout time.Duration) *Server {
	return &Server{create: create, get: get, finish: finish, db: db, pingTimeout: pingTimeout}
}

func (s *Server) CreateTrip(w http.ResponseWriter, r *http.Request, params api.CreateTripParams) {
	s.create.Handle(w, r, params)
}

func (s *Server) GetTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	s.get.Handle(w, r, tripID)
}

func (s *Server) FinishTrip(w http.ResponseWriter, r *http.Request, tripID api.TripId) {
	s.finish.Handle(w, r, tripID)
}

func (s *Server) Health(w http.ResponseWriter, _ *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	_ = json.NewEncoder(w).Encode(api.HealthResponse{Status: api.Ok})
}

func (s *Server) Ready(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), s.pingTimeout)
	defer cancel()

	w.Header().Set("Content-Type", "application/json")
	if err := s.db.Ping(ctx); err != nil {
		w.WriteHeader(http.StatusServiceUnavailable)
		_ = json.NewEncoder(w).Encode(api.HealthResponse{Status: api.Unavailable})
		return
	}
	_ = json.NewEncoder(w).Encode(api.HealthResponse{Status: api.Ok})
}
