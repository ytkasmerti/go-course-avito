package push

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"time"

	"github.com/google/uuid"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

// Различаем отказ по контракту и временную недоступность, транспорт отдельно
var (
	ErrRejected    = errors.New("push rejected")
	ErrUnavailable = errors.New("push unavailable")
)

type Client struct {
	url  string
	http *http.Client
}

func New(url string, timeout time.Duration) *Client {
	return &Client{
		url: url,
		http: &http.Client{
			Timeout:   timeout,
			Transport: otelhttp.NewTransport(http.DefaultTransport),
		},
	}
}

type pushRequest struct {
	RequestID   string            `json:"request_id"`
	RecipientID string            `json:"recipient_id"`
	Kind        string            `json:"kind"`
	Data        map[string]string `json:"data,omitempty"`
}

// Просит устройство водителя прислать координату, одна попытка
func (c *Client) RequestPosition(ctx context.Context, tripID, driverID uuid.UUID) error {
	body, err := json.Marshal(pushRequest{
		RequestID:   uuid.NewString(),
		RecipientID: driverID.String(),
		Kind:        "REQUEST_POSITION",
		Data:        map[string]string{"trip_id": tripID.String()},
	})
	if err != nil {
		return fmt.Errorf("marshal push request: %w", err)
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, c.url+"/api/v1/push", bytes.NewReader(body))
	if err != nil {
		return fmt.Errorf("build push request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	resp, err := c.http.Do(req)
	if err != nil {
		return fmt.Errorf("push transport: %w", err)
	}
	defer func() {
		_, _ = io.Copy(io.Discard, resp.Body)
		_ = resp.Body.Close()
	}()
	switch {
	case resp.StatusCode == http.StatusAccepted:
		return nil
	case resp.StatusCode == http.StatusTooManyRequests || resp.StatusCode >= http.StatusInternalServerError:
		return fmt.Errorf("%w: status %d", ErrUnavailable, resp.StatusCode)
	case resp.StatusCode >= http.StatusBadRequest:
		return fmt.Errorf("%w: status %d", ErrRejected, resp.StatusCode)
	default:
		return fmt.Errorf("push: unexpected status %d", resp.StatusCode)
	}
}

// закрывает живые соединения при остановке сервиса
func (c *Client) Close() { c.http.CloseIdleConnections() }
