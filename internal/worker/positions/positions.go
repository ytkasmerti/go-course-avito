//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package positions

import (
	"context"
	"fmt"
	"log/slog"
	"sync"
	"sync/atomic"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/ytkasmerti/go-course-avito/internal/model"
)

type (
	staleTrips interface {
		ListStale(ctx context.Context, staleAfter time.Duration) ([]model.StaleTrip, error)
	}
	pushGateway interface {
		RequestPosition(ctx context.Context, tripID, driverID uuid.UUID) error
	}
)

type Ticker interface {
	C() <-chan time.Time
	Stop()
}

type realTicker struct{ t *time.Ticker }

func (r realTicker) C() <-chan time.Time { return r.t.C }
func (r realTicker) Stop()               { r.t.Stop() }

func newRealTicker(d time.Duration) Ticker { return realTicker{t: time.NewTicker(d)} }

const (
	resultQueued    = "queued"
	resultDuplicate = "duplicate"
	resultDropped   = "dropped"
	resultSuccess   = "success"
	resultError     = "error"
)

type metrics struct {
	staleChecks metric.Int64Counter
	requests    metric.Int64Counter
	duration    metric.Float64Histogram
	active      atomic.Int64
}

func newMetrics(queueSize func() int) (*metrics, error) {
	meter := otel.Meter("trip-service/positions")
	staleChecks, err := meter.Int64Counter("tripgo.stale_checks",
		metric.WithDescription("Stale trip check results"))
	if err != nil {
		return nil, err
	}
	requests, err := meter.Int64Counter("tripgo.position_requests",
		metric.WithDescription("Position request results"))
	if err != nil {
		return nil, err
	}
	duration, err := meter.Float64Histogram("tripgo.position_requests.duration",
		metric.WithUnit("s"), metric.WithDescription("Push Service call duration"))
	if err != nil {
		return nil, err
	}
	m := &metrics{staleChecks: staleChecks, requests: requests, duration: duration}
	_, err = meter.Int64ObservableGauge("tripgo.position_requests.queue.size",
		metric.WithDescription("Tasks in queue"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(int64(queueSize()))
			return nil
		}))
	if err != nil {
		return nil, err
	}
	_, err = meter.Int64ObservableGauge("tripgo.position_requests.workers.active",
		metric.WithDescription("Busy workers"),
		metric.WithInt64Callback(func(_ context.Context, o metric.Int64Observer) error {
			o.Observe(m.active.Load())
			return nil
		}))
	if err != nil {
		return nil, err
	}
	return m, nil
}

func (m *metrics) staleCheck(ctx context.Context, result string) {
	m.staleChecks.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}

func (m *metrics) request(ctx context.Context, result string) {
	m.requests.Add(ctx, 1, metric.WithAttributes(attribute.String("result", result)))
}

func (m *metrics) observeDuration(ctx context.Context, result string, seconds float64) {
	m.duration.Record(ctx, seconds, metric.WithAttributes(attribute.String("result", result)))
}

type Config struct {
	CheckInterval time.Duration
	StaleAfter    time.Duration
	Workers       int
	QueueSize     int
	PushTimeout   time.Duration
}

type task struct {
	trip model.StaleTrip
	link trace.SpanContext // спан проверки, связь через span link
}

type Component struct {
	cfg       Config
	trips     staleTrips
	push      pushGateway
	log       *slog.Logger
	metrics   *metrics
	newTicker func(time.Duration) Ticker
	tracer    trace.Tracer

	queue   chan task
	mu      sync.Mutex
	pending map[uuid.UUID]struct{} // в очереди: сканер, воркеры и хендлеры координат

	scannerCancel context.CancelFunc
	scannerDone   chan struct{}
	workersCancel context.CancelFunc
	workersDone   chan struct{}
}

func New(cfg Config, trips staleTrips, push pushGateway, log *slog.Logger, newTicker func(time.Duration) Ticker) (*Component, error) {
	queue := make(chan task, cfg.QueueSize)
	m, err := newMetrics(func() int { return len(queue) })
	if err != nil {
		return nil, fmt.Errorf("init positions metrics: %w", err)
	}
	if newTicker == nil {
		newTicker = newRealTicker
	}
	return &Component{
		cfg: cfg, trips: trips, push: push, log: log, metrics: m,
		newTicker:   newTicker,
		tracer:      otel.Tracer("trip-service/positions"),
		queue:       queue,
		pending:     make(map[uuid.UUID]struct{}),
		scannerDone: make(chan struct{}),
		workersDone: make(chan struct{}),
	}, nil
}

