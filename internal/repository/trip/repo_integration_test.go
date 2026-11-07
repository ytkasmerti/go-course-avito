//go:build integration

package trip_test

import (
	"context"
	"errors"
	"os"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"github.com/ytkasmerti/go-course-avito/internal/config"
	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	"github.com/ytkasmerti/go-course-avito/internal/infra/postgres"
	"github.com/ytkasmerti/go-course-avito/internal/infra/postgres/transaction"
	"github.com/ytkasmerti/go-course-avito/internal/model"
	repoTrip "github.com/ytkasmerti/go-course-avito/internal/repository/trip"
)

const testQueryTimeout = 5 * time.Second

func newPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Fatalf("pool: %v", err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func newTrip(driverID uuid.UUID) model.Trip {
	return model.Trip{
		ID:             uuid.New(),
		UserID:         uuid.New(),
		DriverID:       driverID,
		StartLatitude:  59.9,
		StartLongitude: 30.3,
		EndLatitude:    59.8,
		EndLongitude:   30.2,
		Price:          1450,
		Status:         string(domaintrip.StatusActive),
		StartedAt:      time.Now(),
	}
}

func cleanup(t *testing.T, pool *pgxpool.Pool, tripID uuid.UUID) {
	t.Helper()
	t.Cleanup(func() {
		ctx := context.Background()
		_, _ = pool.Exec(ctx, "DELETE FROM trip_status_history WHERE trip_id = $1", tripID)
		_, _ = pool.Exec(ctx, "DELETE FROM trips WHERE id = $1", tripID)
	})
}

func rowCounts(t *testing.T, pool *pgxpool.Pool, tripID uuid.UUID) (trips, history int) {
	t.Helper()
	ctx := context.Background()
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM trips WHERE id = $1", tripID).Scan(&trips); err != nil {
		t.Fatalf("count trips: %v", err)
	}
	if err := pool.QueryRow(ctx, "SELECT count(*) FROM trip_status_history WHERE trip_id = $1", tripID).Scan(&history); err != nil {
		t.Fatalf("count history: %v", err)
	}
	return trips, history
}

// Проверяет вставку и чтение поездки
func TestRepo_Create_Success(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool, testQueryTimeout)
	ctx := context.Background()

	tr := newTrip(uuid.New())
	cleanup(t, pool, tr.ID)
	if err := repo.Create(ctx, tr); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.GetByID(ctx, tr.ID)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got.ID != tr.ID {
		t.Errorf("id: got %v, want %v", got.ID, tr.ID)
	}
}

// Проверяет уникальный индекс активного водителя
func TestRepo_Create_DriverBusy(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool, testQueryTimeout)
	ctx := context.Background()

	driverID := uuid.New()
	t1 := newTrip(driverID)
	cleanup(t, pool, t1.ID)
	if err := repo.Create(ctx, t1); err != nil {
		t.Fatalf("create t1: %v", err)
	}

	t2 := newTrip(driverID)
	cleanup(t, pool, t2.ID)
	err := repo.Create(ctx, t2)
	if !errors.Is(err, domaintrip.ErrDriverBusy) {
		t.Fatalf("want ErrDriverBusy, got %v", err)
	}
}

// Проверяет завершение и повторное завершение
func TestRepo_Finish_Success(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool, testQueryTimeout)
	ctx := context.Background()

	tr := newTrip(uuid.New())
	cleanup(t, pool, tr.ID)
	if err := repo.Create(ctx, tr); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.SaveFinished(ctx, tr.ID, time.Now().UTC())
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if got.Status != string(domaintrip.StatusCompleted) {
		t.Errorf("status: got %v, want completed", got.Status)
	}

	_, err = repo.SaveFinished(ctx, tr.ID, time.Now().UTC())
	if !errors.Is(err, domaintrip.ErrTripCompleted) {
		t.Errorf("want ErrTripCompleted, got %v", err)
	}
}

func historyRows(t *testing.T, pool *pgxpool.Pool, tripID uuid.UUID) [][2]string {
	t.Helper()
	rows, err := pool.Query(context.Background(),
		`SELECT COALESCE(from_status, ''), to_status FROM trip_status_history WHERE trip_id = $1 ORDER BY id`, tripID)
	require.NoError(t, err)
	defer rows.Close()

	var res [][2]string
	for rows.Next() {
		var from, to string
		require.NoError(t, rows.Scan(&from, &to))
		res = append(res, [2]string{from, to})
	}
	require.NoError(t, rows.Err())
	return res
}

