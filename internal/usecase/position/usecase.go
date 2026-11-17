//go:generate go tool mockgen -source ${GOFILE} -destination mocks_test.go -package ${GOPACKAGE}_test
package position

import (
	"context"
	"time"

	"github.com/google/uuid"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	"github.com/ytkasmerti/go-course-avito/internal/model"
)

type (
	positionSaver interface {
		SavePosition(ctx context.Context, p model.Position) (*model.Position, error)
	}
	positionLister interface {
		ListPositions(ctx context.Context, tripID uuid.UUID) ([]model.Position, error)
	}
	tripRepository interface {
		GetByID(ctx context.Context, id uuid.UUID) (*model.Trip, error)
	}
	txManager interface {
		Do(ctx context.Context, fn func(ctx context.Context) error) error
	}
	queueCanceler interface {
		Cancel(tripID uuid.UUID)
	}
	positionMetrics interface {
		IncPositionSaved(ctx context.Context)
	}
)

type SaveInput struct {
	TripID     uuid.UUID
	Latitude   float64
	Longitude  float64
	RecordedAt time.Time
}

type SaveUsecase struct {
	repo    positionSaver
	trips   tripRepository
	tx      txManager
	cancel  queueCanceler
	metrics positionMetrics
}

func NewSave(repo positionSaver, trips tripRepository, tx txManager, cancel queueCanceler, metrics positionMetrics) *SaveUsecase {
	return &SaveUsecase{repo: repo, trips: trips, tx: tx, cancel: cancel, metrics: metrics}
}

func (u *SaveUsecase) Do(ctx context.Context, in SaveInput) (*model.Position, error) {
	p := domaintrip.NewPositionParams{Latitude: in.Latitude, Longitude: in.Longitude, RecordedAt: in.RecordedAt}
	if err := p.Validate(time.Now().UTC()); err != nil {
		return nil, err
	}
	var saved *model.Position
	err := u.tx.Do(ctx, func(ctx context.Context) error {
		current, err := u.trips.GetByID(ctx, in.TripID)
		if err != nil {
			return err
		}
		t := domaintrip.Trip{Status: domaintrip.Status(current.Status)}
		if err := t.EnsureActive(); err != nil {
			return err
		}
		saved, err = u.repo.SavePosition(ctx, model.Position{
			TripID: in.TripID, Latitude: in.Latitude, Longitude: in.Longitude, RecordedAt: in.RecordedAt,
		})
		return err
	})
	if err != nil {
		return nil, err
	}
	// координата пришла — запрошенный пуш больше не нужен
	u.cancel.Cancel(in.TripID)
	u.metrics.IncPositionSaved(ctx)
	return saved, nil
}

type ListUsecase struct {
	repo  positionLister
	trips tripRepository
}

func NewList(repo positionLister, trips tripRepository) *ListUsecase {
	return &ListUsecase{repo: repo, trips: trips}
}

func (u *ListUsecase) Do(ctx context.Context, tripID uuid.UUID) ([]model.Position, error) {
	// маршрут смотрим у любой существующей поездки, в том числе завершенной
	if _, err := u.trips.GetByID(ctx, tripID); err != nil {
		return nil, err
	}
	return u.repo.ListPositions(ctx, tripID)
}
