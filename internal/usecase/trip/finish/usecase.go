package finish

import (
	"context"
	"log/slog"
	"time"

	"github.com/google/uuid"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	"github.com/ytkasmerti/go-course-avito/internal/model"
)

type Usecase struct {
	repo    tripRepository
	tx      txManager
	metrics businessMetrics
	log     *slog.Logger
}

func New(repo tripRepository, tx txManager, metrics businessMetrics, log *slog.Logger) *Usecase {
	return &Usecase{
		repo:    repo,
		tx:      tx,
		metrics: metrics,
		log:     log,
	}
}

func (u *Usecase) Do(ctx context.Context, id uuid.UUID) (*model.Trip, error) {
	var finished *model.Trip
	err := u.tx.Do(ctx, func(ctx context.Context) error {
		current, err := u.repo.GetByID(ctx, id)
		if err != nil {
			return err
		}

		// Переход active - completed проверяет домен, конкурентное завершение ловит БД
		t := domaintrip.Trip{ID: current.ID, Status: domaintrip.Status(current.Status)}
		if err := t.Finish(time.Now().UTC()); err != nil {
			return err
		}

		finished, err = u.repo.SaveFinished(ctx, id, *t.FinishedAt)
		return err
	})
	if err != nil {
		return nil, err
	}

	u.metrics.IncCompleted(ctx)
	u.log.InfoContext(ctx, "trip completed", slog.String("trip_id", finished.ID.String()))
	return finished, nil
}
