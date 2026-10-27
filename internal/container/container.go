package container

import (
	"context"
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"

	"github.com/ytkasmerti/go-course-avito/internal/config"
	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	api "github.com/ytkasmerti/go-course-avito/internal/generated"
	"github.com/ytkasmerti/go-course-avito/internal/http/domainerror"
	"github.com/ytkasmerti/go-course-avito/internal/http/middleware"
	createHandler "github.com/ytkasmerti/go-course-avito/internal/http/trip/create"
	finishHandler "github.com/ytkasmerti/go-course-avito/internal/http/trip/finish"
	getHandler "github.com/ytkasmerti/go-course-avito/internal/http/trip/get"
	"github.com/ytkasmerti/go-course-avito/internal/infra/postgres"
	"github.com/ytkasmerti/go-course-avito/internal/infra/postgres/transaction"
	"github.com/ytkasmerti/go-course-avito/internal/infra/telemetry"
	repoTrip "github.com/ytkasmerti/go-course-avito/internal/repository/trip"
	createUC "github.com/ytkasmerti/go-course-avito/internal/usecase/trip/create"
	finishUC "github.com/ytkasmerti/go-course-avito/internal/usecase/trip/finish"
	getUC "github.com/ytkasmerti/go-course-avito/internal/usecase/trip/get"
)

type Container struct {
	Handler  http.Handler
	Logger   *slog.Logger
	shutdown func(context.Context) error
	pool     interface{ Close() }
}

func New(ctx context.Context, cfg config.Config) (*Container, error) {
	tel, err := telemetry.New(ctx, cfg.LogLevel)
	if err != nil {
		return nil, fmt.Errorf("init telemetry: %w", err)
	}

	pool, err := postgres.NewPool(ctx, cfg)
	if err != nil {
		_ = tel.Shutdown(ctx)
		return nil, fmt.Errorf("init pool: %w", err)
	}

	txManager := transaction.New(pool)
	repo := repoTrip.New(pool)

	bizMetrics, err := telemetry.NewBusinessMetrics()
	if err != nil {
		pool.Close()
		_ = tel.Shutdown(ctx)
		return nil, fmt.Errorf("init business metrics: %w", err)
	}

	createUsecase := createUC.New(repo, txManager, bizMetrics, tel.Logger)
	getUsecase := getUC.New(repo)
	finishUsecase := finishUC.New(repo, txManager, bizMetrics, tel.Logger)

	createH := createHandler.New(createUsecase)
	getH := getHandler.New(getUsecase)
	finishH := finishHandler.New(finishUsecase)

	router := chi.NewRouter()

	metricsMiddleware, err := middleware.Metrics(router)
	if err != nil {
		pool.Close()
		_ = tel.Shutdown(ctx)
		return nil, fmt.Errorf("init metrics: %w", err)
	}

	router.Use(middleware.Recover(tel.Logger))
	router.Use(middleware.Tracing(router))
	router.Use(middleware.Logging(tel.Logger))
	router.Use(metricsMiddleware)

	router.Group(func(r chi.Router) {
		r.Post("/api/v1/trips", func(w http.ResponseWriter, r *http.Request) {
			var params api.CreateTripParams
			if raw := r.Header.Get("Idempotency-Key"); raw != "" {
				var id uuid.UUID
				if err := id.UnmarshalText([]byte(raw)); err != nil {
					status, p := domainerror.Problem(&domaintrip.ValidationError{Field: "Idempotency-Key", Message: "invalid uuid"})
					w.Header().Set("Content-Type", "application/problem+json")
					w.WriteHeader(status)
					_ = json.NewEncoder(w).Encode(p)
					return
				}
				key := api.IdempotencyKey(id)
				params.IdempotencyKey = &key
			}
			createH.Handle(w, r, params)
		})
		r.Get("/api/v1/trips/{tripId}", func(w http.ResponseWriter, r *http.Request) {
			id, err := uuid.Parse(chi.URLParam(r, "tripId"))
			if err != nil {
				status, p := domainerror.Problem(&domaintrip.ValidationError{Field: "tripId", Message: "invalid uuid"})
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(p)
				return
			}
			getH.Handle(w, r, api.TripId(id))
		})
		r.Post("/api/v1/trips/{tripId}/finish", func(w http.ResponseWriter, r *http.Request) {
			id, err := uuid.Parse(chi.URLParam(r, "tripId"))
			if err != nil {
				status, p := domainerror.Problem(&domaintrip.ValidationError{Field: "tripId", Message: "invalid uuid"})
				w.Header().Set("Content-Type", "application/problem+json")
				w.WriteHeader(status)
				_ = json.NewEncoder(w).Encode(p)
				return
			}
			finishH.Handle(w, r, api.TripId(id))
		})
	})

	router.Get("/health", func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(api.HealthResponse{Status: api.Ok})
	})

	router.Get("/ready", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		pingCtx, cancel := context.WithTimeout(r.Context(), cfg.DatabaseQueryTimeout)
		defer cancel()
		if err := pool.Ping(pingCtx); err != nil {
			w.WriteHeader(http.StatusServiceUnavailable)
			_ = json.NewEncoder(w).Encode(api.HealthResponse{Status: api.Unavailable})
			return
		}
		_ = json.NewEncoder(w).Encode(api.HealthResponse{Status: api.Ok})
	})

	return &Container{
		Handler:  router,
		Logger:   tel.Logger,
		shutdown: tel.Shutdown,
		pool:     pool,
	}, nil
}

func (c *Container) Shutdown(ctx context.Context) error {
	var errs []error
	if err := c.shutdown(ctx); err != nil {
		errs = append(errs, err)
	}
	c.pool.Close()
	if len(errs) > 0 {
		return fmt.Errorf("shutdown: %v", errs)
	}
	return nil
}
