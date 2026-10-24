package finish

import (
	"context"

	"github.com/google/uuid"

	"github.com/ytkasmerti/go-course-avito/internal/model"
)

type finishUsecase interface {
	Do(ctx context.Context, id uuid.UUID) (*model.Trip, error)
}
