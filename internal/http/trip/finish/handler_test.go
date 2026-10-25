package finish_test

import (
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	handlerFinish "github.com/ytkasmerti/go-course-avito/internal/http/trip/finish"
	"github.com/ytkasmerti/go-course-avito/internal/model"
)

func TestHandlerFinish_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUC := NewMockfinishUsecase(ctrl)
	id := uuid.New()
	mockUC.EXPECT().Do(gomock.Any(), id).Return(&model.Trip{
		ID: id, Status: string(domaintrip.StatusCompleted),
	}, nil)

	h := handlerFinish.New(mockUC)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+id.String()+"/finish", nil)
	rec := httptest.NewRecorder()
	h.Handle(rec, req, id)

	if rec.Code != http.StatusOK {
		t.Errorf("status: got %d, want 200", rec.Code)
	}
}

func TestHandlerFinish_AlreadyCompleted(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUC := NewMockfinishUsecase(ctrl)
	id := uuid.New()
	mockUC.EXPECT().Do(gomock.Any(), id).Return(nil, domaintrip.ErrTripCompleted)

	h := handlerFinish.New(mockUC)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+id.String()+"/finish", nil)
	rec := httptest.NewRecorder()
	h.Handle(rec, req, id)

	if rec.Code != http.StatusConflict {
		t.Errorf("status: got %d, want 409", rec.Code)
	}
}

func TestHandlerFinish_NotFound(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUC := NewMockfinishUsecase(ctrl)
	id := uuid.New()
	mockUC.EXPECT().Do(gomock.Any(), id).Return(nil, domaintrip.ErrTripNotFound)

	h := handlerFinish.New(mockUC)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+id.String()+"/finish", nil)
	rec := httptest.NewRecorder()
	h.Handle(rec, req, id)

	if rec.Code != http.StatusNotFound {
		t.Errorf("status: got %d, want 404", rec.Code)
	}
}

func TestHandlerFinish_InternalError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()

	mockUC := NewMockfinishUsecase(ctrl)
	id := uuid.New()
	mockUC.EXPECT().Do(gomock.Any(), id).Return(nil, errors.New("boom"))

	h := handlerFinish.New(mockUC)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+id.String()+"/finish", nil)
	rec := httptest.NewRecorder()
	h.Handle(rec, req, id)

	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", rec.Code)
	}
}
