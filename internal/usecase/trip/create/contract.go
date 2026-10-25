//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package create

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/ytkasmerti/go-course-avito/internal/model"
)

type (
	tripRepository interface {
		Create(ctx context.Context, t model.Trip) error
		GetByID(ctx context.Context, id uuid.UUID) (*model.Trip, error)
		GetIdempotencyKey(ctx context.Context, key uuid.UUID) (*model.IdempotencyRecord, error)
		SaveIdempotencyKey(ctx context.Context, key uuid.UUID, hash string, tripID uuid.UUID, ttl time.Duration) error
	}

	txManager interface {
		Do(ctx context.Context, fn func(ctx context.Context) error) error
	}

	businessMetrics interface {
		IncCreated(ctx context.Context)
	}
)
