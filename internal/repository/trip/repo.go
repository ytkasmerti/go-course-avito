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

const (
	tableTrips       = "trips"
	tableHistory     = "trip_status_history"
	tableIdempotency = "idempotency_keys"
	tablePositions   = "trip_positions"
	colStatus        = "status"
)

type Repo struct {
	pool         *pgxpool.Pool
	queryTimeout time.Duration
}

func New(pool *pgxpool.Pool, queryTimeout time.Duration) *Repo {
	return &Repo{pool: pool, queryTimeout: queryTimeout}
}

func (r *Repo) querier(ctx context.Context) interface {
	QueryRow(ctx context.Context, sql string, args ...any) pgx.Row
	Exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error)
	Query(ctx context.Context, sql string, args ...any) (pgx.Rows, error)
} {
	if tx, ok := transaction.TxFromContext(ctx); ok {
		return tx
	}
	return r.pool
}

// ограничивают ожидание соединения + SQL через DATABASE_QUERY_TIMEOUT
func (r *Repo) exec(ctx context.Context, sql string, args ...any) (pgconn.CommandTag, error) {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()
	return r.querier(ctx).Exec(ctx, sql, args...)
}

// выполняется внутри, пока контекст с дедлайном еще жив
func (r *Repo) queryRowScan(ctx context.Context, sql string, args []any, dest ...any) error {
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()
	return r.querier(ctx).QueryRow(ctx, sql, args...).Scan(dest...)
}

func (r *Repo) Create(ctx context.Context, t model.Trip) error {
	insertTrip := sq.Insert(tableTrips).
		Columns("id", "user_id", "driver_id",
			"start_latitude", "start_longitude",
			"end_latitude", "end_longitude",
			"price", colStatus, "started_at").
		Values(t.ID, t.UserID, t.DriverID,
			t.StartLatitude, t.StartLongitude,
			t.EndLatitude, t.EndLongitude,
			t.Price, t.Status, t.StartedAt).
		PlaceholderFormat(sq.Dollar)

	sql, args, err := insertTrip.ToSql()
	if err != nil {
		return fmt.Errorf("build insert trip: %w", err)
	}

	if _, err := r.exec(ctx, sql, args...); err != nil {
		var pgErr *pgconn.PgError
		if errors.As(err, &pgErr) && pgErr.Code == "23505" && pgErr.ConstraintName == "trips_driver_active_uniq" {
			return domaintrip.ErrDriverBusy
		}
		return fmt.Errorf("insert trip: %w", err)
	}

	insertHistory := sq.Insert(tableHistory).
		Columns("trip_id", "from_status", "to_status", "reason").
		Values(t.ID, nil, t.Status, "trip created").
		PlaceholderFormat(sq.Dollar)

	sql, args, err = insertHistory.ToSql()
	if err != nil {
		return fmt.Errorf("build insert history: %w", err)
	}

	if _, err := r.exec(ctx, sql, args...); err != nil {
		return fmt.Errorf("insert history: %w", err)
	}
	return nil
}

func (r *Repo) GetByID(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	q := sq.Select("id", "user_id", "driver_id",
		"start_latitude", "start_longitude",
		"end_latitude", "end_longitude",
		"price", colStatus, "started_at", "finished_at",
		"last_position_at").
		From(tableTrips).
		Where(sq.Eq{"id": id}).
		PlaceholderFormat(sq.Dollar)

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select: %w", err)
	}

	var t model.Trip
	err = r.queryRowScan(ctx, sql, args,
		&t.ID, &t.UserID, &t.DriverID,
		&t.StartLatitude, &t.StartLongitude,
		&t.EndLatitude, &t.EndLongitude,
		&t.Price, &t.Status, &t.StartedAt, &t.FinishedAt,
		&t.LastPositionAt,
	)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, domaintrip.ErrTripNotFound
		}
		return nil, fmt.Errorf("select trip: %w", err)
	}
	return &t, nil
}

