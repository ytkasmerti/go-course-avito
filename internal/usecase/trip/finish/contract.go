//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package finish

import (
	"context"
	"time"

	"github.com/google/uuid"

	"github.com/ytkasmerti/go-course-avito/internal/model"
)

type (
	tripRepository interface {
		GetByID(ctx context.Context, id uuid.UUID) (*model.Trip, error)
		SaveFinished(ctx context.Context, id uuid.UUID, finishedAt time.Time) (*model.Trip, error)
	}

	txManager interface {
		Do(ctx context.Context, fn func(ctx context.Context) error) error
	}

	businessMetrics interface {
		IncCompleted(ctx context.Context)
	}
)
