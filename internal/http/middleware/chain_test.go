package middleware_test

import (
	"bytes"
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/go-chi/chi/v5"
	"github.com/stretchr/testify/require"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/codes"
	sdkmetric "go.opentelemetry.io/otel/sdk/metric"
	"go.opentelemetry.io/otel/sdk/metric/metricdata"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	api "github.com/ytkasmerti/go-course-avito/internal/generated"
	"github.com/ytkasmerti/go-course-avito/internal/http/middleware"
)

type harness struct {
	router http.Handler
	logs   *bytes.Buffer
	spans  *tracetest.SpanRecorder
	reader *sdkmetric.ManualReader
}

// Провайдеры ставим до сборки цепочки
func newHarness(t *testing.T) *harness {
	t.Helper()

	spans := tracetest.NewSpanRecorder()
	otel.SetTracerProvider(sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(spans)))
	reader := sdkmetric.NewManualReader()
	otel.SetMeterProvider(sdkmetric.NewMeterProvider(sdkmetric.WithReader(reader)))

	logs := &bytes.Buffer{}
	router := chi.NewRouter()
	chain, err := middleware.Chain(router, slog.New(slog.NewJSONHandler(logs, nil)))
	require.NoError(t, err)
	router.Use(chain...)
	router.Get("/boom", func(http.ResponseWriter, *http.Request) { panic("boom") })
	router.Get("/ok", func(w http.ResponseWriter, _ *http.Request) { w.WriteHeader(http.StatusNoContent) })

	return &harness{router: router, logs: logs, spans: spans, reader: reader}
}

func (h *harness) get(path string) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.router.ServeHTTP(rec, httptest.NewRequest(http.MethodGet, path, nil))
	return rec
}

func (h *harness) logRecord(t *testing.T, msg string) map[string]any {
	t.Helper()
	for _, line := range strings.Split(strings.TrimSpace(h.logs.String()), "\n") {
		var m map[string]any
		require.NoError(t, json.Unmarshal([]byte(line), &m))
		if m["msg"] == msg {
			return m
		}
	}
	t.Fatalf("в логе нет записи %q:\n%s", msg, h.logs.String())
	return nil
}

func (h *harness) durationPoint(t *testing.T, route string) metricdata.HistogramDataPoint[float64] {
	t.Helper()
	var rm metricdata.ResourceMetrics
	require.NoError(t, h.reader.Collect(context.Background(), &rm))
	for _, sm := range rm.ScopeMetrics {
		for _, m := range sm.Metrics {
			if m.Name != "http.server.request.duration" {
				continue
			}
			hist, ok := m.Data.(metricdata.Histogram[float64])
			require.True(t, ok)
			for _, dp := range hist.DataPoints {
				if v, _ := dp.Attributes.Value("http.route"); v.AsString() == route {
					return dp
				}
			}
		}
	}
	t.Fatalf("нет метрики длительности для маршрута %s", route)
	return metricdata.HistogramDataPoint[float64]{}
}

func (h *harness) span(t *testing.T, name string) sdktrace.ReadOnlySpan {
	t.Helper()
	for _, s := range h.spans.Ended() {
		if s.Name() == name {
			return s
		}
	}
	t.Fatalf("нет завершённого спана %q", name)
	return nil
}

// Проверяет единый статус 500 в ответе, логе, метрике и спане
func TestChain_PanicIsRecordedEverywhereAsStatus500(t *testing.T) {
	h := newHarness(t)

	rec := h.get("/boom")

	// ответ клиенту
	require.Equal(t, http.StatusInternalServerError, rec.Code)
	require.Equal(t, "application/problem+json", rec.Header().Get("Content-Type"))
	var p api.Problem
	require.NoError(t, json.NewDecoder(rec.Body).Decode(&p))
	require.Equal(t, "internal_error", p.Code)
	require.NotContains(t, rec.Body.String(), "boom")

	// лог: и запись о панике, и итоговая запись о запросе
	h.logRecord(t, "panic")
	reqLog := h.logRecord(t, "request handled")
	require.Equal(t, "ERROR", reqLog["level"])
	require.EqualValues(t, 500, reqLog["status"])
	require.Equal(t, "/boom", reqLog["route"])

	// метрика длительности со статусом 500
	dp := h.durationPoint(t, "/boom")
	code, ok := dp.Attributes.Value("http.response.status_code")
	require.True(t, ok)
	require.EqualValues(t, 500, code.AsInt64())
	require.EqualValues(t, 1, dp.Count)

	// спан: статус Error, error.type и событие с причиной паники
	s := h.span(t, "GET /boom")
	require.Equal(t, codes.Error, s.Status().Code)
	attrs := map[string]string{}
	for _, kv := range s.Attributes() {
		attrs[string(kv.Key)] = kv.Value.String()
	}
	require.Equal(t, "500", attrs["error.type"])
	require.Equal(t, "/boom", attrs["http.route"])
	var recorded bool
	for _, ev := range s.Events() {
		recorded = recorded || ev.Name == "exception"
	}
	require.True(t, recorded, "паника должна быть записана в спан")
}

// Проверяет что успешный запрос не помечается как ошибка
func TestChain_SuccessfulRequestIsNotMarkedAsError(t *testing.T) {
	h := newHarness(t)

	require.Equal(t, http.StatusNoContent, h.get("/ok").Code)

	reqLog := h.logRecord(t, "request handled")
	require.Equal(t, "INFO", reqLog["level"])
	require.EqualValues(t, 204, reqLog["status"])

	s := h.span(t, "GET /ok")
	require.Equal(t, codes.Unset, s.Status().Code)
	for _, kv := range s.Attributes() {
		require.NotEqual(t, "error.type", string(kv.Key))
	}
}
