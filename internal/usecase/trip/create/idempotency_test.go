package create_test

import (
	"context"
	"errors"
	"io"
	"log/slog"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	"github.com/ytkasmerti/go-course-avito/internal/model"
	"github.com/ytkasmerti/go-course-avito/internal/usecase/trip/create"
)

type idemFixture struct {
	uc   *create.Usecase
	repo *MocktripRepository
	in   create.Input
	key  uuid.UUID
}

func newIdemFixture(t *testing.T) *idemFixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	tx := NewMocktxManager(ctrl)
	tx.EXPECT().Do(gomock.Any(), gomock.Any()).
		DoAndReturn(func(ctx context.Context, fn func(context.Context) error) error { return fn(ctx) }).
		AnyTimes()
	metrics := NewMockbusinessMetrics(ctrl)
	metrics.EXPECT().IncCreated(gomock.Any()).AnyTimes()
	repo := NewMocktripRepository(ctrl)
	return &idemFixture{
		uc:   create.New(repo, tx, metrics, slog.New(slog.NewJSONHandler(io.Discard, nil))),
		repo: repo,
		in: create.Input{
			UserID: uuid.New(), DriverID: uuid.New(),
			StartLatitude: 59.9, StartLongitude: 30.3, EndLatitude: 59.8, EndLongitude: 30.4, Price: 1450,
		},
		key: uuid.New(),
	}
}

func (f *idemFixture) expectRace() {
	f.repo.EXPECT().GetIdempotencyKey(gomock.Any(), f.key).Return(nil, nil)
	f.repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(domaintrip.ErrDriverBusy)
}

func (f *idemFixture) record(hash string, tripID uuid.UUID) *model.IdempotencyRecord {
	return &model.IdempotencyRecord{TripID: tripID, RequestHash: hash}
}

func TestDo_Idempotent_FirstRequest_SavesKeyFor24h(t *testing.T) {
	f := newIdemFixture(t)
	f.repo.EXPECT().GetIdempotencyKey(gomock.Any(), f.key).Return(nil, nil)
	f.repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	f.repo.EXPECT().SaveIdempotencyKey(gomock.Any(), f.key, create.HashInput(f.in), gomock.Any(), 24*time.Hour).Return(nil)

	trip, repeated, err := f.uc.Do(context.Background(), f.in, &f.key)

	require.NoError(t, err)
	require.False(t, repeated)
	require.NotNil(t, trip)
}

func TestDo_Idempotent_SaveKeyError(t *testing.T) {
	f := newIdemFixture(t)
	boom := errors.New("db down")
	f.repo.EXPECT().GetIdempotencyKey(gomock.Any(), f.key).Return(nil, nil)
	f.repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(nil)
	f.repo.EXPECT().SaveIdempotencyKey(gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any(), gomock.Any()).Return(boom)

	_, _, err := f.uc.Do(context.Background(), f.in, &f.key)

	require.ErrorIs(t, err, boom)
}

func TestDo_Idempotent_InsertError(t *testing.T) {
	f := newIdemFixture(t)
	boom := errors.New("db down")
	f.repo.EXPECT().GetIdempotencyKey(gomock.Any(), f.key).Return(nil, nil)
	f.repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(boom)

	_, _, err := f.uc.Do(context.Background(), f.in, &f.key)

	require.ErrorIs(t, err, boom)
}

func TestDo_Race_FoundOnSecondPoll(t *testing.T) {
	f := newIdemFixture(t)
	tripID := uuid.New()
	f.expectRace()
	f.repo.EXPECT().GetIdempotencyKey(gomock.Any(), f.key).Return(nil, nil)
	f.repo.EXPECT().GetIdempotencyKey(gomock.Any(), f.key).Return(f.record(create.HashInput(f.in), tripID), nil)
	f.repo.EXPECT().GetByID(gomock.Any(), tripID).Return(&model.Trip{ID: tripID}, nil)

	trip, repeated, err := f.uc.Do(context.Background(), f.in, &f.key)

	require.NoError(t, err)
	require.True(t, repeated)
	require.Equal(t, tripID, trip.ID)
}

func TestDo_Race_ConflictingHash(t *testing.T) {
	f := newIdemFixture(t)
	f.expectRace()
	f.repo.EXPECT().GetIdempotencyKey(gomock.Any(), f.key).Return(f.record("hash-of-another-body", uuid.New()), nil)

	_, _, err := f.uc.Do(context.Background(), f.in, &f.key)

	require.ErrorIs(t, err, domaintrip.ErrIdempotencyConflict)
}

func TestDo_Race_PollError(t *testing.T) {
	f := newIdemFixture(t)
	boom := errors.New("db down")
	f.expectRace()
	f.repo.EXPECT().GetIdempotencyKey(gomock.Any(), f.key).Return(nil, boom)

	_, _, err := f.uc.Do(context.Background(), f.in, &f.key)

	require.ErrorIs(t, err, boom)
}

func TestDo_Race_GetByIDError(t *testing.T) {
	f := newIdemFixture(t)
	boom := errors.New("db down")
	tripID := uuid.New()
	f.expectRace()
	f.repo.EXPECT().GetIdempotencyKey(gomock.Any(), f.key).Return(f.record(create.HashInput(f.in), tripID), nil)
	f.repo.EXPECT().GetByID(gomock.Any(), tripID).Return(nil, boom)

	_, _, err := f.uc.Do(context.Background(), f.in, &f.key)

	require.ErrorIs(t, err, boom)
}

func TestDo_Race_Exhausted_PollsExactly20Times(t *testing.T) {
	f := newIdemFixture(t)
	f.repo.EXPECT().GetIdempotencyKey(gomock.Any(), f.key).Return(nil, nil).Times(21)
	f.repo.EXPECT().Create(gomock.Any(), gomock.Any()).Return(domaintrip.ErrDriverBusy)

	start := time.Now()
	_, _, err := f.uc.Do(context.Background(), f.in, &f.key)

	require.ErrorIs(t, err, domaintrip.ErrDriverBusy)
	require.GreaterOrEqual(t, time.Since(start), 150*time.Millisecond)
}
