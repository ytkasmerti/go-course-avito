package finish

import (
	"context"
	"log/slog"

	"github.com/google/uuid"

	"github.com/ytkasmerti/go-course-avito/internal/model"
)

type Usecase struct {
	repo    tripRepository
	tx      txManager
	metrics businessMetrics
	log     *slog.Logger
}

func New(repo tripRepository, tx txManager, metrics businessMetrics, log *slog.Logger) *Usecase {
	return &Usecase{repo: repo, tx: tx, metrics: metrics, log: log}
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
	u.log.InfoContext(ctx, "trip completed", slog.String("trip_id", t.ID.String()))
	return t, nil
}
