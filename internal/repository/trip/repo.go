package trip

import (
	"context"
	"errors"
	"fmt"
	"time"

	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	"github.com/ytkasmerti/go-course-avito/internal/infra/postgres/transaction"
	"github.com/ytkasmerti/go-course-avito/internal/model"
)

type Repo struct {
	pool *pgxpool.Pool
}

func New(pool *pgxpool.Pool) *Repo {
	return &Repo{pool: pool}
}

func (r *Repo) querier(ctx context.Context) interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
} {
	if tx, ok := transaction.TxFromContext(ctx); ok {
		return tx
	}
	return r.pool
}

func (r *Repo) Create(ctx context.Context, t model.Trip) error {
	insertTrip := sq.Insert("trips").
		Columns("id", "user_id", "driver_id",
			"start_latitude", "start_longitude",
			"end_latitude", "end_longitude",
			"price", "status", "started_at").
		Values(t.ID, t.UserID, t.DriverID,
			t.StartLatitude, t.StartLongitude,
			t.EndLatitude, t.EndLongitude,
			t.Price, t.Status, t.StartedAt).
		PlaceholderFormat(sq.Dollar)
	sql, args, err := insertTrip.ToSql()
	if err != nil {
		return fmt.Errorf("build insert trip: %w", err)
	}

	if _, err := r.querier(ctx).Exec(ctx, sql, args...); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "trips_driver_active_uniq" {
			return domaintrip.ErrDriverBusy
		}
		return fmt.Errorf("insert trip: %w", err)
	}

	insertHistory := sq.Insert("trip_status_history").
		Columns("trip_id", "from_status", "to_status", "reason").
		Values(t.ID, nil, t.Status, "trip created").
		PlaceholderFormat(sq.Dollar)
	sql, args, err = insertHistory.ToSql()
	if err != nil {
		return fmt.Errorf("build insert history: %w", err)
	}
	if _, err := r.querier(ctx).Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("insert history: %w", err)
	}
	return nil
}

func (r *Repo) GetByID(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	q := sq.Select("id", "user_id", "driver_id",
		"start_latitude", "start_longitude",
		"end_latitude", "end_longitude",
		"price", "status", "started_at", "finished_at").
		From("trips").
		Where(sq.Eq{"id": id}).
		PlaceholderFormat(sq.Dollar)
	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	var t model.Trip
	err = r.querier(ctx).QueryRow(ctx, sql, args...).Scan(
		&t.ID, &t.UserID, &t.DriverID,
		&t.StartLatitude, &t.StartLongitude,
		&t.EndLatitude, &t.EndLongitude,
		&t.Price, &t.Status, &t.StartedAt, &t.FinishedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domaintrip.ErrTripNotFound
		}
		return nil, fmt.Errorf("select trip: %w", err)
	}
	return &t, nil
}

func (r *Repo) Finish(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	update := sq.Update("trips").
		Set("status", string(domaintrip.StatusCompleted)).
		Set("finished_at", sq.Expr("now()")).
		Set("updated_at", sq.Expr("now()")).
		Where(sq.Eq{"id": id, "status": string(domaintrip.StatusActive)}).
		PlaceholderFormat(sq.Dollar)
	sql, args, err := update.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build update: %w", err)
	}
	tag, err := r.querier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("update trip: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return r.GetByID(ctx, id)
	}

	existing, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing.Status == string(domaintrip.StatusCompleted) {
		return nil, domaintrip.ErrTripCompleted
	}
	return nil, domaintrip.ErrTripNotFound
}

func (r *Repo) GetIdempotencyKey(ctx context.Context, key uuid.UUID) (*model.IdempotencyRecord, error) {
	q := sq.Select("trip_id", "request_hash").
		From("idempotency_keys").
		Where(sq.Eq{"key": key}).
		Where("expires_at > now()").
		PlaceholderFormat(sq.Dollar)
	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select idempotency: %w", err)
	}

	var rec model.IdempotencyRecord
	err = r.querier(ctx).QueryRow(ctx, sql, args...).Scan(&rec.TripID, &rec.RequestHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("select idempotency: %w", err)
	}
	return &rec, nil
}

func (r *Repo) SaveIdempotencyKey(ctx context.Context, key uuid.UUID, hash string, tripID uuid.UUID, ttl time.Duration) error {
	q := sq.Insert("idempotency_keys").
		Columns("key", "request_hash", "trip_id", "expires_at").
		Values(key, hash, tripID, time.Now().Add(ttl)).
		PlaceholderFormat(sq.Dollar)
	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build insert idempotency: %w", err)
	}

	if _, err := r.querier(ctx).Exec(ctx, sql, args...); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "idempotency_keys_pkey" {
			return domaintrip.ErrIdempotencyRace
		}
		return fmt.Errorf("insert idempotency: %w", err)
	}
	return nil
}
