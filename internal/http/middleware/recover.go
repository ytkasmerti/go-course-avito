package middleware

import (
	"encoding/json"
	"fmt"
	"log/slog"
	"net/http"
	"runtime/debug"

	"github.com/ytkasmerti/go-course-avito/internal/http/domainerror"
)

func Recover(log *slog.Logger) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			defer func() {
				if p := recover(); p != nil {
					log.Error("panic", "panic", p, "stack", string(debug.Stack()))
					status, problem := domainerror.Problem(fmt.Errorf("panic"))
					w.Header().Set("Content-Type", "application/problem+json")
					w.WriteHeader(status)
					_ = json.NewEncoder(w).Encode(problem)
				}
			}()
			next.ServeHTTP(w, r)
		})
	}
}
