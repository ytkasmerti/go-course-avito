package position_test

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	"github.com/ytkasmerti/go-course-avito/internal/model"
	uc "github.com/ytkasmerti/go-course-avito/internal/usecase/position"
)

type saveFixture struct {
	uc      *uc.SaveUsecase
	repo    *MockpositionSaver
	trips   *MocktripRepository
	cancel  *MockqueueCanceler
	metrics *MockpositionMetrics
	in      uc.SaveInput
}

func newSaveFixture(t *testing.T) *saveFixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	tx := NewMocktxManager(ctrl)
	tx.EXPECT().Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }).
		AnyTimes()
	f := &saveFixture{
		repo: NewMockpositionSaver(ctrl), trips: NewMocktripRepository(ctrl),
		cancel: NewMockqueueCanceler(ctrl), metrics: NewMockpositionMetrics(ctrl),
		in: uc.SaveInput{
			TripID: uuid.New(), Latitude: 59.9, Longitude: 30.3,
			RecordedAt: time.Now().UTC().Add(-time.Minute),
		},
	}
	f.uc = uc.NewSave(f.repo, f.trips, tx, f.cancel, f.metrics)
	return f
}

func activeTrip(id uuid.UUID) *model.Trip {
	return &model.Trip{ID: id, Status: string(domaintrip.StatusActive)}
}

// Проверяет сохранение, отмену пуша и метрику после коммита
func TestSave_Success(t *testing.T) {
	f := newSaveFixture(t)
	saved := &model.Position{ID: 1, TripID: f.in.TripID}
	f.trips.EXPECT().GetByID(gomock.Any(), f.in.TripID).Return(activeTrip(f.in.TripID), nil)
	f.repo.EXPECT().SavePosition(gomock.Any(), gomock.Any()).Return(saved, nil)
	f.cancel.EXPECT().Cancel(f.in.TripID)
	f.metrics.EXPECT().IncPositionSaved(gomock.Any())
	got, err := f.uc.Do(context.Background(), f.in)
	require.NoError(t, err)
	require.Same(t, saved, got)
}

// Проверяет валидацию до обращения к зависимостям
func TestSave_InvalidBeforeDependencies(t *testing.T) {
	f := newSaveFixture(t)
	f.in.Latitude = 91
	_, err := f.uc.Do(context.Background(), f.in)
	var ve *domaintrip.ValidationError
	require.ErrorAs(t, err, &ve)
	require.Equal(t, "coordinates.latitude", ve.Field)
}

// Проверяет 404, 409 и ошибку репозитория, отмена и метрика не вызываются
func TestSave_Errors(t *testing.T) {
	boom := errors.New("db down")
	t.Run("not found", func(t *testing.T) {
		f := newSaveFixture(t)
		f.trips.EXPECT().GetByID(gomock.Any(), f.in.TripID).Return(nil, domaintrip.ErrTripNotFound)
		_, err := f.uc.Do(context.Background(), f.in)
		require.ErrorIs(t, err, domaintrip.ErrTripNotFound)
	})
	t.Run("completed", func(t *testing.T) {
		f := newSaveFixture(t)
		f.trips.EXPECT().GetByID(gomock.Any(), f.in.TripID).
			Return(&model.Trip{ID: f.in.TripID, Status: string(domaintrip.StatusCompleted)}, nil)
		_, err := f.uc.Do(context.Background(), f.in)
		require.ErrorIs(t, err, domaintrip.ErrTripCompleted)
	})
	t.Run("save fails", func(t *testing.T) {
		f := newSaveFixture(t)
		f.trips.EXPECT().GetByID(gomock.Any(), f.in.TripID).Return(activeTrip(f.in.TripID), nil)
		f.repo.EXPECT().SavePosition(gomock.Any(), gomock.Any()).Return(nil, boom)
		_, err := f.uc.Do(context.Background(), f.in)
		require.ErrorIs(t, err, boom)
	})
}

// Проверяет отдачу маршрута и 404 до запроса точек
func TestList(t *testing.T) {
	t.Run("success", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo, trips := NewMockpositionLister(ctrl), NewMocktripRepository(ctrl)
		id := uuid.New()
		want := []model.Position{{ID: 1, TripID: id}}
		trips.EXPECT().GetByID(gomock.Any(), id).Return(&model.Trip{ID: id}, nil)
		repo.EXPECT().ListPositions(gomock.Any(), id).Return(want, nil)
		got, err := uc.NewList(repo, trips).Do(context.Background(), id)
		require.NoError(t, err)
		require.Equal(t, want, got)
	})
	t.Run("not found", func(t *testing.T) {
		ctrl := gomock.NewController(t)
		repo, trips := NewMockpositionLister(ctrl), NewMocktripRepository(ctrl)
		id := uuid.New()
		trips.EXPECT().GetByID(gomock.Any(), id).Return(nil, domaintrip.ErrTripNotFound)
		_, err := uc.NewList(repo, trips).Do(context.Background(), id)
		require.ErrorIs(t, err, domaintrip.ErrTripNotFound)
	})
}
