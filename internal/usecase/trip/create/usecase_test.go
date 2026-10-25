package create_test

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	"github.com/ytkasmerti/go-course-avito/internal/model"
	uc "github.com/ytkasmerti/go-course-avito/internal/usecase/trip/create"
)

func TestUsecase_Create_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := NewMocktripRepository(ctrl)
	tx := NewMocktxManager(ctrl)
	metrics := NewMockbusinessMetrics(ctrl)

	in := uc.Input{UserID: uuid.New(), DriverID: uuid.New(), Price: 1450}

	tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	metrics.EXPECT().IncCreated(gomock.Any())

	u := uc.New(repo, tx, metrics)
	trip, repeated, err := u.Do(context.Background(), in, nil)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if trip == nil || repeated {
		t.Fatal("invalid result")
	}
}

func TestUsecase_Create_DriverBusy(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := NewMocktripRepository(ctrl)
	tx := NewMocktxManager(ctrl)
	metrics := NewMockbusinessMetrics(ctrl)

	in := uc.Input{UserID: uuid.New(), DriverID: uuid.New(), Price: 100}

	tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(domaintrip.ErrDriverBusy)

	u := uc.New(repo, tx, metrics)
	_, _, err := u.Do(context.Background(), in, nil)

	if !errors.Is(err, domaintrip.ErrDriverBusy) {
		t.Fatalf("want ErrDriverBusy, got %v", err)
	}
}

func TestUsecase_Create_IdempotencyConflict(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := NewMocktripRepository(ctrl)
	tx := NewMocktxManager(ctrl)
	metrics := NewMockbusinessMetrics(ctrl)

	key := uuid.New()
	in := uc.Input{UserID: uuid.New(), DriverID: uuid.New(), Price: 100}

	tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	repo.EXPECT().GetIdempotencyKey(gomock.Any(), key).Return(&model.IdempotencyRecord{
		TripID:      uuid.New(),
		RequestHash: "different-hash",
	}, nil)

	u := uc.New(repo, tx, metrics)
	_, _, err := u.Do(context.Background(), in, &key)

	if !errors.Is(err, domaintrip.ErrIdempotencyConflict) {
		t.Fatalf("want ErrIdempotencyConflict, got %v", err)
	}
}

func TestUsecase_Create_RepoError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := NewMocktripRepository(ctrl)
	tx := NewMocktxManager(ctrl)
	metrics := NewMockbusinessMetrics(ctrl)

	in := uc.Input{UserID: uuid.New(), DriverID: uuid.New(), Price: 100}
	dbErr := errors.New("db down")

	tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(dbErr)

	u := uc.New(repo, tx, metrics)
	_, _, err := u.Do(context.Background(), in, nil)

	if !errors.Is(err, dbErr) {
		t.Fatalf("want dbErr, got %v", err)
	}
}
func TestUsecase_Create_IdempotencyRepeat(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := NewMocktripRepository(ctrl)
	tx := NewMocktxManager(ctrl)
	metrics := NewMockbusinessMetrics(ctrl)

	key := uuid.New()
	in := uc.Input{UserID: uuid.New(), DriverID: uuid.New(), Price: 100}
	tripID := uuid.New()

	tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	repo.EXPECT().GetIdempotencyKey(gomock.Any(), key).Return(&model.IdempotencyRecord{
		TripID:      tripID,
		RequestHash: hashOf(in),
	}, nil)
	repo.EXPECT().GetByID(gomock.Any(), tripID).Return(&model.Trip{ID: tripID}, nil)

	u := uc.New(repo, tx, metrics)
	trip, repeated, err := u.Do(context.Background(), in, &key)

	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if !repeated {
		t.Error("repeated must be true")
	}
	if trip.ID != tripID {
		t.Errorf("want %v, got %v", tripID, trip.ID)
	}
}

func hashOf(in uc.Input) string {
	h := sha256.New()
	fmt.Fprintf(h, "%s|%s|%v|%v|%v|%v|%d",
		in.UserID, in.DriverID,
		in.StartLatitude, in.StartLongitude,
		in.EndLatitude, in.EndLongitude,
		in.Price,
	)
	return hex.EncodeToString(h.Sum(nil))
}
