package telemetry

import (
	"context"
	"fmt"
	"log/slog"
	"os"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlpmetric/otlpmetrichttp"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracehttp"
	"go.opentelemetry.io/otel/propagation"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	semconv "go.opentelemetry.io/otel/semconv/v1.24.0"
)

type Telemetry struct {
	Logger         *slog.Logger
	shutdownTrace  func(context.Context) error
	shutdownMetric func(context.Context) error
}

func New(ctx context.Context, logLevel string) (*Telemetry, error) {
	level := slog.LevelInfo
	if err := level.UnmarshalText([]byte(logLevel)); err != nil {
		level = slog.LevelInfo
	}

	res, err := resource.New(ctx,
		resource.WithAttributes(
			semconv.ServiceName("trip-service"),
			semconv.ServiceVersion("1.0.0"),
			attribute.String("service.namespace", "tripgo"),
			attribute.String("deployment.environment", "local"),
		),
	)
	if err != nil {
		return nil, fmt.Errorf("resource: %w", err)
	}

	traceExp, err := otlptracehttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("otlp trace exporter: %w", err)
	}
	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(traceExp),
		sdktrace.WithResource(res),
	)
	otel.SetTracerProvider(tp)
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(
		propagation.TraceContext{},
		propagation.Baggage{},
	))

	metricExp, err := otlpmetrichttp.New(ctx)
	if err != nil {
		return nil, fmt.Errorf("otlp metric exporter: %w", err)
	}
	mp := sdkmetric.NewMeterProvider(
		sdkmetric.WithReader(sdkmetric.NewPeriodicReader(metricExp)),
		sdkmetric.WithResource(res),
	)
	otel.SetMeterProvider(mp)

	logger := slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{
		Level: level,
		ReplaceAttr: func(_ []string, a slog.Attr) slog.Attr {
			switch a.Key {
			case slog.TimeKey:
				return slog.Attr{Key: "time", Value: a.Value}
			case slog.MessageKey:
				return slog.Attr{Key: "msg", Value: a.Value}
			case slog.LevelKey:
				return slog.Attr{Key: "level", Value: a.Value}
			}
			return a
		},
	}).WithAttrs([]slog.Attr{slog.String("service", "trip-service")}))

	return &Telemetry{
		Logger:         logger,
		shutdownTrace:  tp.Shutdown,
		shutdownMetric: mp.Shutdown,
	}, nil
}

func (t *Telemetry) Shutdown(ctx context.Context) error {
	var errs []error
	if err := t.shutdownTrace(ctx); err != nil {
		errs = append(errs, err)
	}
	if err := t.shutdownMetric(ctx); err != nil {
		errs = append(errs, err)
	}
	if len(errs) > 0 {
		return fmt.Errorf("shutdown telemetry: %v", errs)
	}
	return nil
}
