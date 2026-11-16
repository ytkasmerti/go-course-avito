//go:build integration

package trip_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	"github.com/ytkasmerti/go-course-avito/internal/infra/postgres/transaction"
	"github.com/ytkasmerti/go-course-avito/internal/model"
	repoTrip "github.com/ytkasmerti/go-course-avito/internal/repository/trip"
)

func positionOf(tripID uuid.UUID, at time.Time) model.Position {
	return model.Position{TripID: tripID, Latitude: 59.93, Longitude: 30.33, RecordedAt: at}
}

func lastPositionAt(t *testing.T, pool *pgxpool.Pool, tripID uuid.UUID) *time.Time {
	t.Helper()
	var at *time.Time
	require.NoError(t, pool.QueryRow(context.Background(),
		"SELECT last_position_at FROM trips WHERE id = $1", tripID).Scan(&at))
	return at
}

// Проверяет, что сохранение обновляет last_position_at
func TestRepo_SavePosition_UpdatesLastPositionAt(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool, testQueryTimeout)
	tr := newTrip(uuid.New())
	cleanup(t, pool, tr.ID)
	require.NoError(t, repo.Create(context.Background(), tr))
	at := time.Now().UTC().Truncate(time.Microsecond)
	saved, err := repo.SavePosition(context.Background(), positionOf(tr.ID, at))
	require.NoError(t, err)
	require.Positive(t, saved.ID)
	got := lastPositionAt(t, pool, tr.ID)
	require.NotNil(t, got)
	require.True(t, got.Equal(at))
}

// Проверяет что опоздавшая координата не состаривает поездку, но попадает в маршрут
func TestRepo_SavePosition_OldPositionDoesNotAgeTrip(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool, testQueryTimeout)
	tr := newTrip(uuid.New())
	cleanup(t, pool, tr.ID)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, tr))
	newer := time.Now().UTC().Truncate(time.Microsecond)
	older := newer.Add(-time.Hour)
	_, err := repo.SavePosition(ctx, positionOf(tr.ID, newer))
	require.NoError(t, err)
	_, err = repo.SavePosition(ctx, positionOf(tr.ID, older))
	require.NoError(t, err)
	require.True(t, lastPositionAt(t, pool, tr.ID).Equal(newer))
	list, err := repo.ListPositions(ctx, tr.ID)
	require.NoError(t, err)
	require.Len(t, list, 2)
	require.True(t, list[0].RecordedAt.Equal(older))
}

// Проверяет 409 и 404 на уровне репозитория
func TestRepo_SavePosition_CompletedOrMissing(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool, testQueryTimeout)
	ctx := context.Background()
	tr := newTrip(uuid.New())
	cleanup(t, pool, tr.ID)
	require.NoError(t, repo.Create(ctx, tr))
	_, err := repo.SaveFinished(ctx, tr.ID, time.Now().UTC())
	require.NoError(t, err)
	_, err = repo.SavePosition(ctx, positionOf(tr.ID, time.Now().UTC()))
	require.ErrorIs(t, err, domaintrip.ErrTripCompleted)
	list, err := repo.ListPositions(ctx, tr.ID)
	require.NoError(t, err)
	require.Empty(t, list)
	_, err = repo.SavePosition(ctx, positionOf(uuid.New(), time.Now().UTC()))
	require.ErrorIs(t, err, domaintrip.ErrTripNotFound)
}

// Проверяет атомарный откат точки и last_position_at
func TestRepo_SavePosition_TxRollback(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool, testQueryTimeout)
	tx := transaction.New(pool, testQueryTimeout)
	tr := newTrip(uuid.New())
	cleanup(t, pool, tr.ID)
	require.NoError(t, repo.Create(context.Background(), tr))
	errAbort := errors.New("abort after save")
	err := tx.Do(context.Background(), func(ctx context.Context) error {
		if _, err := repo.SavePosition(ctx, positionOf(tr.ID, time.Now().UTC())); err != nil {
			return err
		}
		return errAbort
	})
	require.ErrorIs(t, err, errAbort)
	require.Nil(t, lastPositionAt(t, pool, tr.ID))
	list, err := repo.ListPositions(context.Background(), tr.ID)
	require.NoError(t, err)
	require.Empty(t, list)
}

