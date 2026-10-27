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

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	"github.com/ytkasmerti/go-course-avito/internal/infra/postgres/transaction"
	"github.com/ytkasmerti/go-course-avito/internal/model"
	repoTrip "github.com/ytkasmerti/go-course-avito/internal/repository/trip"
)

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

func TestRepo_Create_Success(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool)
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

func TestRepo_Create_DriverBusy(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool)
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

func TestRepo_Finish_Success(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool)
	ctx := context.Background()

	tr := newTrip(uuid.New())
	cleanup(t, pool, tr.ID)
	if err := repo.Create(ctx, tr); err != nil {
		t.Fatalf("create: %v", err)
	}

	got, err := repo.Finish(ctx, tr.ID)
	if err != nil {
		t.Fatalf("finish: %v", err)
	}
	if got.Status != string(domaintrip.StatusCompleted) {
		t.Errorf("status: got %v, want completed", got.Status)
	}

	_, err = repo.Finish(ctx, tr.ID)
	if !errors.Is(err, domaintrip.ErrTripCompleted) {
		t.Errorf("want ErrTripCompleted, got %v", err)
	}
}

func TestRepo_IdempotencyKey(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool)
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

func TestRepo_Create_TxCommitWritesBothTables(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool)
	txManager := transaction.New(pool)
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

func TestRepo_Create_TxRollbackRevertsBothTables(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool)
	txManager := transaction.New(pool)
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
