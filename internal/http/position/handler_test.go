package position_test

import (
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
	handler "github.com/ytkasmerti/go-course-avito/internal/http/position"
	"github.com/ytkasmerti/go-course-avito/internal/model"
)

var testLog = slog.New(slog.DiscardHandler)

const (
	validBody   = `{"coordinates":{"latitude":59.9,"longitude":30.3},"recorded_at":"2026-09-01T10:05:00Z"}`
	problemType = "application/problem+json"
)

func serveSave(uc *MocksaveUsecase, body string) *httptest.ResponseRecorder {
	h := handler.NewSave(uc, testLog)
	id := uuid.New()
	req := httptest.NewRequest(http.MethodPost, "/api/v1/trips/"+id.String()+"/positions", strings.NewReader(body))
	rec := httptest.NewRecorder()
	h.Handle(rec, req, id)
	return rec
}

func serveList(uc *MocklistUsecase) *httptest.ResponseRecorder {
	h := handler.NewList(uc, testLog)
	id := uuid.New()
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/"+id.String()+"/positions", nil)
	rec := httptest.NewRecorder()
	h.Handle(rec, req, id)
	return rec
}

// Проверяет 201 и тело по контракту
func TestSaveHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	uc := NewMocksaveUsecase(ctrl)
	uc.EXPECT().Do(gomock.Any(), gomock.Any()).
		Return(&model.Position{ID: 42, TripID: uuid.New(), Latitude: 59.9, Longitude: 30.3}, nil)
	rec := serveSave(uc, validBody)
	require.Equal(t, http.StatusCreated, rec.Code)
	require.Contains(t, rec.Body.String(), `"id":42`)
}

// Проверяет 400 на неполное и битое тело
func TestSaveHandler_InvalidBody(t *testing.T) {
	tests := []struct{ name, body, wantDetail string }{
		{"no coordinates", `{"recorded_at":"2026-09-01T10:05:00Z"}`, "coordinates: required"},
		{"no latitude", `{"coordinates":{"longitude":30.3},"recorded_at":"2026-09-01T10:05:00Z"}`, "coordinates.latitude: required"},
		{"no recorded_at", `{"coordinates":{"latitude":59.9,"longitude":30.3}}`, "recorded_at: required"},
		{"bad recorded_at", `{"coordinates":{"latitude":59.9,"longitude":30.3},"recorded_at":"yesterday"}`, "body:"},
		{"unknown field", `{"foo":"bar"}`, "body:"},
		{"second object", validBody + `{}`, "unexpected data after JSON object"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			rec := serveSave(NewMocksaveUsecase(ctrl), tt.body)
			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, problemType, rec.Header().Get("Content-Type"))
			var p api.Problem
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&p))
			require.Equal(t, "invalid_request", p.Code)
			require.Contains(t, *p.Detail, tt.wantDetail)
		})
	}
}

// Проверяет 404, 409 и 500 без деталей в теле
func TestSaveHandler_Errors(t *testing.T) {
	tests := []struct {
		name     string
		err      error
		wantCode int
	}{
		{"not found", domaintrip.ErrTripNotFound, http.StatusNotFound},
		{"completed", domaintrip.ErrTripCompleted, http.StatusConflict},
		{"internal", errors.New("db down"), http.StatusInternalServerError},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			ctrl := gomock.NewController(t)
			uc := NewMocksaveUsecase(ctrl)
			uc.EXPECT().Do(gomock.Any(), gomock.Any()).Return(nil, tt.err)
			rec := serveSave(uc, validBody)
			require.Equal(t, tt.wantCode, rec.Code)
			require.NotContains(t, rec.Body.String(), "db down")
		})
	}
}

// Проверяет 200 с маршрутом
func TestListHandler_Success(t *testing.T) {
	ctrl := gomock.NewController(t)
	uc := NewMocklistUsecase(ctrl)
	uc.EXPECT().Do(gomock.Any(), gomock.Any()).
		Return([]model.Position{{ID: 1}, {ID: 2}}, nil)
	rec := serveList(uc)
	require.Equal(t, http.StatusOK, rec.Code)
	var resp api.TripPositionsResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&resp))
	require.Len(t, resp.Positions, 2)
	require.Equal(t, int64(1), resp.Positions[0].Id)
}

// Проверяет 404 и пустой маршрут как []
func TestListHandler_NotFoundAndEmpty(t *testing.T) {
	ctrl := gomock.NewController(t)
	uc := NewMocklistUsecase(ctrl)
	uc.EXPECT().Do(gomock.Any(), gomock.Any()).Return(nil, domaintrip.ErrTripNotFound)
	require.Equal(t, http.StatusNotFound, serveList(uc).Code)

	uc2 := NewMocklistUsecase(ctrl)
	uc2.EXPECT().Do(gomock.Any(), gomock.Any()).Return(nil, nil)
	require.Contains(t, serveList(uc2).Body.String(), `"positions":[]`)
}
