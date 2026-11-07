-- +goose Up
CREATE TABLE idempotency_keys (
    key          UUID PRIMARY KEY,
    request_hash TEXT NOT NULL,
    trip_id      UUID NOT NULL REFERENCES trips(id) ON DELETE CASCADE,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at   TIMESTAMPTZ NOT NULL
);
CREATE INDEX idempotency_keys_expires_at_idx ON idempotency_keys (expires_at);

-- +goose Down
DROP INDEX IF EXISTS idempotency_keys_expires_at_idx;
DROP TABLE idempotency_keys;
