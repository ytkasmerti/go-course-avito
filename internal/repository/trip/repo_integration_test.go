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

func TestRepo_Create_Success(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool)
	ctx := context.Background()

	tr := model.Trip{
		ID:             uuid.New(),
		UserID:         uuid.New(),
		DriverID:       uuid.New(),
		StartLatitude:  59.9,
		StartLongitude: 30.3,
		EndLatitude:    59.8,
		EndLongitude:   30.2,
		Price:          1450,
		Status:         string(domaintrip.StatusActive),
		StartedAt:      time.Now(),
	}

	if err := repo.Create(ctx, tr); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM trips WHERE id = $1", tr.ID)
	})

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
	t1 := model.Trip{
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
	if err := repo.Create(ctx, t1); err != nil {
		t.Fatalf("create t1: %v", err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, "DELETE FROM trips WHERE id = $1", t1.ID)
	})

	t2 := t1
	t2.ID = uuid.New()
	err := repo.Create(ctx, t2)
	if !errors.Is(err, domaintrip.ErrDriverBusy) {
		t.Fatalf("want ErrDriverBusy, got %v", err)
	}
}

func TestRepo_Finish_Success(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool)
	ctx := context.Background()

	tr := model.Trip{
		ID: uuid.New(), UserID: uuid.New(), DriverID: uuid.New(),
		StartLatitude: 59.9, StartLongitude: 30.3, EndLatitude: 59.8, EndLongitude: 30.2,
		Price: 1450, Status: string(domaintrip.StatusActive), StartedAt: time.Now(),
	}
	if err := repo.Create(ctx, tr); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM trips WHERE id = $1", tr.ID) })

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

	tr := model.Trip{
		ID: uuid.New(), UserID: uuid.New(), DriverID: uuid.New(),
		StartLatitude: 59.9, StartLongitude: 30.3, EndLatitude: 59.8, EndLongitude: 30.2,
		Price: 1450, Status: string(domaintrip.StatusActive), StartedAt: time.Now(),
	}
	if err := repo.Create(ctx, tr); err != nil {
		t.Fatalf("create: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM trips WHERE id = $1", tr.ID) })

	key := uuid.New()
	if err := repo.SaveIdempotencyKey(ctx, key, "hash", tr.ID, time.Hour); err != nil {
		t.Fatalf("save: %v", err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(ctx, "DELETE FROM idempotency_keys WHERE key = $1", key) })

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