// сохраняет результат доменного Trip.Finish
// в UPDATE защищает от конкурентного завершения, второй запрос не найдет строку
func (r *Repo) SaveFinished(ctx context.Context, id uuid.UUID, finishedAt time.Time) (*model.Trip, error) {
	update := sq.Update(tableTrips).
		Set(colStatus, string(domaintrip.StatusCompleted)).
		Set("finished_at", finishedAt).
		Set("updated_at", sq.Expr("now()")).
		Where(sq.Eq{"id": id, colStatus: string(domaintrip.StatusActive)}).
		PlaceholderFormat(sq.Dollar)

	sql, args, err := update.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build update: %w", err)
	}

	tag, err := r.exec(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("update trip: %w", err)
	}

	if tag.RowsAffected() == 1 {
		insertHistory := sq.Insert(tableHistory).
			Columns("trip_id", "from_status", "to_status", "reason").
			Values(id, string(domaintrip.StatusActive), string(domaintrip.StatusCompleted), "trip finished").
			PlaceholderFormat(sq.Dollar)

		hSQL, hArgs, err := insertHistory.ToSql()
		if err != nil {
			return nil, fmt.Errorf("build insert history: %w", err)
		}
		if _, err := r.exec(ctx, hSQL, hArgs...); err != nil {
			return nil, fmt.Errorf("insert history: %w", err)
		}
		return r.GetByID(ctx, id)
	}

	// Ноль строк - поездки нет либо ее уже завершил параллельный запрос
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
		From(tableIdempotency).
		Where(sq.Eq{"key": key}).
		Where("expires_at > now()").
		PlaceholderFormat(sq.Dollar)

	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select idempotency: %w", err)
	}

	var rec model.IdempotencyRecord
	err = r.queryRowScan(ctx, sql, args, &rec.TripID, &rec.RequestHash)
	if err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("select idempotency: %w", err)
	}
	return &rec, nil
}

// Вставляет ключ или заменяет просроченный, возвращает ErrIdempotencyRace при гонке
func (r *Repo) SaveIdempotencyKey(ctx context.Context, key uuid.UUID, hash string, tripID uuid.UUID, ttl time.Duration) error {
	q := sq.Insert(tableIdempotency).
		Columns("key", "request_hash", "trip_id", "expires_at").
		Values(key, hash, tripID, sq.Expr("now() + make_interval(secs => ?::double precision)", ttl.Seconds())).
		Suffix(`ON CONFLICT (key) DO UPDATE SET
			request_hash = EXCLUDED.request_hash,
			trip_id      = EXCLUDED.trip_id,
			created_at   = now(),
			expires_at   = EXCLUDED.expires_at
			WHERE idempotency_keys.expires_at <= now()`).
		PlaceholderFormat(sq.Dollar)

	sql, args, err := q.ToSql()
	if err != nil {
		return fmt.Errorf("build insert idempotency: %w", err)
	}

	tag, err := r.exec(ctx, sql, args...)
	if err != nil {
		return fmt.Errorf("insert idempotency: %w", err)
	}
	if tag.RowsAffected() == 0 {
		return domaintrip.ErrIdempotencyRace
	}
	return nil
}

// Читает один батч устаревших поездок, продолжая с курсора (время, id)
// Условие по времени считается в SQL
func (r *Repo) ListStale(ctx context.Context, staleAfter time.Duration, after *model.StaleCursor, limit int) ([]model.StaleTrip, error) {
	if limit <= 0 {
		return nil, fmt.Errorf("stale batch limit must be > 0, got %d", limit)
	}
	q := sq.Select("id", "driver_id", "COALESCE(last_position_at, started_at) AS last_at").
		From(tableTrips).
		Where(sq.Eq{colStatus: string(domaintrip.StatusActive)}).
		Where("COALESCE(last_position_at, started_at) < now() - make_interval(secs => ?::double precision)", staleAfter.Seconds()).
		OrderBy("last_at", "id").
		Limit(uint64(limit)).
		PlaceholderFormat(sq.Dollar)
	if after != nil {
		q = q.Where("(COALESCE(last_position_at, started_at), id) > (?, ?)", after.At, after.ID)
	}
	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select stale: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()
	rows, err := r.querier(ctx).Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("select stale: %w", err)
	}
	defer rows.Close()
	res := make([]model.StaleTrip, 0, limit)
	for rows.Next() {
		var t model.StaleTrip
		if err := rows.Scan(&t.ID, &t.DriverID, &t.LastAt); err != nil {
			return nil, fmt.Errorf("scan stale: %w", err)
		}
		res = append(res, t)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("stale rows: %w", err)
	}
	return res, nil
}

