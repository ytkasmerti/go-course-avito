-- +goose NO TRANSACTION

-- +goose Up
CREATE INDEX CONCURRENTLY IF NOT EXISTS trips_stale_idx
    ON trips (COALESCE(last_position_at, started_at))
    WHERE status = 'active';

-- +goose Down
DROP INDEX CONCURRENTLY IF EXISTS trips_stale_idx;