package trip

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"time"

	"github.com/google/uuid"
	"github.com/ytkasmerti/go-course-avito/internal/txmanager"
)

const idempotencyTTL = 24 * time.Hour

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

func NewService(repo *Repository, tx txmanager.TxManager) *Service {
	return &Service{repo: repo, tx: tx}
}

func (s *Service) Create(ctx context.Context, in CreateInput, idemKey *uuid.UUID) (*Trip, bool, error) {
	if idemKey == nil {
		t, err := s.createInTx(ctx, in)
		return t, false, err
	}

	hash := hashInput(in)

	var (
		result   *Trip
		repeated bool
	)

	err := s.tx.Do(ctx, func(ctx context.Context) error {
		rec, err := s.repo.GetIdempotencyKey(ctx, *idemKey)
		if err != nil {
			return err
		}
		if rec != nil {
			if rec.RequestHash != hash {
				return ErrIdempotencyConflict
			}
			t, err := s.repo.GetByID(ctx, rec.TripID)
			if err != nil {
				return err
			}
			result, repeated = t, true
			return nil
		}

		t, err := s.insertTrip(ctx, in)
		if err != nil {
			if errors.Is(err, ErrDriverBusy) {
				return ErrIdempotencyRace
			}
			return err
		}

		if err := s.repo.SaveIdempotencyKey(ctx, *idemKey, hash, t.ID, idempotencyTTL); err != nil {
			if errors.Is(err, ErrIdempotencyRace) {
				return ErrIdempotencyRace
			}
			return err
		}

		result = t
		return nil
	})

	if errors.Is(err, ErrIdempotencyRace) {
		for i := 0; i < 20; i++ {
			rec, err2 := s.repo.GetIdempotencyKey(ctx, *idemKey)
			if err2 != nil {
				return nil, false, err2
			}
			if rec != nil {
				if rec.RequestHash != hash {
					return nil, false, ErrIdempotencyConflict
				}
				t, err2 := s.repo.GetByID(ctx, rec.TripID)
				if err2 != nil {
					return nil, false, err2
				}
				return t, true, nil
			}
			time.Sleep(10 * time.Millisecond)
		}
		return nil, false, ErrDriverBusy
	}
	if err != nil {
		return nil, false, err
	}
	return result, repeated, nil
}

func (s *Service) createInTx(ctx context.Context, in CreateInput) (*Trip, error) {
	var t *Trip
	err := s.tx.Do(ctx, func(ctx context.Context) error {
		tt, err := s.insertTrip(ctx, in)
		if err != nil {
			return err
		}
		t = tt
		return nil
	})
	return t, err
}

func (s *Service) insertTrip(ctx context.Context, in CreateInput) (*Trip, error) {
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

	if err := s.repo.Create(ctx, t); err != nil {
		return nil, err
	}
	return t, nil
}

func hashInput(in CreateInput) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%v|%v|%v|%v|%d",
		in.UserID, in.DriverID,
		in.StartLatitude, in.StartLongitude,
		in.EndLatitude, in.EndLongitude,
		in.Price,
	)
	return hex.EncodeToString(h.Sum(nil))
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
