package middleware

import (
	"net/http"

	"github.com/go-chi/chi/v5"
)

const unmatchedRoute = "unmatched"

func routeOf(r *http.Request) string {
	if rc := chi.RouteContext(r.Context()); rc != nil {
		if p := rc.RoutePattern(); p != "" {
			return p
		}
	}
	return unmatchedRoute
}

func matchRoute(routes chi.Routes, r *http.Request) string {
	rctx := chi.NewRouteContext()
	if routes.Match(rctx, r.Method, r.URL.Path) {
		if p := rctx.RoutePattern(); p != "" {
			return p
		}
	}
	return unmatchedRoute
}
