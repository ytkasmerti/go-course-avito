package middleware

import (
	"net/http"
	"time"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/metric"
)

type httpMetrics struct {
	duration metric.Float64Histogram
	active   metric.Int64UpDownCounter
}

func newHTTPMetrics() (*httpMetrics, error) {
	meter := otel.Meter("trip-service/http")
	duration, err := meter.Float64Histogram(
		"http.server.request.duration",
		metric.WithUnit("s"),
		metric.WithDescription("HTTP request duration"),
	)
	if err != nil {
		return nil, err
	}
	active, err := meter.Int64UpDownCounter(
		"http.server.active_requests",
		metric.WithUnit("{request}"),
		metric.WithDescription("HTTP requests in flight"),
	)
	if err != nil {
		return nil, err
	}
	return &httpMetrics{duration: duration, active: active}, nil
}

func Metrics(routes chi.Routes) (func(http.Handler) http.Handler, error) {
	m, err := newHTTPMetrics()
	if err != nil {
		return nil, err
	}
	return func(next http.Handler) http.Handler {
		return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			method := attribute.String("http.request.method", r.Method)
			route := attribute.String("http.route", matchRoute(routes, r))
			active := metric.WithAttributes(method, route)
			m.active.Add(r.Context(), 1, active)
			defer m.active.Add(r.Context(), -1, active)
			start := time.Now()
			ww := chiMiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			m.duration.Record(r.Context(), time.Since(start).Seconds(),
				metric.WithAttributes(method, route, attribute.Int("http.response.status_code", ww.Status())))
		})
	}, nil
}
