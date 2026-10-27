package get_test

import (
	"context"
	"errors"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	"github.com/ytkasmerti/go-course-avito/internal/model"
	uc "github.com/ytkasmerti/go-course-avito/internal/usecase/trip/get"
)

func TestUsecase_Get_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := NewMocktripRepository(ctrl)
	id := uuid.New()
	repo.EXPECT().GetByID(gomock.Any(), id).Return(&model.Trip{ID: id}, nil)

	u := uc.New(repo)
	trip, err := u.Do(context.Background(), id)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if trip.ID != id {
		t.Errorf("want %v, got %v", id, trip.ID)
	}
}

func TestUsecase_Get_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	repo := NewMocktripRepository(ctrl)
	id := uuid.New()
	repo.EXPECT().GetByID(gomock.Any(), id).Return(nil, domaintrip.ErrTripNotFound)

	u := uc.New(repo)
	_, err := u.Do(context.Background(), id)
	if !errors.Is(err, domaintrip.ErrTripNotFound) {
		t.Fatalf("want ErrTripNotFound, got %v", err)
	}
}
