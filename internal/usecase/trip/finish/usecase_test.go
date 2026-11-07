package finish_test

import (
	"context"
	"errors"
	"log/slog"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	"github.com/ytkasmerti/go-course-avito/internal/model"
	uc "github.com/ytkasmerti/go-course-avito/internal/usecase/trip/finish"
)

var testLog = slog.New(slog.DiscardHandler)

type fixture struct {
	uc      *uc.Usecase
	repo    *MocktripRepository
	metrics *MockbusinessMetrics
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	tx := NewMocktxManager(ctrl)
	tx.EXPECT().Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }).
		AnyTimes()

	f := &fixture{
		repo:    NewMocktripRepository(ctrl),
		metrics: NewMockbusinessMetrics(ctrl),
	}
	f.uc = uc.New(f.repo, tx, f.metrics, testLog)
	return f
}

func tripWith(id uuid.UUID, status domaintrip.Status) *model.Trip {
	return &model.Trip{ID: id, Status: string(status)}
}

// Проверяет завершение активной поездки
func TestUsecase_Finish_Success(t *testing.T) {
	f := newFixture(t)
	id := uuid.New()
	finished := tripWith(id, domaintrip.StatusCompleted)

	f.repo.EXPECT().GetByID(gomock.Any(), id).Return(tripWith(id, domaintrip.StatusActive), nil)
	// в репозиторий уходит время, которое выставил домен, а не now из БД
	f.repo.EXPECT().SaveFinished(gomock.Any(), id, gomock.Any()).Return(finished, nil)
	f.metrics.EXPECT().IncCompleted(gomock.Any())

	got, err := f.uc.Do(context.Background(), id)
	require.NoError(t, err)
	require.Same(t, finished, got)
}

// Проверяет 409 до записи в БД
func TestUsecase_Finish_DomainRejectsCompletedTrip(t *testing.T) {
	f := newFixture(t)
	id := uuid.New()
	f.repo.EXPECT().GetByID(gomock.Any(), id).Return(tripWith(id, domaintrip.StatusCompleted), nil)

	_, err := f.uc.Do(context.Background(), id)
	require.ErrorIs(t, err, domaintrip.ErrTripCompleted)
}

// Проверяет 404
func TestUsecase_Finish_NotFound(t *testing.T) {
	f := newFixture(t)
	id := uuid.New()
	f.repo.EXPECT().GetByID(gomock.Any(), id).Return(nil, domaintrip.ErrTripNotFound)

	_, err := f.uc.Do(context.Background(), id)
	require.ErrorIs(t, err, domaintrip.ErrTripNotFound)
}

// Проверяет 409 при конкурентном завершении
func TestUsecase_Finish_LostRaceToConcurrentFinish(t *testing.T) {
	f := newFixture(t)
	id := uuid.New()
	f.repo.EXPECT().GetByID(gomock.Any(), id).Return(tripWith(id, domaintrip.StatusActive), nil)
	f.repo.EXPECT().SaveFinished(gomock.Any(), id, gomock.Any()).Return(nil, domaintrip.ErrTripCompleted)

	_, err := f.uc.Do(context.Background(), id)
	require.ErrorIs(t, err, domaintrip.ErrTripCompleted)
}

// Проверяет проброс ошибок репозитория
func TestUsecase_Finish_RepoErrors(t *testing.T) {
	boom := errors.New("db down")

	t.Run("read fails", func(t *testing.T) {
		f := newFixture(t)
		id := uuid.New()
		f.repo.EXPECT().GetByID(gomock.Any(), id).Return(nil, boom)
		_, err := f.uc.Do(context.Background(), id)
		require.ErrorIs(t, err, boom)
	})

	t.Run("save fails", func(t *testing.T) {
		f := newFixture(t)
		id := uuid.New()
		f.repo.EXPECT().GetByID(gomock.Any(), id).Return(tripWith(id, domaintrip.StatusActive), nil)
		f.repo.EXPECT().SaveFinished(gomock.Any(), id, gomock.Any()).Return(nil, boom)
		_, err := f.uc.Do(context.Background(), id)
		require.ErrorIs(t, err, boom)
	})
}
