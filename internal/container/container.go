package container

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"

	"github.com/ytkasmerti/go-course-avito/internal/config"
	"github.com/ytkasmerti/go-course-avito/internal/gateway/push"
	api "github.com/ytkasmerti/go-course-avito/internal/generated"
	"github.com/ytkasmerti/go-course-avito/internal/http/domainerror"
	"github.com/ytkasmerti/go-course-avito/internal/http/middleware"
	positionHandler "github.com/ytkasmerti/go-course-avito/internal/http/position"
	httpserver "github.com/ytkasmerti/go-course-avito/internal/http/server"
	createHandler "github.com/ytkasmerti/go-course-avito/internal/http/trip/create"
	finishHandler "github.com/ytkasmerti/go-course-avito/internal/http/trip/finish"
	getHandler "github.com/ytkasmerti/go-course-avito/internal/http/trip/get"
	"github.com/ytkasmerti/go-course-avito/internal/infra/postgres"
	"github.com/ytkasmerti/go-course-avito/internal/infra/postgres/transaction"
	"github.com/ytkasmerti/go-course-avito/internal/infra/telemetry"
	repoTrip "github.com/ytkasmerti/go-course-avito/internal/repository/trip"
	positionUC "github.com/ytkasmerti/go-course-avito/internal/usecase/position"
	createUC "github.com/ytkasmerti/go-course-avito/internal/usecase/trip/create"
	finishUC "github.com/ytkasmerti/go-course-avito/internal/usecase/trip/finish"
	getUC "github.com/ytkasmerti/go-course-avito/internal/usecase/trip/get"
	"github.com/ytkasmerti/go-course-avito/internal/worker/positions"
)

type Container struct {
	Handler  http.Handler
	Logger   *slog.Logger
	shutdown func(context.Context) error
	pool     interface{ Close() }
	workers  interface{ Shutdown(context.Context) error }
	push     interface{ Close() }
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

	txManager := transaction.New(pool, cfg.DatabaseQueryTimeout)
	repo := repoTrip.New(pool, cfg.DatabaseQueryTimeout)

	bizMetrics, err := telemetry.NewBusinessMetrics()
	if err != nil {
		pool.Close()
		_ = tel.Shutdown(ctx)
		return nil, fmt.Errorf("init business metrics: %w", err)
	}

	createUsecase := createUC.New(repo, txManager, bizMetrics, tel.Logger)
	getUsecase := getUC.New(repo)
	finishUsecase := finishUC.New(repo, txManager, bizMetrics, tel.Logger)

	createH := createHandler.New(createUsecase, tel.Logger)
	getH := getHandler.New(getUsecase, tel.Logger)
	finishH := finishHandler.New(finishUsecase, tel.Logger)

	router := chi.NewRouter()

	chain, err := middleware.Chain(router, tel.Logger)
	if err != nil {
		pool.Close()
		_ = tel.Shutdown(ctx)
		return nil, fmt.Errorf("init middleware: %w", err)
	}
	router.Use(chain...)

	pushClient := push.New(cfg.PushHTTPURL, cfg.PushRequestTimeout)
	workers, err := positions.New(positions.Config{
		CheckInterval: cfg.PositionCheckInterval,
		StaleAfter:    cfg.PositionStaleAfter,
		Workers:       cfg.PositionWorkers,
		QueueSize:     cfg.PositionQueueSize,
		PushTimeout:   cfg.PushRequestTimeout,
		BatchSize:     cfg.PositionScanBatchSize,
	}, repo, pushClient, tel.Logger, nil)
	if err != nil {
		pushClient.Close()
		pool.Close()
		_ = tel.Shutdown(ctx)
		return nil, fmt.Errorf("init positions worker: %w", err)
	}
	savePositionUsecase := positionUC.NewSave(repo, repo, txManager, workers, bizMetrics)
	listPositionUsecase := positionUC.NewList(repo, repo)
	savePositionH := positionHandler.NewSave(savePositionUsecase, tel.Logger)
	listPositionH := positionHandler.NewList(listPositionUsecase, tel.Logger)

	// Маршруты и разбор параметров: только сгенерированный адаптер
	srv := httpserver.New(createH, getH, finishH, savePositionH, listPositionH, pool, cfg.DatabaseQueryTimeout)
	api.HandlerWithOptions(srv, api.ChiServerOptions{
		BaseRouter:       router,
		ErrorHandlerFunc: domainerror.InvalidParam,
	})

	// фоновый опрос живет своим контекстом
	workers.Start(context.WithoutCancel(ctx))

	return &Container{
		Handler:  router,
		Logger:   tel.Logger,
		shutdown: tel.Shutdown,
		pool:     pool,
		workers:  workers,
		push:     pushClient,
	}, nil
}

func (c *Container) Shutdown(ctx context.Context) error {
	var errs []error
	//  сканер и воркеры - HTTP-клиент пушей - пул БД - телеметрия
	if err := c.workers.Shutdown(ctx); err != nil {
		errs = append(errs, err)
	}
	c.push.Close()
	// pgxpool.Close ждет возврата всех занятых соединений
	// Если запрос завис остановка укладывается в тот же дедлайн SHUTDOWN_TIMEOUT
	closed := make(chan struct{})
	go func() {
		c.pool.Close()
		close(closed)
	}()
	select {
	case <-closed:
	case <-ctx.Done():
		errs = append(errs, fmt.Errorf("close pool: %w", ctx.Err()))
	}
	// телеметрия последней, логи и метрики остановки успевают экспортироваться
	if err := c.shutdown(ctx); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("shutdown: %v", errs)
	}
	return nil
}
