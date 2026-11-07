package middleware

import (
	"log/slog"
	"net/http"

	"github.com/go-chi/chi/v5"
)

func Chain(routes chi.Routes, log *slog.Logger) ([]func(http.Handler) http.Handler, error) {
	metrics, err := Metrics(routes)
	if err != nil {
		return nil, err
	}
	// Recover снаружи, остальные записывают результат в defer, поэтому видят и панику со статусом 500
	return []func(http.Handler) http.Handler{
		Recover(log),
		Tracing(routes),
		Logging(log),
		metrics,
	}, nil
}
