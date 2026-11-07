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

	api "github.com/ytkasmerti/go-course-avito/internal/generated"
	"github.com/ytkasmerti/go-course-avito/internal/http/domainerror"
	"github.com/ytkasmerti/go-course-avito/internal/http/server"
)

type createStub struct {
	calls  int
	hadKey bool
}

func (s *createStub) Handle(w http.ResponseWriter, _ *http.Request, p api.CreateTripParams) {
	s.calls++
	s.hadKey = p.IdempotencyKey != nil
	w.WriteHeader(http.StatusCreated)
}

type tripStub struct {
	calls int
	id    api.TripId
}

func (s *tripStub) Handle(w http.ResponseWriter, _ *http.Request, id api.TripId) {
	s.calls++
	s.id = id
	w.WriteHeader(http.StatusOK)
}

type pingFunc func(ctx context.Context) error

func (f pingFunc) Ping(ctx context.Context) error { return f(ctx) }

type fixture struct {
	create *createStub
	get    *tripStub
	finish *tripStub
	h      http.Handler
}

func newFixture(ping pingFunc, timeout time.Duration) *fixture {
	f := &fixture{create: &createStub{}, get: &tripStub{}, finish: &tripStub{}}
	srv := server.New(f.create, f.get, f.finish, ping, timeout)
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
	f := newFixture(nil, time.Second)

	requireInvalidRequest(t, f.do(http.MethodGet, "/api/v1/trips/abc", nil))
	requireInvalidRequest(t, f.do(http.MethodPost, "/api/v1/trips/abc/finish", nil))

	require.Zero(t, f.get.calls)
	require.Zero(t, f.finish.calls)
}

// Проверяет 400 на невалидный ключ
func TestInvalidIdempotencyKey_IsProblemAndHandlerNotCalled(t *testing.T) {
	f := newFixture(nil, time.Second)

	requireInvalidRequest(t, f.do(http.MethodPost, "/api/v1/trips", map[string]string{"Idempotency-Key": "not-a-uuid"}))
	require.Zero(t, f.create.calls)
}

// Проверяет, что валидные запросы доходят до хендлеров
func TestValidRequests_ReachHandlers(t *testing.T) {
	f := newFixture(nil, time.Second)
	id := uuid.New()

	require.Equal(t, http.StatusOK, f.do(http.MethodGet, "/api/v1/trips/"+id.String(), nil).Code)
	require.Equal(t, 1, f.get.calls)
	require.Equal(t, id, f.get.id)

	require.Equal(t, http.StatusOK, f.do(http.MethodPost, "/api/v1/trips/"+id.String()+"/finish", nil).Code)
	require.Equal(t, 1, f.finish.calls)
	require.Equal(t, id, f.finish.id)

	require.Equal(t, http.StatusCreated, f.do(http.MethodPost, "/api/v1/trips", map[string]string{"Idempotency-Key": uuid.NewString()}).Code)
	require.True(t, f.create.hadKey)

	require.Equal(t, http.StatusCreated, f.do(http.MethodPost, "/api/v1/trips", nil).Code)
	require.False(t, f.create.hadKey)
}

// Проверяет, что /health не ходит в БД
func TestHealth_DoesNotTouchDatabase(t *testing.T) {
	f := newFixture(nil, time.Second) // nil-пинг упал бы с паникой при обращении к БД
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
		ping       pingFunc
		wantCode   int
		wantStatus api.HealthResponseStatus
	}{
		{"db available", func(context.Context) error { return nil }, http.StatusOK, api.Ok},
		{"db down", func(context.Context) error { return errors.New("down") }, http.StatusServiceUnavailable, api.Unavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			rec := newFixture(tt.ping, time.Second).do(http.MethodGet, "/ready", nil)

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
	hang := pingFunc(func(ctx context.Context) error {
		<-ctx.Done()
		return ctx.Err()
	})
	start := time.Now()
	rec := newFixture(hang, 50*time.Millisecond).do(http.MethodGet, "/ready", nil)

	require.Equal(t, http.StatusServiceUnavailable, rec.Code)
	require.Less(t, time.Since(start), time.Second)
}
