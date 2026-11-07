package main

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	"github.com/ytkasmerti/go-course-avito/internal/config"
	"github.com/ytkasmerti/go-course-avito/internal/container"
)

func main() {
	if err := run(); err != nil {
		slog.Error("service failed", "err", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return fmt.Errorf("load config: %w", err)
	}

	ctx, stop := signal.NotifyContext(context.Background(), syscall.SIGINT, syscall.SIGTERM)
	defer stop()

	c, err := container.New(ctx, *cfg)
	if err != nil {
		return fmt.Errorf("init container: %w", err)
	}

	server := &http.Server{
		Addr:              cfg.HTTPAddr,
		Handler:           c.Handler,
		ReadTimeout:       cfg.HTTPReadTimeout,
		ReadHeaderTimeout: cfg.HTTPReadHeaderTimeout,
		WriteTimeout:      cfg.HTTPWriteTimeout,
		IdleTimeout:       cfg.HTTPIdleTimeout,
	}

	errCh := make(chan error, 1)
	go func() {
		c.Logger.Info("server started", "addr", cfg.HTTPAddr)
		if err := server.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	var serveErr error
	select {
	case <-ctx.Done():
		c.Logger.Info("shutdown started")
	case serveErr = <-errCh:
		c.Logger.Error("server failed", "err", serveErr)
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), cfg.ShutdownTimeout)
	defer cancel()

	if err := server.Shutdown(shutdownCtx); err != nil {
		c.Logger.Error("server shutdown", "err", err)
	}
	if err := c.Shutdown(shutdownCtx); err != nil {
		c.Logger.Error("container shutdown", "err", err)
	}

	c.Logger.Info("shutdown completed")
	return serveErr
}
