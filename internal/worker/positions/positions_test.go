package positions_test

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"sync/atomic"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	"go.uber.org/goleak"
	"go.uber.org/mock/gomock"

	"github.com/ytkasmerti/go-course-avito/internal/model"
	"github.com/ytkasmerti/go-course-avito/internal/worker/positions"
)

func TestMain(m *testing.M) { goleak.VerifyTestMain(m) }

const (
	metricStaleChecks = "tripgo.stale_checks"
	metricRequests    = "tripgo.position_requests"
	metricActive      = "tripgo.position_requests.workers.active"
	resQueued         = "queued"
	resDuplicate      = "duplicate"
	resDropped        = "dropped"
	resSuccess        = "success"
	resError          = "error"
	metricRowsRead    = "tripgo.stale_rows_read"
)

// тесты не ждут реальные секунды
type fakeTicker struct{ ch chan time.Time }

func (f *fakeTicker) C() <-chan time.Time { return f.ch }
func (f *fakeTicker) Stop()               {}

func (f *fakeTicker) tick(t *testing.T) {
	t.Helper()
	select {
	case f.ch <- time.Now():
	case <-time.After(time.Second):
		t.Fatal("сканер не принял тик")
	}
}

type harness struct {
	c      *positions.Component
	ticker *fakeTicker
	trips  *MockstaleTrips
	push   *MockpushGateway
	reader *sdkmetric.ManualReader
	logs   *bytes.Buffer
}

func newHarness(t *testing.T, workers, queueSize int) *harness {
	t.Helper()
	return newHarnessCfg(t, positions.Config{
		CheckInterval: time.Hour, StaleAfter: time.Minute,
		Workers: workers, QueueSize: queueSize, PushTimeout: time.Second, BatchSize: 100,
	})
}

func newHarnessCfg(t *testing.T, cfg positions.Config) *harness {
	t.Helper()
	ctrl := gomock.NewController(t)
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))
	tk := &fakeTicker{ch: make(chan time.Time)}
	logs := &bytes.Buffer{}
	h := &harness{
		trips: NewMockstaleTrips(ctrl), push: NewMockpushGateway(ctrl),
		ticker: tk, reader: reader, logs: logs,
	}
	c, err := positions.New(cfg, h.trips, h.push, slog.New(slog.NewJSONHandler(logs, nil)),
		func(time.Duration) positions.Ticker { return tk })
	require.NoError(t, err)
	h.c = c
	c.Start(context.Background())
	return h
}

func (h *harness) counter(t *testing.T, name, result string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, h.reader.Collect(context.Background(), &rm))
	var sum int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			data, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range data.DataPoints {
				if v, _ := dp.Attributes.Value("result"); v.AsString() == result {
					sum += dp.Value
				}
			}
		}
	}
	return sum
}

func (h *harness) counterTotal(t *testing.T, name string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, h.reader.Collect(context.Background(), &rm))
	var sum int64
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			data, ok := m.Data.(metricdata.Sum[int64])
			require.True(t, ok)
			for _, dp := range data.DataPoints {
				sum += dp.Value
			}
		}
	}
	return sum
}

func (h *harness) gauge(t *testing.T, name string) int64 {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, h.reader.Collect(context.Background(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != name {
				continue
			}
			data, ok := m.Data.(metricdata.Gauge[int64])
			require.True(t, ok)
			if len(data.DataPoints) > 0 {
				return data.DataPoints[0].Value
			}
		}
	}
	return 0
}

