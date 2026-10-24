package create

import (
	"context"

	"github.com/google/uuid"

	"github.com/ytkasmerti/go-course-avito/internal/model"
	usecaseCreate "github.com/ytkasmerti/go-course-avito/internal/usecase/trip/create"
)

type (
	createUsecase interface {
		Do(ctx context.Context, in usecaseCreate.Input, idemKey *uuid.UUID) (*model.Trip, bool, error)
	}
)
