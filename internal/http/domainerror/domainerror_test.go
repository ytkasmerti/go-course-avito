package domainerror_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/stretchr/testify/require"

	domaintrip "github.com/ytkasmerti/go-course-avito/internal/domain/trip"
	api "github.com/ytkasmerti/go-course-avito/internal/generated"
	"github.com/ytkasmerti/go-course-avito/internal/http/domainerror"
)

// Проверяет маппинг доменных ошибок в HTTP-код и code
func TestProblem(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantStatus int
		wantCode   string
	}{
		{"validation", &domaintrip.ValidationError{Field: "x", Message: "y"}, http.StatusBadRequest, "invalid_request"},
		{"not found", domaintrip.ErrTripNotFound, http.StatusNotFound, "trip_not_found"},
		{"driver busy", domaintrip.ErrDriverBusy, http.StatusConflict, "driver_busy"},
		{"completed", domaintrip.ErrTripCompleted, http.StatusConflict, "trip_completed"},
		{"idempotency conflict", domaintrip.ErrIdempotencyConflict, http.StatusConflict, "idempotency_conflict"},
		{"unknown", errors.New("boom"), http.StatusInternalServerError, "internal_error"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			status, p := domainerror.Problem(tt.err)
			if status != tt.wantStatus {
				t.Errorf("status: got %d, want %d", status, tt.wantStatus)
			}
			if p.Code != tt.wantCode {
				t.Errorf("code: got %s, want %s", p.Code, tt.wantCode)
			}
		})
	}
}

// Проверяет problem+json для ошибок разбора параметров
func TestInvalidParam(t *testing.T) {
	tests := []struct {
		name       string
		err        error
		wantDetail string
	}{
		{"invalid format", &api.InvalidParamFormatError{ParamName: "tripId", Err: errors.New("bad uuid")}, "tripId"},
		{"too many values", &api.TooManyValuesForParamError{ParamName: "Idempotency-Key", Count: 2}, "Idempotency-Key"},
		{"unknown error", errors.New("boom"), "parameter"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := httptest.NewRecorder()
			domainerror.InvalidParam(rec, httptest.NewRequest(http.MethodGet, "/x", nil), tt.err)

			require.Equal(t, http.StatusBadRequest, rec.Code)
			require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
			var p api.Problem
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&p))
			require.Equal(t, "invalid_request", p.Code)
			require.Contains(t, *p.Detail, tt.wantDetail)
			require.NotContains(t, *p.Detail, "boom")
		})
	}
}

type ctxKey struct{}

type captureHandler struct {
	records []slog.Record
	ctxs    []context.Context
}

func (h *captureHandler) Enabled(context.Context, slog.Level) bool { return true }
func (h *captureHandler) Handle(ctx context.Context, r slog.Record) error {
	h.records = append(h.records, r)
	h.ctxs = append(h.ctxs, ctx)
	return nil
}
func (h *captureHandler) WithAttrs([]slog.Attr) slog.Handler { return h }
func (h *captureHandler) WithGroup(string) slog.Handler      { return h }

// Проверяет лог и скрытие деталей 500
func TestWrite_LogsInternalErrorOnceAndHidesItFromClient(t *testing.T) {
	h := &captureHandler{}
	req := httptest.NewRequest(http.MethodGet, "/api/v1/trips/x", nil)
	req = req.WithContext(context.WithValue(req.Context(), ctxKey{}, "marker"))
	rec := httptest.NewRecorder()

	cause := errors.New("connection reset by peer")
	domainerror.Write(rec, req, slog.New(h), fmt.Errorf("select trip: %w", cause))

	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.NotContains(t, rec.Body.String(), "connection reset")

	require.Len(t, h.records, 1)
	require.Equal(t, slog.LevelError, h.records[0].Level)

	// контекст запроса доезжает до логгера, значит trace_id попадет в запись
	require.Equal(t, "marker", h.ctxs[0].Value(ctxKey{}))

	var logged error
	h.records[0].Attrs(func(a slog.Attr) bool {
		if a.Key == "err" {
			logged, _ = a.Value.Any().(error)
		}
		return true
	})
	require.ErrorIs(t, logged, cause)
}

// Проверяет что 4xx не пишутся в лог ошибок
func TestWrite_DoesNotLogExpectedClientErrors(t *testing.T) {
	for _, err := range []error{
		domaintrip.ErrTripNotFound,
		domaintrip.ErrTripCompleted,
		domaintrip.ErrDriverBusy,
		&domaintrip.ValidationError{Field: "price", Message: "must be >= 0"},
	} {
		h := &captureHandler{}
		rec := httptest.NewRecorder()
		domainerror.Write(rec, httptest.NewRequest(http.MethodGet, "/", nil), slog.New(h), err)

		require.Less(t, rec.Code, http.StatusInternalServerError)
		require.Empty(t, h.records, "%v не должна попадать в лог ошибок", err)
	}
}
