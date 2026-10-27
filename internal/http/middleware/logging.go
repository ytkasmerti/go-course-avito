package middleware

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/go-chi/chi/v5/middleware"
)

func Logging(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			start := time.Now()
			ww := middleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			level := slog.LevelInfo
			if ww.Status() >= http.StatusInternalServerError {
				level = slog.LevelError
			}
			log.LogAttrs(r.Context(), level, "request handled",
				slog.String("method", r.Method),
				slog.String("route", routeOf(r)),
				slog.Int("status", ww.Status()),
				slog.Int64("duration_ms", time.Since(start).Milliseconds()),
			)
		})
	}
}