// scans задаёт ответы ListStale по тикам; последний сигналит, что предыдущий тик обработан
func (h *harness) scans(scanned chan struct{}, batches ...[]model.StaleTrip) {
	calls := make([]any, 0, len(batches))
	for i, b := range batches {
		batch := b
		if i == len(batches)-1 {
			calls = append(calls, h.trips.EXPECT().ListStale(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
				DoAndReturn(func(context.Context, time.Duration, *model.StaleCursor, int) ([]model.StaleTrip, error) {
					close(scanned)
					return batch, nil
				}))
		} else {
			calls = append(calls, h.trips.EXPECT().ListStale(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(batch, nil))
		}
	}
	gomock.InOrder(calls...)
}

// сигналит о старте и ждет release либо отмены контекста
func blockPush(mock *MockpushGateway, id uuid.UUID, started chan<- struct{}, release <-chan struct{}) {
	mock.EXPECT().RequestPosition(gomock.Any(), id, gomock.Any()).
		DoAndReturn(func(ctx context.Context, _, _ uuid.UUID) error {
			started <- struct{}{}
			select {
			case <-release:
				return nil
			case <-ctx.Done():
				return ctx.Err()
			}
		})
}

// Проверяет что повторный тик не ставит поездку в очередь второй раз
func TestComponent_DuplicateTicks(t *testing.T) {
	h := newHarness(t, 1, 2)
	a := model.StaleTrip{ID: uuid.New(), DriverID: uuid.New()}
	started, release := make(chan struct{}), make(chan struct{})
	blockPush(h.push, a.ID, started, release)
	scanned := make(chan struct{})
	h.scans(scanned, []model.StaleTrip{a}, []model.StaleTrip{a}, nil)

	h.ticker.tick(t)
	<-started // задача в обработке, запись держится до конца обработки
	h.ticker.tick(t)
	h.ticker.tick(t)
	<-scanned // третий обход начался — второй точно завершен

	require.EqualValues(t, 1, h.counter(t, metricStaleChecks, resQueued))
	require.EqualValues(t, 1, h.counter(t, metricStaleChecks, resDuplicate))
	close(release)
	require.NoError(t, h.c.Shutdown(context.Background()))
	require.EqualValues(t, 1, h.counter(t, metricRequests, resSuccess))
}

// Проверяет отмену, координата между постановкой и обработкой отменяет пуш
func TestComponent_CancelBeforeProcessing(t *testing.T) {
	h := newHarness(t, 1, 2)
	a := model.StaleTrip{ID: uuid.New(), DriverID: uuid.New()}
	b := model.StaleTrip{ID: uuid.New(), DriverID: uuid.New()}
	started, release := make(chan struct{}), make(chan struct{})
	blockPush(h.push, b.ID, started, release)
	scanned := make(chan struct{})
	h.scans(scanned, []model.StaleTrip{b}, []model.StaleTrip{a}, nil)

	h.ticker.tick(t)
	<-started
	h.ticker.tick(t)
	h.ticker.tick(t)
	<-scanned

	h.c.Cancel(a.ID)
	close(release)
	require.NoError(t, h.c.Shutdown(context.Background()))
	require.EqualValues(t, 2, h.counter(t, metricStaleChecks, resQueued))
	require.EqualValues(t, 1, h.counter(t, metricRequests, resSuccess))
	require.EqualValues(t, 1, h.counter(t, metricRequests, resDropped))
}

// Проверяет переполнение, dropped вместо блокировки сканера
func TestComponent_QueueOverflowDrops(t *testing.T) {
	h := newHarness(t, 1, 1)
	a := model.StaleTrip{ID: uuid.New(), DriverID: uuid.New()}
	b := model.StaleTrip{ID: uuid.New(), DriverID: uuid.New()}
	c := model.StaleTrip{ID: uuid.New(), DriverID: uuid.New()}
	started, release := make(chan struct{}), make(chan struct{})
	blockPush(h.push, b.ID, started, release)
	h.push.EXPECT().RequestPosition(gomock.Any(), a.ID, gomock.Any()).Return(nil)
	scanned := make(chan struct{})
	h.scans(scanned, []model.StaleTrip{b}, []model.StaleTrip{a, c}, nil)

	h.ticker.tick(t)
	<-started
	h.ticker.tick(t)
	h.ticker.tick(t)
	<-scanned

	require.EqualValues(t, 2, h.counter(t, metricStaleChecks, resQueued))
	require.EqualValues(t, 1, h.counter(t, metricStaleChecks, resDropped))
	require.EqualValues(t, 1, h.counter(t, metricRequests, resDropped))
	require.Contains(t, h.logs.String(), "queue full")
	close(release)
	require.NoError(t, h.c.Shutdown(context.Background()))
	require.EqualValues(t, 2, h.counter(t, metricRequests, resSuccess))
}

// Проверяет, что N задач обрабатывают ровно POSITION_WORKERS воркеров
func TestComponent_FixedWorkerCount(t *testing.T) {
	h := newHarness(t, 2, 8)
	trips := make([]model.StaleTrip, 0, 5)
	for i := 0; i < 5; i++ {
		trips = append(trips, model.StaleTrip{ID: uuid.New(), DriverID: uuid.New()})
	}
	var cur, max atomic.Int64
	started, release := make(chan struct{}, 5), make(chan struct{})
	h.push.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).
		DoAndReturn(func(context.Context, uuid.UUID, uuid.UUID) error {
			n := cur.Add(1)
			for m := max.Load(); n > m && !max.CompareAndSwap(m, n); m = max.Load() {
			}
			started <- struct{}{}
			<-release
			cur.Add(-1)
			return nil
		}).Times(5)
	scanned := make(chan struct{})
	h.scans(scanned, trips, nil)

	h.ticker.tick(t)
	<-started
	<-started
	require.EqualValues(t, 2, h.gauge(t, metricActive))
	close(release)
	h.ticker.tick(t)
	<-scanned
	require.NoError(t, h.c.Shutdown(context.Background()))
	require.EqualValues(t, 2, max.Load())
	require.EqualValues(t, 5, h.counter(t, metricRequests, resSuccess))
	require.EqualValues(t, 0, h.gauge(t, metricActive))
}

// Проверяет, что ошибка пуша не роняет воркер, следующая задача обрабатывается
func TestComponent_PushErrorDoesNotKillWorker(t *testing.T) {
	h := newHarness(t, 1, 2)
	a := model.StaleTrip{ID: uuid.New(), DriverID: uuid.New()}
	b := model.StaleTrip{ID: uuid.New(), DriverID: uuid.New()}
	gomock.InOrder(
		h.push.EXPECT().RequestPosition(gomock.Any(), a.ID, a.DriverID).Return(errors.New("push down")),
		h.push.EXPECT().RequestPosition(gomock.Any(), b.ID, b.DriverID).Return(nil),
	)
	scanned := make(chan struct{})
	h.scans(scanned, []model.StaleTrip{a, b}, nil)

	h.ticker.tick(t)
	h.ticker.tick(t)
	<-scanned
	require.NoError(t, h.c.Shutdown(context.Background()))
	require.EqualValues(t, 1, h.counter(t, metricRequests, resError))
	require.EqualValues(t, 1, h.counter(t, metricRequests, resSuccess))
	require.Contains(t, h.logs.String(), "push request failed")
}

// Проверяет что ошибка сканера не останавливает опрос
func TestComponent_ScanErrorKeepsScannerAlive(t *testing.T) {
	h := newHarness(t, 1, 2)
	a := model.StaleTrip{ID: uuid.New(), DriverID: uuid.New()}
	h.push.EXPECT().RequestPosition(gomock.Any(), a.ID, a.DriverID).Return(nil)
	scanned := make(chan struct{})
	gomock.InOrder(
		h.trips.EXPECT().ListStale(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			Return(nil, errors.New("db down")),
		h.trips.EXPECT().ListStale(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).
			DoAndReturn(func(context.Context, time.Duration, *model.StaleCursor, int) ([]model.StaleTrip, error) {
				close(scanned)
				return []model.StaleTrip{a}, nil
			}),
	)
	h.ticker.tick(t)
	h.ticker.tick(t)
	<-scanned
	require.NoError(t, h.c.Shutdown(context.Background()))
	require.EqualValues(t, 1, h.counter(t, metricRequests, resSuccess))
	require.Contains(t, h.logs.String(), "stale trips scan failed")
}

// Проверяет остановку, воркеры дорабатывают очередь, горутин не остается
func TestComponent_ShutdownDrainsQueue(t *testing.T) {
	h := newHarness(t, 1, 4)
	a := model.StaleTrip{ID: uuid.New(), DriverID: uuid.New()}
	h.push.EXPECT().RequestPosition(gomock.Any(), a.ID, a.DriverID).Return(nil)
	scanned := make(chan struct{})
	h.scans(scanned, []model.StaleTrip{a}, nil)
	h.ticker.tick(t)
	h.ticker.tick(t)
	<-scanned
	require.NoError(t, h.c.Shutdown(context.Background()))
	require.EqualValues(t, 1, h.counter(t, metricRequests, resSuccess))
}

// Проверяет принудительную остановку по истечении бюджета
func TestComponent_ShutdownTimeoutCancelsWorkers(t *testing.T) {
	h := newHarness(t, 1, 2)
	a := model.StaleTrip{ID: uuid.New(), DriverID: uuid.New()}
	started, release := make(chan struct{}), make(chan struct{})
	t.Cleanup(func() { close(release) })
	blockPush(h.push, a.ID, started, release)
	scanned := make(chan struct{})
	h.scans(scanned, []model.StaleTrip{a}, nil)
	h.ticker.tick(t)
	<-started
	h.ticker.tick(t)
	<-scanned
	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	require.ErrorIs(t, h.c.Shutdown(ctx), context.DeadlineExceeded)
}

// Проверяет чтение батчами, 250 поездок и батч 100 — три запроса
func TestComponent_ScanReadsAllBatches(t *testing.T) {
	h := newHarnessCfg(t, positions.Config{
		CheckInterval: time.Hour, StaleAfter: time.Minute,
		Workers: 2, QueueSize: 300, PushTimeout: time.Second, BatchSize: 100,
	})
	all := make([]model.StaleTrip, 0, 250)
	for i := 0; i < 250; i++ {
		all = append(all, model.StaleTrip{ID: uuid.New(), DriverID: uuid.New(), LastAt: time.Now()})
	}
	h.push.EXPECT().RequestPosition(gomock.Any(), gomock.Any(), gomock.Any()).Return(nil).Times(250)
	scanned := make(chan struct{})
	gomock.InOrder(
		h.trips.EXPECT().ListStale(gomock.Any(), gomock.Any(), gomock.Any(), 100).Return(all[:100], nil),
		h.trips.EXPECT().ListStale(gomock.Any(), gomock.Any(), gomock.Any(), 100).Return(all[100:200], nil),
		h.trips.EXPECT().ListStale(gomock.Any(), gomock.Any(), gomock.Any(), 100).
			DoAndReturn(func(context.Context, time.Duration, *model.StaleCursor, int) ([]model.StaleTrip, error) {
				close(scanned)
				return all[200:], nil
			}),
	)
	h.ticker.tick(t)
	<-scanned
	require.NoError(t, h.c.Shutdown(context.Background()))
	require.EqualValues(t, 250, h.counterTotal(t, metricRowsRead))
	require.EqualValues(t, 250, h.counter(t, metricStaleChecks, resQueued))
	require.EqualValues(t, 250, h.counter(t, metricRequests, resSuccess))
}

// Проверяет остановку чтения на заполненной очереди, за тик ровно два запроса
func TestComponent_ScanStopsWhenQueueFull(t *testing.T) {
	h := newHarnessCfg(t, positions.Config{
		CheckInterval: time.Hour, StaleAfter: time.Minute,
		Workers: 1, QueueSize: 1, PushTimeout: time.Second, BatchSize: 1,
	})
	blocker := model.StaleTrip{ID: uuid.New(), DriverID: uuid.New(), LastAt: time.Now()}
	a := model.StaleTrip{ID: uuid.New(), DriverID: uuid.New(), LastAt: time.Now()}
	b := model.StaleTrip{ID: uuid.New(), DriverID: uuid.New(), LastAt: time.Now()}
	started, release := make(chan struct{}), make(chan struct{})
	blockPush(h.push, blocker.ID, started, release)
	h.push.EXPECT().RequestPosition(gomock.Any(), a.ID, gomock.Any()).Return(nil)
	scanned := make(chan struct{})
	gomock.InOrder(
		// тик 1 - blocker уходит воркеру, второй запрос пустой, конец обхода
		h.trips.EXPECT().ListStale(gomock.Any(), gomock.Any(), gomock.Any(), 1).Return([]model.StaleTrip{blocker}, nil),
		h.trips.EXPECT().ListStale(gomock.Any(), gomock.Any(), gomock.Any(), 1).Return(nil, nil),
		// тик 2 - a занимает очередь, b дропается, чтение останавливается
		h.trips.EXPECT().ListStale(gomock.Any(), gomock.Any(), gomock.Any(), 1).Return([]model.StaleTrip{a}, nil),
		h.trips.EXPECT().ListStale(gomock.Any(), gomock.Any(), gomock.Any(), 1).Return([]model.StaleTrip{b}, nil),
		// тик 3 - сигнал что второй тик завершен
		h.trips.EXPECT().ListStale(gomock.Any(), gomock.Any(), gomock.Any(), 1).
			DoAndReturn(func(context.Context, time.Duration, *model.StaleCursor, int) ([]model.StaleTrip, error) {
				close(scanned)
				return nil, nil
			}),
	)
	h.ticker.tick(t)
	<-started
	h.ticker.tick(t)
	h.ticker.tick(t)
	<-scanned
	require.EqualValues(t, 3, h.counterTotal(t, metricRowsRead))
	require.EqualValues(t, 2, h.counter(t, metricStaleChecks, resQueued))
	require.EqualValues(t, 1, h.counter(t, metricStaleChecks, resDropped))
	close(release)
	require.NoError(t, h.c.Shutdown(context.Background()))
	require.EqualValues(t, 2, h.counter(t, metricRequests, resSuccess))
}