// Проверяет запись в историю статусов
func TestRepo_Finish_WritesStatusHistory(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool, testQueryTimeout)
	txManager := transaction.New(pool, testQueryTimeout)
	ctx := context.Background()
	tr := newTrip(uuid.New())
	cleanup(t, pool, tr.ID)
	require.NoError(t, repo.Create(ctx, tr))

	err := txManager.Do(ctx, func(ctx context.Context) error {
		_, err := repo.SaveFinished(ctx, tr.ID, time.Now().UTC())
		return err
	})
	require.NoError(t, err)
	require.Equal(t, [][2]string{{"", "active"}, {"active", "completed"}}, historyRows(t, pool, tr.ID))

	// повторное завершение не добавляет запись в историю
	err = txManager.Do(ctx, func(ctx context.Context) error {
		_, err := repo.SaveFinished(ctx, tr.ID, time.Now().UTC())
		return err
	})
	require.ErrorIs(t, err, domaintrip.ErrTripCompleted)
	require.Len(t, historyRows(t, pool, tr.ID), 2)
}

// Проверяет откат статуса и истории
func TestRepo_Finish_TxRollbackRevertsStatusAndHistory(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool, testQueryTimeout)
	txManager := transaction.New(pool, testQueryTimeout)
	ctx := context.Background()
	tr := newTrip(uuid.New())
	cleanup(t, pool, tr.ID)
	require.NoError(t, repo.Create(ctx, tr))

	errAbort := context.Canceled // любая ошибка после успешного Finish
	err := txManager.Do(ctx, func(ctx context.Context) error {
		if _, err := repo.SaveFinished(ctx, tr.ID, time.Now().UTC()); err != nil {
			return err
		}
		return errAbort
	})
	require.ErrorIs(t, err, errAbort)

	got, err := repo.GetByID(ctx, tr.ID)
	require.NoError(t, err)
	require.Equal(t, string(domaintrip.StatusActive), got.Status)
	require.Nil(t, got.FinishedAt)
	require.Len(t, historyRows(t, pool, tr.ID), 1)
}

// Проверяет сохранение и конфликт ключа
func TestRepo_IdempotencyKey(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool, testQueryTimeout)
	ctx := context.Background()

	tr := newTrip(uuid.New())
	cleanup(t, pool, tr.ID)
	if err := repo.Create(ctx, tr); err != nil {
		t.Fatalf("create: %v", err)
	}

	key := uuid.New()
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM idempotency_keys WHERE key = $1", key) })
	if err := repo.SaveIdempotencyKey(ctx, key, "hash", tr.ID, time.Hour); err != nil {
		t.Fatalf("save: %v", err)
	}

	got, err := repo.GetIdempotencyKey(ctx, key)
	if err != nil {
		t.Fatalf("get: %v", err)
	}
	if got == nil || got.TripID != tr.ID {
		t.Fatalf("want trip %v, got %v", tr.ID, got)
	}

	err = repo.SaveIdempotencyKey(ctx, key, "hash", tr.ID, time.Hour)
	if !errors.Is(err, domaintrip.ErrIdempotencyRace) {
		t.Fatalf("want ErrIdempotencyRace, got %v", err)
	}
}

// Проверяет замену просроченного ключа
func TestRepo_SaveIdempotencyKey_ReplacesExpiredKey(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool, testQueryTimeout)
	ctx := context.Background()
	first, second := newTrip(uuid.New()), newTrip(uuid.New())
	cleanup(t, pool, first.ID)
	cleanup(t, pool, second.ID)
	require.NoError(t, repo.Create(ctx, first))
	require.NoError(t, repo.Create(ctx, second))

	key := uuid.New()
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), "DELETE FROM idempotency_keys WHERE key = $1", key) })

	require.NoError(t, repo.SaveIdempotencyKey(ctx, key, "hash-1", first.ID, 24*time.Hour))

	// «проходит 24 часа»
	_, err := pool.Exec(ctx, "UPDATE idempotency_keys SET expires_at = now() - interval '1 second' WHERE key = $1", key)
	require.NoError(t, err)

	rec, err := repo.GetIdempotencyKey(ctx, key)
	require.NoError(t, err)
	require.Nil(t, rec, "просроченный ключ не должен читаться")

	// повторное использование ключа после TTL: запись заменяется, а не падает на PRIMARY
	require.NoError(t, repo.SaveIdempotencyKey(ctx, key, "hash-2", second.ID, 24*time.Hour))

	rec, err = repo.GetIdempotencyKey(ctx, key)
	require.NoError(t, err)
	require.NotNil(t, rec)
	require.Equal(t, second.ID, rec.TripID)
	require.Equal(t, "hash-2", rec.RequestHash)

	var renewed bool
	require.NoError(t, pool.QueryRow(ctx,
		"SELECT expires_at > now() + interval '23 hours' FROM idempotency_keys WHERE key = $1", key).Scan(&renewed))
	require.True(t, renewed, "TTL должен начаться заново")
}

// Проверяет, что коммит пишет в обе таблицы
func TestRepo_Create_TxCommitWritesBothTables(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool, testQueryTimeout)
	txManager := transaction.New(pool, testQueryTimeout)
	ctx := context.Background()

	tr := newTrip(uuid.New())
	cleanup(t, pool, tr.ID)
	err := txManager.Do(ctx, func(ctx context.Context) error {
		return repo.Create(ctx, tr)
	})
	if err != nil {
		t.Fatalf("tx: %v", err)
	}

	trips, history := rowCounts(t, pool, tr.ID)
	if trips != 1 || history != 1 {
		t.Fatalf("after commit: trips=%d history=%d, want 1 and 1", trips, history)
	}
}

