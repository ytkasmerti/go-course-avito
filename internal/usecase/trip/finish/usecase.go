package finish

import (
	"context"

	"github.com/google/uuid"

	"github.com/ytkasmerti/go-course-avito/internal/model"
)

type Usecase struct {
	repo    tripRepository
	tx      txManager
	metrics businessMetrics
}

func New(repo tripRepository, tx txManager, metrics businessMetrics) *Usecase {
	return &Usecase{repo: repo, tx: tx, metrics: metrics}
}

func (u *Usecase) Do(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	var t *model.Trip
	err := u.tx.Do(ctx, func(ctx context.Context) error {
		tt, err := u.repo.Finish(ctx, id)
		if err != nil {
			return err
		}
		t = tt
		return nil
	})
	if err != nil {
		return nil, err
	}
	u.metrics.IncCompleted(ctx)
	return t, nil
}