// Проверяет сортировку маршрута по recorded_at, затем id
func TestRepo_ListPositions_OrderByRecordedAtThenID(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool, testQueryTimeout)
	tr := newTrip(uuid.New())
	cleanup(t, pool, tr.ID)
	ctx := context.Background()
	require.NoError(t, repo.Create(ctx, tr))
	base := time.Now().UTC().Truncate(time.Microsecond)
	first, err := repo.SavePosition(ctx, positionOf(tr.ID, base))
	require.NoError(t, err)
	second, err := repo.SavePosition(ctx, positionOf(tr.ID, base))
	require.NoError(t, err)
	early, err := repo.SavePosition(ctx, positionOf(tr.ID, base.Add(-time.Minute)))
	require.NoError(t, err)
	list, err := repo.ListPositions(ctx, tr.ID)
	require.NoError(t, err)
	require.Equal(t, []int64{early.ID, first.ID, second.ID},
		[]int64{list[0].ID, list[1].ID, list[2].ID})
}

// Проверяет выборку, свежие и завершенные не попадают, без координат попадает, порядок от самых старых
func TestRepo_ListStale(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool, testQueryTimeout)
	ctx := context.Background()
	now := time.Now().UTC()

	noPos := newTrip(uuid.New()) // без координат, стартовала 3 часа назад
	noPos.StartedAt = now.Add(-1000 * time.Hour)
	cleanup(t, pool, noPos.ID)
	require.NoError(t, repo.Create(ctx, noPos))

	oldPos := newTrip(uuid.New()) // координата 2 часа назад
	cleanup(t, pool, oldPos.ID)
	require.NoError(t, repo.Create(ctx, oldPos))
	_, err := pool.Exec(ctx, "UPDATE trips SET last_position_at = now() - interval '999 hours' WHERE id = $1", oldPos.ID)
	require.NoError(t, err)

	fresh := newTrip(uuid.New())
	cleanup(t, pool, fresh.ID)
	require.NoError(t, repo.Create(ctx, fresh))
	_, err = pool.Exec(ctx, "UPDATE trips SET last_position_at = now() WHERE id = $1", fresh.ID)
	require.NoError(t, err)

	completed := newTrip(uuid.New())
	cleanup(t, pool, completed.ID)
	require.NoError(t, repo.Create(ctx, completed))
	_, err = repo.SaveFinished(ctx, completed.ID, now)
	require.NoError(t, err)

	got, err := repo.ListStale(ctx, time.Minute, nil, 100)
	require.NoError(t, err)
	ids := make([]uuid.UUID, 0, len(got))
	for _, s := range got {
		ids = append(ids, s.ID)
	}
	require.Contains(t, ids, noPos.ID)
	require.Contains(t, ids, oldPos.ID)
	require.NotContains(t, ids, fresh.ID)
	require.NotContains(t, ids, completed.ID)
	require.Less(t, indexOf(ids, noPos.ID), indexOf(ids, oldPos.ID))
}

func indexOf(ids []uuid.UUID, id uuid.UUID) int {
	for i, v := range ids {
		if v == id {
			return i
		}
	}
	return -1
}

// Проверяет форму плана
func TestRepo_ListStale_UsesIndex(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	t.Cleanup(conn.Release)
	_, err = conn.Exec(ctx, "SET enable_seqscan = off")
	require.NoError(t, err)
	rows, err := conn.Query(ctx, `EXPLAIN SELECT id, driver_id FROM trips
		WHERE status = 'active'
		  AND COALESCE(last_position_at, started_at) < now() - make_interval(secs => $1::double precision)
		ORDER BY COALESCE(last_position_at, started_at), id`, 60.0)
	require.NoError(t, err)
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		plan = append(plan, line)
	}
	require.NoError(t, rows.Err())
	require.Contains(t, strings.Join(plan, "\n"), "trips_stale_idx")
}

