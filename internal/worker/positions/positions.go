//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package positions

import (
	"container/heap"
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

type Config struct {
	CheckInterval time.Duration
	StaleAfter    time.Duration
	Workers       int
	QueueSize     int
	PushTimeout   time.Duration
	BatchSize     int
}

const (
	resultQueued    = "queued"
	resultDuplicate = "duplicate"
	resultDropped   = "dropped"
	resultSuccess   = "success"
	resultError     = "error"
	attrTripID      = "trip_id"
)

// min-heap по LastAt, первым уходит тот, кто дольше молчит
type priorityQueue []task

func (q priorityQueue) Len() int { return len(q) }

func (q priorityQueue) Less(i, j int) bool { return q[i].trip.LastAt.Before(q[j].trip.LastAt) }

func (q priorityQueue) Swap(i, j int) { q[i], q[j] = q[j], q[i] }

func (q *priorityQueue) Push(x any) { *q = append(*q, x.(task)) }

func (q *priorityQueue) Pop() any {
	old := *q
	n := len(old)
	t := old[n-1]
	*q = old[:n-1]
	return t
}

type task struct {
	trip model.StaleTrip
	link trace.SpanContext // спан проверки, связь через span link
}

type Component struct {
	cfg           Config
	trips         staleTrips
	push          pushGateway
	log           *slog.Logger
	metrics       *metrics
	newTicker     func(time.Duration) Ticker
	tracer        trace.Tracer
	queue         priorityQueue
	cond          *sync.Cond
	closed        bool
	mu            sync.Mutex
	pending       map[uuid.UUID]struct{} // в очереди: сканер, воркеры и хендлеры координат
	scannerCancel context.CancelFunc
	scannerDone   chan struct{}
	workersCancel context.CancelFunc
	workersDone   chan struct{}
}
type (
	staleTrips interface {
		ListStale(ctx context.Context, staleAfter time.Duration, after *model.StaleCursor, limit int) ([]model.StaleTrip, error)
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

type metrics struct {
	staleChecks metric.Int64Counter
	requests    metric.Int64Counter
	duration    metric.Float64Histogram
	active      atomic.Int64
	rowsRead    metric.Int64Histogram
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

	rowsRead, err := meter.Int64Histogram("tripgo.stale_checks.rows_read",
		metric.WithDescription("Stale trips read per tick"))
	if err != nil {
		return nil, err
	}

	m := &metrics{
		staleChecks: staleChecks,
		requests:    requests,
		duration:    duration,
		rowsRead:    rowsRead,
	}

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

func (m *metrics) observeRows(ctx context.Context, n int) {
	m.rowsRead.Record(ctx, int64(n))
}

func New(cfg Config, trips staleTrips, push pushGateway, log *slog.Logger, newTicker func(time.Duration) Ticker) (*Component, error) {
	if newTicker == nil {
		newTicker = newRealTicker
	}
	c := &Component{
		cfg: cfg, trips: trips, push: push, log: log,
		newTicker:   newTicker,
		tracer:      otel.Tracer("trip-service/positions"),
		queue:       make(priorityQueue, 0, cfg.QueueSize),
		pending:     make(map[uuid.UUID]struct{}),
		scannerDone: make(chan struct{}),
		workersDone: make(chan struct{}),
	}
	c.cond = sync.NewCond(&c.mu)
	m, err := newMetrics(func() int {
		c.mu.Lock()
		defer c.mu.Unlock()
		return c.queue.Len()
	})
	if err != nil {
		return nil, fmt.Errorf("init positions metrics: %w", err)
	}
	c.metrics = m
	return c, nil
}

// Запускает сканер и фиксированный пул воркеров;
func (c *Component) Start(ctx context.Context) {
	scannerCtx, scannerCancel := context.WithCancel(ctx)
	c.scannerCancel = scannerCancel
	workersCtx, workersCancel := context.WithCancel(ctx)
	c.workersCancel = workersCancel

	// отмена контекста должна разбудить воркеров, ждущих на cond
	go func() {
		<-workersCtx.Done()
		c.cond.Broadcast()
	}()

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
	defer c.mu.Unlock()
	if _, ok := c.pending[tripID]; !ok {
		return
	}
	delete(c.pending, tripID)
	for i, t := range c.queue {
		if t.trip.ID == tripID {
			heap.Remove(&c.queue, i)
			break
		}
	}
}

// останавливает сканер, воркеры дорабатывают очередь в рамках ctx, по его истечении принудительная отмена
func (c *Component) Shutdown(ctx context.Context) error {
	c.scannerCancel()
	<-c.scannerDone
	c.mu.Lock()
	c.closed = true
	c.cond.Broadcast()
	c.mu.Unlock()
	defer c.workersCancel()
	defer c.log.InfoContext(ctx, "position workers stopped")
	select {
	case <-c.workersDone:
		return nil
	case <-ctx.Done():
		c.workersCancel()
		c.cond.Broadcast()
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
	var cursor *model.StaleCursor
	read := 0
	for ctx.Err() == nil {
		batch, err := c.trips.ListStale(ctx, c.cfg.StaleAfter, cursor, c.cfg.BatchSize)
		if err != nil {
			span.RecordError(err)
			span.SetStatus(codes.Error, "stale scan failed")
			if ctx.Err() == nil {
				c.log.WarnContext(ctx, "stale trips scan failed", slog.Any("err", err))
			}
			break
		}
		read += len(batch)
		full := false
		for _, t := range batch {
			if c.enqueue(ctx, task{trip: t, link: span.SpanContext()}) == resultDropped {
				full = true // очередь заполнилась - на следующем тике заново
				break
			}
		}
		if full || len(batch) < c.cfg.BatchSize {
			break
		}
		last := batch[len(batch)-1]
		cursor = &model.StaleCursor{At: last.LastAt, ID: last.ID}
	}
	c.metrics.observeRows(ctx, read)
	span.SetAttributes(attribute.Int("stale.rows_read", read))
}

// не блокирует сканер, при переполнении вытесняем самую свежую поездку, если новая старше, иначе дропаем новую
func (c *Component) enqueue(ctx context.Context, t task) string {
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, ok := c.pending[t.trip.ID]; ok {
		c.metrics.staleCheck(ctx, resultDuplicate)
		return resultDuplicate
	}
	if c.queue.Len() >= c.cfg.QueueSize {
		worst := c.freshestIndex()
		if !t.trip.LastAt.Before(c.queue[worst].trip.LastAt) {
			c.metrics.staleCheck(ctx, resultDropped)
			c.metrics.request(ctx, resultDropped)
			c.log.WarnContext(ctx, "position request dropped: queue full",
				slog.String(attrTripID, t.trip.ID.String()))
			return resultDropped
		}
		// вытесненная молчит меньше всех и попадет в выборку на следующем тике
		evicted := c.queue[worst]
		heap.Remove(&c.queue, worst)
		delete(c.pending, evicted.trip.ID)
		c.metrics.staleCheck(ctx, resultDropped)
		c.metrics.request(ctx, resultDropped)
		c.log.WarnContext(ctx, "position request evicted from queue",
			slog.String(attrTripID, evicted.trip.ID.String()))
	}
	heap.Push(&c.queue, t)
	c.pending[t.trip.ID] = struct{}{}
	c.cond.Signal()
	c.metrics.staleCheck(ctx, resultQueued)
	return resultQueued
}

// индекс самой свежей поездки в куче
func (c *Component) freshestIndex() int {
	worst := 0
	for i := 1; i < c.queue.Len(); i++ {
		if c.queue[worst].trip.LastAt.Before(c.queue[i].trip.LastAt) {
			worst = i
		}
	}
	return worst
}

// ждет задачу, при закрытой и пустой очереди возвращает false
func (c *Component) next() (task, bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for c.queue.Len() == 0 && !c.closed {
		c.cond.Wait()
	}
	if c.queue.Len() == 0 {
		return task{}, false
	}
	return heap.Pop(&c.queue).(task), true
}

func (c *Component) runWorker(ctx context.Context) {
	for {
		t, ok := c.next()
		if !ok || ctx.Err() != nil {
			return // очередь закрыта и пуста — доработали все
		}
		c.process(ctx, t)
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
			slog.String(attrTripID, t.trip.ID.String()), slog.Any("err", err))
	}
	c.metrics.request(ctx, result)
	c.metrics.observeDuration(ctx, result, time.Since(start).Seconds())

	// запись убираем после обработки
	c.mu.Lock()
	delete(c.pending, t.trip.ID)
	c.mu.Unlock()
}
