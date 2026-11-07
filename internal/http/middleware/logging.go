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
			completed := false
			defer func() {
				status := ww.Status()
				if !completed {
					status = http.StatusInternalServerError // паника: клиент получит 500 от Recover
				}
				level := slog.LevelInfo
				if status >= http.StatusInternalServerError {
					level = slog.LevelError
				}
				log.LogAttrs(r.Context(), level, "request handled",
					slog.String("method", r.Method),
					slog.String("route", routeOf(r)),
					slog.Int("status", status),
					slog.Int64("duration_ms", time.Since(start).Milliseconds()),
				)
			}()
			next.ServeHTTP(ww, r)
			completed = true
		})
	}
}