// Проверяет откат обеих таблиц
func TestRepo_Create_TxRollbackRevertsBothTables(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool, testQueryTimeout)
	txManager := transaction.New(pool, testQueryTimeout)
	ctx := context.Background()

	tr := newTrip(uuid.New())
	cleanup(t, pool, tr.ID)
	errAbort := errors.New("abort after insert")
	err := txManager.Do(ctx, func(ctx context.Context) error {
		if err := repo.Create(ctx, tr); err != nil {
			return err
		}
		return errAbort
	})
	if !errors.Is(err, errAbort) {
		t.Fatalf("want errAbort, got %v", err)
	}

	trips, history := rowCounts(t, pool, tr.ID)
	if trips != 0 || history != 0 {
		t.Fatalf("after rollback: trips=%d history=%d, want 0 and 0", trips, history)
	}
}

// Проверяет таймаут ожидания соединения
func TestRepo_QueryTimeout_PoolExhausted(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}
	ctx := context.Background()
	cfg, err := pgxpool.ParseConfig(dsn)
	require.NoError(t, err)
	cfg.MaxConns = 1
	pool, err := pgxpool.NewWithConfig(ctx, cfg)
	require.NoError(t, err)
	t.Cleanup(pool.Close)

	conn, err := pool.Acquire(ctx) // единственное соединение занято
	require.NoError(t, err)
	t.Cleanup(conn.Release)

	repo := repoTrip.New(pool, 100*time.Millisecond)
	start := time.Now()
	_, err = repo.GetByID(ctx, uuid.New())
	require.ErrorIs(t, err, context.DeadlineExceeded)
	require.Less(t, time.Since(start), 2*time.Second)
}

const (
	opInsert          = "INSERT"
	tableTrips        = "trips"
	pgUniqueViolation = "23505"
)

func attrsOf(kvs []attribute.KeyValue) map[string]string {
	m := make(map[string]string, len(kvs))
	for _, kv := range kvs {
		m[string(kv.Key)] = kv.Value.String()
	}
	return m
}

// ожидаемые атрибуты INSERT в trips, errType пустой для успешного запроса
func insertTripsAttrs(errType string) map[string]string {
	m := map[string]string{
		"db.system.name":     "postgresql",
		"db.operation.name":  opInsert,
		"db.collection.name": tableTrips,
	}
	if errType != "" {
		m["error.type"] = errType
	}
	return m
}

// Проверяет атрибуты БД в спанах и метриках
func TestPool_Telemetry_ExportsDBAttributes(t *testing.T) {
	dsn := os.Getenv("DATABASE_URL")
	if dsn == "" {
		t.Skip("DATABASE_URL not set")
	}

	spans := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans))
	otel.SetTracerProvider(tp)
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))

	ctx, root := tp.Tracer("test").Start(context.Background(), "test-root")
	defer root.End()

	pool, err := postgres.NewPool(ctx, config.Config{
		DatabaseURL:             dsn,
		DatabaseMaxConns:        4,
		DatabaseMaxConnLifetime: time.Hour,
		DatabaseConnectTimeout:  5 * time.Second,
		DatabaseQueryTimeout:    5 * time.Second,
	})
	require.NoError(t, err)
	t.Cleanup(pool.Close)
	repo := repoTrip.New(pool, testQueryTimeout)

	driver := uuid.New()
	first, second := newTrip(driver), newTrip(driver)
	cleanup(t, pool, first.ID)
	cleanup(t, pool, second.ID)
	require.NoError(t, repo.Create(ctx, first))
	require.ErrorIs(t, repo.Create(ctx, second), domaintrip.ErrDriverBusy)

	var okInsert, failedInsert bool
	for _, s := range spans.Ended() {
		a := attrsOf(s.Attributes())
		if a["db.operation.name"] != opInsert || a["db.collection.name"] != tableTrips {
			continue
		}
		require.Equal(t, "postgresql", a["db.system.name"])
		if a["error.type"] == pgUniqueViolation {
			failedInsert = true
			require.Equal(t, codes.Error, s.Status().Code)
		} else {
			okInsert = true
		}
	}
	require.True(t, okInsert, "нет спана успешного INSERT с атрибутами")
	require.True(t, failedInsert, "нет спана неудачного INSERT с error.type=23505")

	var rm metricdata.ResourceMetrics
	require.NoError(t, reader.Collect(ctx, &rm))
	var points []map[string]string
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "db.client.operation.duration" {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			for _, dp := range hist.DataPoints {
				points = append(points, attrsOf(dp.Attributes.ToSlice()))
			}
		}
	}
	require.Contains(t, points, insertTripsAttrs(""))
	require.Contains(t, points, insertTripsAttrs(pgUniqueViolation))
}
