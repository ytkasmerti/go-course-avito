package trip

import (
	"context"
	"errors"
	"fmt"
	sq "github.com/Masterminds/squirrel"
	"github.com/google/uuid"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"

	"github.com/ytkasmerti/go-course-avito/internal/txmanager"
)

type Repository struct {
	pool *pgxpool.Pool
}

//если есть транзакция в контексте, возвращаем ее, иначе возрващаем пул
func (r *Repository) querier(ctx context.Context) interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
} {
	if tx, ok := txmanager.TxFromContext(ctx); ok {
		return tx
	}
	return r.pool
}

func (r *Repository) Create(ctx context.Context, t *Trip) error {
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
		return fmt.Errorf("Build insert trip: %w", err)
	}

	if _, err := r.querier(ctx).Exec(ctx, sql, args...); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" {
			return ErrDriverBusy
		}
		return fmt.Errorf("Insert trip: %w", err)
	}

	insertHistory := sq.Insert("trip_status_history").
		Columns("trip_id", "from_status", "to_status", "reason").
		Values(t.ID, nil, string(t.Status), "trip created").
		PlaceholderFormat(sq.Dollar)

	sql, args, err = insertHistory.ToSql()
	if err != nil {
		return fmt.Errorf("Build insert history: %w", err)
	}
	if _, err := r.querier(ctx).Exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("Insert history: %w", err)
	}
	return nil
}

func (r *Repository) GetByID(ctx context.Context, id uuid.UUID) (*Trip, error) {
	q := sq.Select("id", "user_id", "driver_id",
		"start_latitude", "start_longitude",
		"end_latitude", "end_longitude",
		"price", "status", "started_at", "finished_at").
		From("trips").
		Where(sq.Eq{"id": id}).
		PlaceholderFormat(sq.Dollar)
	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("Build select: %w", err)
	}

	var t Trip
	err = r.querier(ctx).QueryRow(ctx, sql, args...).Scan(
		&t.ID, &t.UserID, &t.DriverID,
		&t.StartLatitude, &t.StartLongitude,
		&t.EndLatitude, &t.EndLongitude,
		&t.Price, &t.Status, &t.StartedAt, &t.FinishedAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, ErrTripNotFound
		}
		return nil, fmt.Errorf("Select trip: %w", err)
	}
	return &t, nil
}

func (r *Repository) Finish(ctx context.Context, id uuid.UUID) (*Trip, error) {
	update := sq.Update("trips").
		Set("status", StatusCompleted).
		Set("finished_at", sq.Expr("now()")).
		Set("updated_at", sq.Expr("now()")).
		Where(sq.Eq{"id": id, "status": StatusActive}).
		PlaceholderFormat(sq.Dollar)

	sql, args, err := update.ToSql()
	if err != nil {
		return nil, fmt.Errorf("Build update: %w", err)
	}
	tag, err := r.querier(ctx).Exec(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("Update trip: %w", err)
	}
	if tag.RowsAffected() == 1 {
		return r.GetByID(ctx, id)
	}

	existing, err := r.GetByID(ctx, id)
	if err != nil {
		return nil, err
	}
	if existing.Status == StatusCompleted {
		return nil, ErrTripCompleted
	}

	return nil, ErrTripNotFound
}