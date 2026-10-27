package create_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/google/uuid"
	"go.uber.org/mock/gomock"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	api "github.com/ytkasmerti/go-course-avito/internal/generated"
	handlerCreate "github.com/ytkasmerti/go-course-avito/internal/http/trip/create"
	"github.com/ytkasmerti/go-course-avito/internal/model"
)

const validBody = `{
	"user_id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c",
	"driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5",
	"start_point":{"latitude":59.9,"longitude":30.3},
	"end_point":{"latitude":59.8,"longitude":30.2},
	"price":1450
}`

func TestHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockUC := NewMockcreateUsecase(ctrl)
	tripID := uuid.New()
	mockUC.EXPECT().Do(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		&model.Trip{ID: tripID, Status: "active"}, false, nil)
	h := handlerCreate.New(mockUC)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips", bytes.NewBufferString(validBody))
	rec := httptest.NewRecorder()
	h.Handle(rec, req, api.CreateTripParams{})
	if rec.Code != http.StatusCreated {
		t.Errorf("status: got %d, want 201", rec.Code)
	}
	if rec.Header().Get("Location") == "" {
		t.Error("Location header missing")
	}
}

func TestHandler_InvalidBody(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockUC := NewMockcreateUsecase(ctrl)
	h := handlerCreate.New(mockUC)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips", bytes.NewBufferString(`{"foo":"bar"}`))
	rec := httptest.NewRecorder()
	h.Handle(rec, req, api.CreateTripParams{})
	if rec.Code != http.StatusBadRequest {
		t.Errorf("status: got %d, want 400", rec.Code)
	}
	var p api.Problem
	if err := json.NewDecoder(rec.Body).Decode(&p); err != nil {
		t.Fatalf("decode problem: %v", err)
	}
	if p.Code != "invalid_request" {
		t.Errorf("code: got %s, want invalid_request", p.Code)
	}
}

func TestHandler_DriverBusy(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockUC := NewMockcreateUsecase(ctrl)
	mockUC.EXPECT().Do(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		nil, false, domaintrip.ErrDriverBusy)
	h := handlerCreate.New(mockUC)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips", bytes.NewBufferString(validBody))
	rec := httptest.NewRecorder()
	h.Handle(rec, req, api.CreateTripParams{})
	if rec.Code != http.StatusConflict {
		t.Errorf("status: got %d, want 409", rec.Code)
	}
}

func TestHandler_InternalError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockUC := NewMockcreateUsecase(ctrl)
	mockUC.EXPECT().Do(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		nil, false, errors.New("db down"))
	h := handlerCreate.New(mockUC)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips", bytes.NewBufferString(validBody))
	rec := httptest.NewRecorder()
	h.Handle(rec, req, api.CreateTripParams{})
	if rec.Code != http.StatusInternalServerError {
		t.Errorf("status: got %d, want 500", rec.Code)
	}
	if bytes.Contains(rec.Body.Bytes(), []byte("db down")) {
		t.Error("internal error details leaked to response")
	}
}
