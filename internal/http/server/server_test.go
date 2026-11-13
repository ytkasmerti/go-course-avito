package server_test

import (
	"context"
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/stretchr/testify/require"
	"go.uber.org/mock/gomock"

	api "github.com/ytkasmerti/go-course-avito/internal/generated"
	"github.com/ytkasmerti/go-course-avito/internal/http/domainerror"
	"github.com/ytkasmerti/go-course-avito/internal/http/server"
)

type fixture struct {
	create *MockcreateHandler
	get    *MocktripHandler
	finish *MocktripHandler
	db     *Mockpinger
	h      http.Handler
}

func newFixture(t *testing.T, pingTimeout time.Duration) *fixture {
	t.Helper()
	ctrl := gomock.NewController(t)
	f := &fixture{
		create: NewMockcreateHandler(ctrl),
		get:    NewMocktripHandler(ctrl),
		finish: NewMocktripHandler(ctrl),
		db:     NewMockpinger(ctrl),
	}
	srv := server.New(f.create, f.get, f.finish, f.db, pingTimeout)
	f.h = api.HandlerWithOptions(srv, api.ChiServerOptions{
		BaseRouter:       chi.NewRouter(),
		ErrorHandlerFunc: domainerror.InvalidParam,
	})
	return f
}

func (f *fixture) do(method, target string, headers map[string]string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, target, nil)
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	f.h.ServeHTTP(rec, req)
	return rec
}

func replyTrip(status int) func(http.ResponseWriter, *http.Request, api.TripId) {
	return func(w http.ResponseWriter, _ *http.Request, _ api.TripId) { w.WriteHeader(status) }
}

func requireInvalidRequest(t *testing.T, rec *httptest.ResponseRecorder) {
	t.Helper()
	require.Equal(t, http.StatusBadRequest, rec.Code)
	require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	var p api.Problem
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&p))
	require.Equal(t, "invalid_request", p.Code)
}

// Проверяет 400 и что хендлер не вызван
func TestInvalidTripID_IsProblemAndHandlerNotCalled(t *testing.T) {
	f := newFixture(t, time.Second)

	requireInvalidRequest(t, f.do(http.MethodGet, "/api/v1/trips/abc", nil))
	requireInvalidRequest(t, f.do(http.MethodPost, "/api/v1/trips/abc/finish", nil))
}

// Проверяет 400 на невалидный ключ
func TestInvalidIdempotencyKey_IsProblemAndHandlerNotCalled(t *testing.T) {
	f := newFixture(t, time.Second)

	requireInvalidRequest(t, f.do(http.MethodPost, "/api/v1/trips", map[string]string{"Idempotency-Key": "not-a-uuid"}))
}

// Проверяет, что валидные запросы доходят до хендлеров
func TestValidRequests_ReachHandlers(t *testing.T) {
	f := newFixture(t, time.Second)
	id := uuid.New()
	key := uuid.New()

	f.get.EXPECT().Handle(gomock.Any(), gomock.Any(), id).Do(replyTrip(http.StatusOK))
	f.finish.EXPECT().Handle(gomock.Any(), gomock.Any(), id).Do(replyTrip(http.StatusOK))
	gomock.InOrder(
		f.create.EXPECT().Handle(gomock.Any(), gomock.Any(), gomock.Any()).
			Do(func(w http.ResponseWriter, _ *http.Request, p api.CreateTripParams) {
				require.NotNil(t, p.IdempotencyKey)
				require.Equal(t, key, *p.IdempotencyKey)
				w.WriteHeader(http.StatusCreated)
			}),
		f.create.EXPECT().Handle(gomock.Any(), gomock.Any(), gomock.Any()).
			Do(func(w http.ResponseWriter, _ *http.Request, p api.CreateTripParams) {
				require.Nil(t, p.IdempotencyKey)
				w.WriteHeader(http.StatusCreated)
			}),
	)

	require.Equal(t, http.StatusOK, f.do(http.MethodGet, "/api/v1/trips/"+id.String(), nil).Code)
	require.Equal(t, http.StatusOK, f.do(http.MethodPost, "/api/v1/trips/"+id.String()+"/finish", nil).Code)
	require.Equal(t, http.StatusCreated, f.do(http.MethodPost, "/api/v1/trips", map[string]string{"Idempotency-Key": key.String()}).Code)
	require.Equal(t, http.StatusCreated, f.do(http.MethodPost, "/api/v1/trips", nil).Code)
}

// Проверяет, что /health не ходит в БД
func TestHealth_DoesNotTouchDatabase(t *testing.T) {
	f := newFixture(t, time.Second) // у мока нет EXPECT: вызов Ping провалил бы тест
	rec := f.do(http.MethodGet, "/health", nil)

	require.Equal(t, http.StatusOK, rec.Code)
	require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
	var hr api.HealthResponse
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&hr))
	require.Equal(t, api.Ok, hr.Status)
}

// Проверяет 200 и 503 в зависимости от Pin
func TestReady(t *testing.T) {
	tests := []struct {
		name       string
		pingErr    error
		wantCode   int
		wantStatus api.HealthResponseStatus
	}{
		{"db available", nil, http.StatusOK, api.Ok},
		{"db down", errors.New("down"), http.StatusServiceUnavailable, api.Unavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := newFixture(t, time.Second)
			f.db.EXPECT().Ping(gomock.Any()).Return(tt.pingErr)

			rec := f.do(http.MethodGet, "/ready", nil)

			require.Equal(t, tt.wantCode, rec.Code)
			require.Equal(t, "application/json", rec.Header().Get("Content-Type"))
			var hr api.HealthResponse
			require.NoError(t, json.NewDecoder(rec.Body).Decode(&hr))
			require.Equal(t, tt.wantStatus, hr.Status)
		})
	}
}

// Проверяет, что /ready ограничен таймаутом
func TestReady_PingIsBoundedByDeadline(t *testing.T) {
	f := newFixture(t, 50*time.Millisecond)
	f.db.EXPECT().Ping(gomock.Any()).DoAndReturn(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})

	start := time.Now()
	rec := f.do(http.MethodGet, "/ready", nil)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Less(t, time.Since(start), time.Second)
}
