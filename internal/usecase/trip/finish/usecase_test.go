package finish_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	"github.com/ytkasmerti/go-course-avito/internal/model"
	uc "github.com/ytkasmerti/go-course-avito/internal/usecase/trip/finish"
)

var testLog = slog.New(slog.DiscardHandler)

func TestUsecase_Finish_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	repo := NewMocktripRepository(ctrl)
	tx := NewMocktxManager(ctrl)
	metrics := NewMockbusinessMetrics(ctrl)
	id := uuid.New()
	tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	repo.EXPECT().Finish(gomock.Any(), id).Return(&model.Trip{
		ID: id, Status: string(domaintrip.StatusCompleted),
	}, nil)
	metrics.EXPECT().IncCompleted(gomock.Any())
	u := uc.New(repo, tx, metrics, testLog)
	trip, err := u.Do(context.Background(), id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if trip.Status != string(domaintrip.StatusCompleted) {
		t.Errorf("want completed, got %v", trip.Status)
	}
}

func TestUsecase_Finish_AlreadyCompleted(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	repo := NewMocktripRepository(ctrl)
	tx := NewMocktxManager(ctrl)
	metrics := NewMockbusinessMetrics(ctrl)
	id := uuid.New()
	tx.EXPECT().Do(gomock.Any(), gomock.Any()).DoAndReturn(
		func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) })
	repo.EXPECT().Finish(gomock.Any(), id).Return(nil, domaintrip.ErrTripCompleted)
	u := uc.New(repo, tx, metrics, testLog)
	_, err := u.Do(context.Background(), id)
	if !errors.Is(err, domaintrip.ErrTripCompleted) {
		t.Fatalf("want ErrTripCompleted, got %v", err)
	}
}
