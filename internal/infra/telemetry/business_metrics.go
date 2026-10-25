package telemetry

import (
	"context"
	"sync/atomic"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/metric"
)

type BusinessMetrics struct {
	created   metric.Int64Counter
	completed metric.Int64Counter
	active    *atomic.Int64
}

func NewBusinessMetrics() (*BusinessMetrics, error) {
	meter := otel.Meter("trip-service/business")

	created, err := meter.Int64Counter("tripgo.trips.created",
		metric.WithDescription("Trips created"))
	if err != nil {
		return nil, err
	}

	completed, err := meter.Int64Counter("tripgo.trips.completed",
		metric.WithDescription("Trips completed"))
	if err != nil {
		return nil, err
	}

	activeVal := &atomic.Int64{}
	_, err = meter.Int64ObservableGauge("tripgo.trips.active",
		metric.WithDescription("Trips currently active"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(activeVal.Load())
			return nil
		}),
	)
	if err != nil {
		return nil, err
	}

	return &BusinessMetrics{created: created, completed: completed, active: activeVal}, nil
}

func (m *BusinessMetrics) IncCreated(ctx context.Context) {
	m.created.Add(ctx, 1)
	m.active.Add(1)
}

func (m *BusinessMetrics) IncCompleted(ctx context.Context) {
	m.completed.Add(ctx, 1)
	m.active.Add(-1)
}