// Запускает сканер и фиксированный пул воркеров;
func (c *Component) Start(ctx context.Context) {
	scannerCtx, scannerCancel := context.WithCancel(ctx)
	c.scannerCancel = scannerCancel
	workersCtx, workersCancel := context.WithCancel(ctx)
	c.workersCancel = workersCancel

	var wg sync.WaitGroup
	for i := 0; i < c.cfg.Workers; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			c.runWorker(workersCtx)
		}()
	}
	go func() {
		wg.Wait()
		close(c.workersDone)
	}()
	c.log.InfoContext(ctx, "position workers started", slog.Int("workers", c.cfg.Workers))

	go c.runScanner(scannerCtx)
}

// Убирает поездку из очереди, координата пришла, пуш не нужен
func (c *Component) Cancel(tripID uuid.UUID) {
	c.mu.Lock()
	delete(c.pending, tripID)
	c.mu.Unlock()
}

// дорабатывают очередь в рамках ctx, по его истечении принудительная отмена
func (c *Component) Shutdown(ctx context.Context) error {
	c.scannerCancel()
	<-c.scannerDone // запрос сканера в бд ограничен его контекстом
	close(c.queue)
	defer c.workersCancel()
	defer c.log.InfoContext(ctx, "position workers stopped")
	select {
	case <-c.workersDone:
		return nil
	case <-ctx.Done():
		c.workersCancel()
		<-c.workersDone
		c.log.WarnContext(ctx, "position workers stopped by timeout", slog.Any("err", ctx.Err()))
		return fmt.Errorf("drain position workers: %w", ctx.Err())
	}
}

func (c *Component) runScanner(ctx context.Context) {
	defer close(c.scannerDone)
	c.log.InfoContext(ctx, "position scanner started", slog.Duration("interval", c.cfg.CheckInterval))
	t := c.newTicker(c.cfg.CheckInterval)
	defer t.Stop()
	for {
		select {
		case <-ctx.Done():
			c.log.InfoContext(ctx, "position scanner stopped")
			return
		case <-t.C():
			c.scanOnce(ctx)
		}
	}
}

func (c *Component) scanOnce(ctx context.Context) {
	ctx, span := c.tracer.Start(ctx, "positions.scan")
	defer span.End()
	trips, err := c.trips.ListStale(ctx, c.cfg.StaleAfter)
	if err != nil {
		span.RecordError(err)
		span.SetStatus(codes.Error, "stale scan failed")
		if ctx.Err() == nil { // при остановке отмена запроса
			c.log.WarnContext(ctx, "stale trips scan failed", slog.Any("err", err))
		}
		return
	}
	for _, t := range trips {
		result := c.enqueue(task{trip: t, link: span.SpanContext()})
		c.metrics.staleCheck(ctx, result)
		if result == resultDropped {
			c.metrics.request(ctx, resultDropped)
			c.log.WarnContext(ctx, "position request dropped: queue full",
				slog.String("trip_id", t.ID.String()))
		}
	}
}

// не блокирует сканер, заполненная очередь - дроп
func (c *Component) enqueue(t task) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.pending[t.trip.ID]; ok {
		return resultDuplicate
	}
	c.pending[t.trip.ID] = struct{}{}
	select {
	case c.queue <- t:
		return resultQueued
	default:
		delete(c.pending, t.trip.ID)
		return resultDropped
	}
}

func (c *Component) runWorker(ctx context.Context) {
	for {
		select {
		case <-ctx.Done():
			return
		case t, ok := <-c.queue:
			if !ok {
				return // канал закрыт и пуст — очередь доработана
			}
			c.process(ctx, t)
		}
	}
}

func (c *Component) process(ctx context.Context, t task) {
	c.mu.Lock()
	_, queued := c.pending[t.trip.ID]
	c.mu.Unlock()
	if !queued {
		// координата пришла, пока задача лежала в очереди
		c.metrics.request(ctx, resultDropped)
		return
	}
	c.metrics.active.Add(1)
	defer c.metrics.active.Add(-1)

	ctx, cancel := context.WithTimeout(ctx, c.cfg.PushTimeout)
	defer cancel()
	// обработка асинхронная, со спаном проверки связано через link
	ctx, span := c.tracer.Start(ctx, "positions.request",
		trace.WithNewRoot(), trace.WithLinks(trace.Link{SpanContext: t.link}))
	defer span.End()

	start := time.Now()
	err := c.push.RequestPosition(ctx, t.trip.ID, t.trip.DriverID)
	result := resultSuccess
	if err != nil {
		result = resultError
		span.RecordError(err)
		span.SetStatus(codes.Error, "push request failed")
		c.log.WarnContext(ctx, "push request failed",
			slog.String("trip_id", t.trip.ID.String()), slog.Any("err", err))
	}
	c.metrics.request(ctx, result)
	c.metrics.observeDuration(ctx, result, time.Since(start).Seconds())

	// запись убираем после обработки
	c.mu.Lock()
	delete(c.pending, t.trip.ID)
	c.mu.Unlock()
}
