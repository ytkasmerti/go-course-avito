package get

import (
	"context"

	"github.com/google/uuid"

	"github.com/ytkasmerti/go-course-avito/internal/model"
)

type getUsecase interface {
	Do(ctx context.Context, id uuid.UUID) (*model.Trip, error)
}
