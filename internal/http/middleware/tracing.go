package middleware

import (
	"net/http"
	"strconv"

	"github.com/go-chi/chi/v5"
	chiMiddleware "github.com/go-chi/chi/v5/middleware"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric/noop"
	"go.opentelemetry.io/otel/trace"
)

func Tracing(routes chi.Routes) func(http.Handler) http.Handler {
	return func(next http.Handler) http.Handler {
		inner := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			ww := chiMiddleware.NewWrapResponseWriter(w, r.ProtoMajor)
			next.ServeHTTP(ww, r)
			span := trace.SpanFromContext(r.Context())
			span.SetAttributes(attribute.String("http.route", matchRoute(routes, r)))
			if ww.Status() >= http.StatusInternalServerError {
				span.SetStatus(codes.Error, http.StatusText(ww.Status()))
				span.SetAttributes(attribute.String("error.type", strconv.Itoa(ww.Status())))
			}
		})
		return otelhttp.NewHandler(inner, "http.server",
			otelhttp.WithMeterProvider(noop.NewMeterProvider()),
			otelhttp.WithSpanNameFormatter(func(_ string, r *http.Request) string {
				return r.Method + " " + matchRoute(routes, r)
			}),
		)
	}
}