// Вызывается внутри TxManager, UPDATE и INSERT в одной транзакции
// Старая координата не состаривает поездку, сравнение в самом UPDATE
func (r *Repo) SavePosition(ctx context.Context, p model.Position) (*model.Position, error) {
	update := sq.Update(tableTrips).
		Set("last_position_at", p.RecordedAt).
		Set("updated_at", sq.Expr("now()")).
		Where(sq.Eq{"id": p.TripID, colStatus: string(domaintrip.StatusActive)}).
		Where("(last_position_at IS NULL OR last_position_at < ?)", p.RecordedAt).
		PlaceholderFormat(sq.Dollar)
	sql, args, err := update.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build update last_position_at: %w", err)
	}
	tag, err := r.exec(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("update last_position_at: %w", err)
	}
	if tag.RowsAffected() == 0 {
		// ноль строк - поездки нет, она завершена  либо координата опоздала
		status, err := r.tripStatus(ctx, p.TripID)
		if err != nil {
			return nil, err
		}
		if status != string(domaintrip.StatusActive) {
			return nil, domaintrip.ErrTripCompleted
		}
	}
	insert := sq.Insert(tablePositions).
		Columns("trip_id", "latitude", "longitude", "recorded_at").
		Values(p.TripID, p.Latitude, p.Longitude, p.RecordedAt).
		Suffix("RETURNING id").
		PlaceholderFormat(sq.Dollar)
	sql, args, err = insert.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build insert position: %w", err)
	}
	if err := r.queryRowScan(ctx, sql, args, &p.ID); err != nil {
		return nil, fmt.Errorf("insert position: %w", err)
	}
	return &p, nil
}

func (r *Repo) tripStatus(ctx context.Context, tripID uuid.UUID) (string, error) {
	q := sq.Select(colStatus).From(tableTrips).
		Where(sq.Eq{"id": tripID}).
		PlaceholderFormat(sq.Dollar)
	sql, args, err := q.ToSql()
	if err != nil {
		return "", fmt.Errorf("build select status: %w", err)
	}
	var status string
	if err := r.queryRowScan(ctx, sql, args, &status); err != nil {
		if errors.Is(err, pgx.ErrNoRows) {
			return "", domaintrip.ErrTripNotFound
		}
		return "", fmt.Errorf("select status: %w", err)
	}
	return status, nil
}

// отдает весь маршрут, сортировка под индекс trip_positions_trip_recorded_idx
func (r *Repo) ListPositions(ctx context.Context, tripID uuid.UUID) ([]model.Position, error) {
	q := sq.Select("id", "trip_id", "latitude", "longitude", "recorded_at").
		From(tablePositions).
		Where(sq.Eq{"trip_id": tripID}).
		OrderBy("recorded_at", "id").
		PlaceholderFormat(sq.Dollar)
	sql, args, err := q.ToSql()
	if err != nil {
		return nil, fmt.Errorf("build select positions: %w", err)
	}
	ctx, cancel := context.WithTimeout(ctx, r.queryTimeout)
	defer cancel()
	rows, err := r.querier(ctx).Query(ctx, sql, args...)
	if err != nil {
		return nil, fmt.Errorf("select positions: %w", err)
	}
	defer rows.Close()
	var res []model.Position
	for rows.Next() {
		var p model.Position
		if err := rows.Scan(&p.ID, &p.TripID, &p.Latitude, &p.Longitude, &p.RecordedAt); err != nil {
			return nil, fmt.Errorf("scan position: %w", err)
		}
		res = append(res, p)
	}
	if err := rows.Err(); err != nil {
		return nil, fmt.Errorf("positions rows: %w", err)
	}
	return res, nil
}
