package create_test

import (
	"bytes"
	"encoding/json"
	"errors"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	api "github.com/ytkasmerti/go-course-avito/internal/generated"
	handlerCreate "github.com/ytkasmerti/go-course-avito/internal/http/trip/create"
	"github.com/ytkasmerti/go-course-avito/internal/model"
	usecaseCreate "github.com/ytkasmerti/go-course-avito/internal/usecase/trip/create"
)

const validBody = `{
	"user_id":"5cb72c04-7650-45c9-a79b-bcdba0631e0c",
	"driver_id":"8860b315-ec86-42eb-a17c-7c163d721ff5",
	"start_point":{"latitude":59.9,"longitude":30.3},
	"end_point":{"latitude":59.8,"longitude":30.2},
	"price":1450
}`

const (
	uid = "5cb72c04-7650-45c9-a79b-bcdba0631e0c"
	did = "8860b315-ec86-42eb-a17c-7c163d721ff5"
)

var testLog = slog.New(slog.DiscardHandler)

// Проверяет 201 и Location
func TestHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockUC := NewMockcreateUsecase(ctrl)
	tripID := uuid.New()
	mockUC.EXPECT().Do(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		&model.Trip{ID: tripID, Status: "active"}, false, nil,
	)
	h := handlerCreate.New(mockUC, testLog)
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

// Проверяет 400 на неизвестные поля
func TestHandler_InvalidBody(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockUC := NewMockcreateUsecase(ctrl)
	h := handlerCreate.New(mockUC, testLog)
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

// Проверяет 409 с занятым водителем
func TestHandler_DriverBusy(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockUC := NewMockcreateUsecase(ctrl)
	mockUC.EXPECT().Do(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		nil, false, domaintrip.ErrDriverBusy,
	)
	h := handlerCreate.New(mockUC, testLog)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips", bytes.NewBufferString(validBody))
	rec := httptest.NewRecorder()
	h.Handle(rec, req, api.CreateTripParams{})
	if rec.Code != http.StatusConflict {
		t.Errorf("status: got %d, want 409", rec.Code)
	}
}

// Проверяет 500 без деталей в теле
func TestHandler_InternalError(t *testing.T) {
	ctrl := gomock.NewController(t)
	defer ctrl.Finish()
	mockUC := NewMockcreateUsecase(ctrl)
	mockUC.EXPECT().Do(gomock.Any(), gomock.Any(), gomock.Any()).Return(
		nil, false, errors.New("db down"),
	)
	h := handlerCreate.New(mockUC, testLog)
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

func serve(uc *MockcreateUsecase, body string) *httptest.ResponseRecorder {
	h := handlerCreate.New(uc, testLog)
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.Handle(rec, req, api.CreateTripParams{})
	return rec
}

// Проверяет 400 на неполное и битое тело
func TestHandler_RejectsIncompleteOrMalformedBodies(t *testing.T) {
	pt := `{"latitude":59.9,"longitude":30.3}`
	tests := []struct {
		name       string
		body       string
		wantDetail string
	}{
		{"only user_id and driver_id", `{"user_id":"` + uid + `","driver_id":"` + did + `"}`, "start_point: required"},
		{"no user_id", `{"driver_id":"` + did + `","start_point":` + pt + `,"end_point":` + pt + `,"price":1}`, "user_id: required"},
		{"null user_id", `{"user_id":null,"driver_id":"` + did + `","start_point":` + pt + `,"end_point":` + pt + `,"price":1}`, "user_id: required"},
		{"no price", `{"user_id":"` + uid + `","driver_id":"` + did + `","start_point":` + pt + `,"end_point":` + pt + `}`, "price: required"},
		{"start_point without latitude", `{"user_id":"` + uid + `","driver_id":"` + did + `","start_point":{"longitude":30.3},"end_point":` + pt + `,"price":1}`, "start_point.latitude: required"},
		{"end_point without longitude", `{"user_id":"` + uid + `","driver_id":"` + did + `","start_point":` + pt + `,"end_point":{"latitude":59.9},"price":1}`, "end_point.longitude: required"},
		{"second JSON object", validBody + `{}`, "unexpected data after JSON object"},
		{"garbage after JSON", validBody + ` garbage`, "unexpected data after JSON object"},
		{"empty body", ``, "body:"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			rec := serve(NewMockcreateUsecase(ctrl), tt.body)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
			var p api.Problem
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&p))
			require.Equal(t, "invalid_request", p.Code)
			require.NotNil(t, p.Detail)
			require.Contains(t, *p.Detail, tt.wantDetail)
		})
	}
}

// Проверяет, что явные нули допустимы
func TestHandler_ExplicitZeroValuesAreAccepted(t *testing.T) {
	ctrl := gomock.NewController(t)
	uc := NewMockcreateUsecase(ctrl)
	userID, driverID := uuid.MustParse(uid), uuid.MustParse(did)
	uc.EXPECT().
		Do(gomock.Any(), usecaseCreate.Input{UserID: userID, DriverID: driverID}, gomock.Any()).
		Return(&model.Trip{ID: uuid.New(), Status: "active"}, false, nil)

	zero := `{"latitude":0,"longitude":0}`
	rec := serve(uc, `{"user_id":"`+uid+`","driver_id":"`+did+`","start_point":`+zero+`,"end_point":`+zero+`,"price":0}`)
	require.Equal(t, http.StatusCreated, rec.Code)
}

// Проверяет 400 от доменной валидации
func TestHandler_DomainValidationErrorBecomes400(t *testing.T) {
	ctrl := gomock.NewController(t)
	uc := NewMockcreateUsecase(ctrl)
	uc.EXPECT().Do(gomock.Any(), gomock.Any(), gomock.Any()).
		Return(nil, false, &domaintrip.ValidationError{Field: "price", Message: "must be >= 0"})

	rec := serve(uc, strings.Replace(validBody, `"price":1450`, `"price":-1`, 1))
	require.Equal(t, http.StatusBadRequest, rec.Code)
}
