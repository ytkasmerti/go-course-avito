package finish

import (
	"context"

	"github.com/google/uuid"

	"github.com/ytkasmerti/go-course-avito/internal/model"
)

type (
	tripRepository interface {
		GetByID(ctx context.Context, id uuid.UUID) (*model.Trip, error)
		Finish(ctx context.Context, id uuid.UUID) (*model.Trip, error)
	}

	txManager interface {
		Do(ctx context.Context, fn func(ctx context.Context) error) error
	}
)
