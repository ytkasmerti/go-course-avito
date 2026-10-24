package create

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	"github.com/ytkasmerti/go-course-avito/internal/model"
)

const idempotencyTTL = 24 * time.Hour

type Input struct {
	UserID         uuid.UUID
	DriverID       uuid.UUID
	StartLatitude  float64
	StartLongitude float64
	EndLatitude    float64
	EndLongitude   float64
	Price          int64
}

type Usecase struct {
	repo    tripRepository
	tx      txManager
	metrics businessMetrics
}

func New(repo tripRepository, tx txManager, metrics businessMetrics) *Usecase {
	return &Usecase{repo: repo, tx: tx, metrics: metrics}
}

func (u *Usecase) Do(ctx context.Context, in Input, idemKey *uuid.UUID) (*model.Trip, bool, error) {
	if idemKey == nil {
		t, err := u.createInTx(ctx, in)
		return t, false, err
	}

	hash := hashInput(in)

	var (
		result   *model.Trip
		repeated bool
	)

	err := u.tx.Do(ctx, func(ctx context.Context) error {
		rec, err := u.repo.GetIdempotencyKey(ctx, *idemKey)
		if err != nil {
			return err
		}
		if rec != nil {
			if rec.RequestHash != hash {
				return domaintrip.ErrIdempotencyConflict
			}
			t, err := u.repo.GetByID(ctx, rec.TripID)
			if err != nil {
				return err
			}
			result, repeated = t, true
			return nil
		}

		t, err := u.insertTrip(ctx, in)
		if err != nil {
			if errors.Is(err, domaintrip.ErrDriverBusy) {
				return domaintrip.ErrIdempotencyRace
			}
			return err
		}

		if err := u.repo.SaveIdempotencyKey(ctx, *idemKey, hash, t.ID, idempotencyTTL); err != nil {
			if errors.Is(err, domaintrip.ErrIdempotencyRace) {
				return domaintrip.ErrIdempotencyRace
			}
			return err
		}

		result = t
		return nil
	})

	if errors.Is(err, domaintrip.ErrIdempotencyRace) {
		for i := 0; i < 20; i++ {
			rec, err2 := u.repo.GetIdempotencyKey(ctx, *idemKey)
			if err2 != nil {
				return nil, false, err2
			}
			if rec != nil {
				if rec.RequestHash != hash {
					return nil, false, domaintrip.ErrIdempotencyConflict
				}
				t, err2 := u.repo.GetByID(ctx, rec.TripID)
				if err2 != nil {
					return nil, false, err2
				}
				return t, true, nil
			}
			time.Sleep(10 * time.Millisecond)
		}
		return nil, false, domaintrip.ErrDriverBusy
	}
	if err != nil {
		return nil, false, err
	}
	return result, repeated, nil
}

func (u *Usecase) createInTx(ctx context.Context, in Input) (*model.Trip, error) {
	var t *model.Trip
	err := u.tx.Do(ctx, func(ctx context.Context) error {
		tt, err := u.insertTrip(ctx, in)
		if err != nil {
			return err
		}
		t = tt
		return nil
	})
	u.metrics.IncCreated(ctx)
	return t, err
}

func (u *Usecase) insertTrip(ctx context.Context, in Input) (*model.Trip, error) {
	t := &model.Trip{
		ID:             uuid.New(),
		UserID:         in.UserID,
		DriverID:       in.DriverID,
		StartLatitude:  in.StartLatitude,
		StartLongitude: in.StartLongitude,
		EndLatitude:    in.EndLatitude,
		EndLongitude:   in.EndLongitude,
		Price:          in.Price,
		Status:         string(domaintrip.StatusActive),
		StartedAt:      time.Now().UTC(),
	}

	if err := u.repo.Create(ctx, *t); err != nil {
		return nil, err
	}
	return t, nil
}

func hashInput(in Input) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%v|%v|%v|%v|%d",
		in.UserID, in.DriverID,
		in.StartLatitude, in.StartLongitude,
		in.EndLatitude, in.EndLongitude,
		in.Price,
	)
	return hex.EncodeToString(h.Sum(nil))
}
