package main

import (
	"context"
	"errors"
	"log"
	"net/http"
	"os/signal"
	"syscall"
	"github.com/go-chi/chi/v5"
	"github.com/ytkasmerti/go-course-avito/internal/config"
	"github.com/ytkasmerti/go-course-avito/internal/db"
	api "github.com/ytkasmerti/go-course-avito/internal/generated"
	"github.com/ytkasmerti/go-course-avito/internal/trip"
	"github.com/ytkasmerti/go-course-avito/internal/txmanager"
)

func main() {
	cfg, err := config.Load()
	if err != nil {
		log.Fatalf("Load config: %v", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	pool, err := db.NewPool(ctx, *cfg)
	if err != nil {
		log.Fatalf("Connect db: %v", err)
	}
	defer pool.Close()

	txManager := txmanager.New(pool)
	repo := trip.NewRepository(pool)
	service := trip.NewService(repo, txManager)
	handler := trip.NewHandler(service, pool, log.Default())

	router := chi.NewRouter()
	api.HandlerFromMux(handler, router)

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           router,
		ReadTimeout:       cfg.HTTPReadTimeout,
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
	}

	go func() {
		log.Printf("Server started: %s", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			log.Fatalf("Listen and serve: %v", err)
		}
	}()

	<-ctx.Done()
	log.Println("Shutdown server")
	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		log.Fatalf("Shutdown server: %v", err)
	}

	log.Println("Server stopped")
}