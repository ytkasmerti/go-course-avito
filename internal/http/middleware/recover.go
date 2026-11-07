package middleware

import (
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
				p := recover()
				if p == nil {
					return
				}
				// net/http сам прерывает ответ
				if p == http.ErrAbortHandler {
					panic(p)
				}
				log.ErrorContext(r.Context(), "panic", "panic", p, "stack", string(debug.Stack()))
				status, problem := domainerror.Problem(fmt.Errorf("panic: %v", p))
				domainerror.WriteProblem(w, status, problem)
			}()
			next.ServeHTTP(w, r)
		})
	}
}
