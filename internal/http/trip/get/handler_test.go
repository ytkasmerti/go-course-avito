package get_test

import (
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	handlerGet "github.com/ytkasmerti/go-course-avito/internal/http/trip/get"
	"github.com/ytkasmerti/go-course-avito/internal/model"
)

var testLog = slog.New(slog.DiscardHandler)

// Проверяет 200
func TestHandlerGet_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUC := NewMockgetUsecase(ctrl)
	id := uuid.New()
	mockUC.EXPECT().Do(gomock.Any(), id).Return(&model.Trip{ID: id}, nil)

	h := handlerGet.New(mockUC, testLog)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String(), nil)
	rec := httptest.NewRecorder()
	h.Handle(rec, req, id)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rec.Code)
	}
}

// Проверяет 404
func TestHandlerGet_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUC := NewMockgetUsecase(ctrl)
	id := uuid.New()
	mockUC.EXPECT().Do(gomock.Any(), id).Return(nil, domaintrip.ErrTripNotFound)

	h := handlerGet.New(mockUC, testLog)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String(), nil)
	rec := httptest.NewRecorder()
	h.Handle(rec, req, id)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", rec.Code)
	}
}

// Проверяет 500
func TestHandlerGet_InternalError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUC := NewMockgetUsecase(ctrl)
	id := uuid.New()
	mockUC.EXPECT().Do(gomock.Any(), id).Return(nil, errors.New("boom"))

	h := handlerGet.New(mockUC, testLog)
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String(), nil)
	rec := httptest.NewRecorder()
	h.Handle(rec, req, id)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", rec.Code)
	}
}
