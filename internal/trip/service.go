package trip

import (
	"context"
	"time"
	"github.com/google/uuid"
	"github.com/ytkasmerti/go-course-avito/internal/txmanager"
)

type Service struct {
	repo *Repository
	tx   txmanager.TxManager
}

type CreateInput struct {
	UserID         uuid.UUID
	DriverID       uuid.UUID
	StartLatitude  float64
	StartLongitude float64
	EndLatitude    float64
	EndLongitude   float64
	Price          int64
}

func (s *Service) Create(ctx context.Context, in CreateInput) (*Trip, error) {
	t := &Trip{
		ID:             uuid.New(),
		UserID:         in.UserID,
		DriverID:       in.DriverID,
		StartLatitude:  in.StartLatitude,
		StartLongitude: in.StartLongitude,
		EndLatitude:    in.EndLatitude,
		EndLongitude:   in.EndLongitude,
		Price:          in.Price,
		Status:         StatusActive,
		StartedAt:      time.Now().UTC(),
	}

	err := s.tx.Do(ctx, func(ctx context.Context) error {
		return s.repo.Create(ctx, t)
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}

func (s *Service) Get(ctx context.Context, id uuid.UUID) (*Trip, error) {
	return s.repo.GetByID(ctx, id)
}

func (s *Service) Finish(ctx context.Context, id uuid.UUID) (*Trip, error) {
	var t *Trip
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		var errFinish error
		t, errFinish = s.repo.Finish(ctx, id)
		return errFinish
	})
	if err != nil {
		return nil, err
	}
	return t, nil
}