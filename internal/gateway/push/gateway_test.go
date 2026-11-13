package push_test

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/stretchr/testify/require"

	"github.com/ytkasmerti/go-course-avito/internal/gateway/push"
)

const contentTypeJSON = "application/json"

// Проверяет тело запроса по контракту и 202
func TestClient_RequestPosition_Success(t *testing.T) {
	tripID, driverID := uuid.New(), uuid.New()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		require.Equal(t, http.MethodPost, r.Method)
		require.Equal(t, "/api/v1/push", r.URL.Path)
		require.Equal(t, contentTypeJSON, r.Header.Get("Content-Type"))
		var req struct {
			RequestID   string            `json:"request_id"`
			RecipientID string            `json:"recipient_id"`
			Kind        string            `json:"kind"`
			Data        map[string]string `json:"data"`
		}
		require.NoError(t, json.NewDecoder(r.Body).Decode(&req))
		require.NotEmpty(t, req.RequestID)
		require.Equal(t, driverID.String(), req.RecipientID)
		require.Equal(t, "REQUEST_POSITION", req.Kind)
		require.Equal(t, tripID.String(), req.Data["trip_id"])
		w.WriteHeader(http.StatusAccepted)
	}))
	t.Cleanup(srv.Close)
	c := push.New(srv.URL, time.Second)
	t.Cleanup(c.Close)
	require.NoError(t, c.RequestPosition(context.Background(), tripID, driverID))
}

// Проверяет различение 4xx, 429 и 5xx
func TestClient_RequestPosition_StatusErrors(t *testing.T) {
	tests := []struct {
		name    string
		status  int
		wantErr error
	}{
		{"bad request", http.StatusBadRequest, push.ErrRejected},
		{"rate limited", http.StatusTooManyRequests, push.ErrUnavailable},
		{"unavailable", http.StatusServiceUnavailable, push.ErrUnavailable},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(tt.status)
			}))
			t.Cleanup(srv.Close)
			c := push.New(srv.URL, time.Second)
			t.Cleanup(c.Close)
			require.ErrorIs(t, c.RequestPosition(context.Background(), uuid.New(), uuid.New()), tt.wantErr)
		})
	}
}

// Проверяет ошибку транспорта, сервер недоступен
func TestClient_RequestPosition_TransportError(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) {}))
	url := srv.URL
	srv.Close()
	c := push.New(url, time.Second)
	t.Cleanup(c.Close)
	err := c.RequestPosition(context.Background(), uuid.New(), uuid.New())
	require.Error(t, err)
	require.NotErrorIs(t, err, push.ErrRejected)
	require.NotErrorIs(t, err, push.ErrUnavailable)
}