// Проверяет курсор, 250 своих поездок читаются тремя батчами по 100 без пропусков и дублей
func TestRepo_ListStale_BatchesByCursor(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool, testQueryTimeout)
	ctx := context.Background()
	// делаем поездки самыми старыми, чтобы они шли первыми в выборке
	base := time.Now().UTC().Add(-100 * time.Hour).Truncate(time.Microsecond)
	ids := make([]uuid.UUID, 0, 250)
	for i := 0; i < 250; i++ {
		tr := newTrip(uuid.New())
		tr.StartedAt = base.Add(time.Duration(i) * time.Second)
		cleanup(t, pool, tr.ID)
		require.NoError(t, repo.Create(ctx, tr))
		ids = append(ids, tr.ID)
	}
	var cursor *model.StaleCursor
	got := make([]uuid.UUID, 0, 300)
	for i := 0; i < 3; i++ {
		batch, err := repo.ListStale(ctx, time.Minute, cursor, 100)
		require.NoError(t, err)
		require.NotEmpty(t, batch)
		for _, s := range batch {
			got = append(got, s.ID)
		}
		last := batch[len(batch)-1]
		cursor = &model.StaleCursor{At: last.LastAt, ID: last.ID}
	}
	// порядок совпал - значит три батча покрыли все 250 без пропусков и повторов
	require.Equal(t, ids, got[:250])
}

// Проверяет курсор при одинаковом времени, никто не потерян и не прочитан дважды
func TestRepo_ListStale_CursorWithEqualTimes(t *testing.T) {
	pool := newPool(t)
	repo := repoTrip.New(pool, testQueryTimeout)
	ctx := context.Background()
	at := time.Now().UTC().Add(-100 * time.Hour).Truncate(time.Microsecond)
	ids := make([]uuid.UUID, 0, 5)
	for i := 0; i < 5; i++ {
		tr := newTrip(uuid.New())
		tr.StartedAt = at
		cleanup(t, pool, tr.ID)
		require.NoError(t, repo.Create(ctx, tr))
		ids = append(ids, tr.ID)
	}
	var cursor *model.StaleCursor
	got := make([]uuid.UUID, 0, 6)
	for i := 0; i < 3; i++ {
		batch, err := repo.ListStale(ctx, time.Minute, cursor, 2)
		require.NoError(t, err)
		require.NotEmpty(t, batch)
		for _, s := range batch {
			got = append(got, s.ID)
		}
		last := batch[len(batch)-1]
		cursor = &model.StaleCursor{At: last.LastAt, ID: last.ID}
	}
	require.ElementsMatch(t, ids, got[:5])
}

// Проверяет форму плана запроса с продолжением, тоже должен идти по индексу
func TestRepo_ListStale_CursorUsesIndex(t *testing.T) {
	pool := newPool(t)
	ctx := context.Background()
	conn, err := pool.Acquire(ctx)
	require.NoError(t, err)
	t.Cleanup(conn.Release)
	_, err = conn.Exec(ctx, "SET enable_seqscan = off")
	require.NoError(t, err)
	rows, err := conn.Query(ctx, `EXPLAIN SELECT id, driver_id, COALESCE(last_position_at, started_at) AS last_at FROM trips
		WHERE status = 'active'
		  AND COALESCE(last_position_at, started_at) < now() - make_interval(secs => $1::double precision)
		  AND (COALESCE(last_position_at, started_at), id) > ($2, $3)
		ORDER BY last_at, id LIMIT 100`,
		60.0, time.Now().UTC().Add(-time.Hour), uuid.New())
	require.NoError(t, err)
	defer rows.Close()
	var plan []string
	for rows.Next() {
		var line string
		require.NoError(t, rows.Scan(&line))
		plan = append(plan, line)
	}
	require.NoError(t, rows.Err())
	require.Contains(t, strings.Join(plan, "\n"), "trips_stale_idx")
}
