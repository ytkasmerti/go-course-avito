package config_test

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/ytkasmerti/go-course-avito/internal/config"
)

const (
	envMaxConns  = "DATABASE_MAX_CONNS"
	envMinConns  = "DATABASE_MIN_CONNS"
	envWorkers   = "POSITION_WORKERS"
	envQueueSize = "POSITION_QUEUE_SIZE"
	envBatchSize = "POSITION_BATCH_SIZE"
)

var durationVars = []string{
	"SHUTDOWN_TIMEOUT", "DATABASE_MAX_CONN_LIFETIME", "DATABASE_CONNECT_TIMEOUT", "DATABASE_QUERY_TIMEOUT",
	"HTTP_READ_TIMEOUT", "HTTP_READ_HEADER_TIMEOUT", "HTTP_WRITE_TIMEOUT", "HTTP_IDLE_TIMEOUT",
	"POSITION_CHECK_INTERVAL", "POSITION_STALE_AFTER", "PUSH_REQUEST_TIMEOUT",
}

func validEnv() map[string]string {
	return map[string]string{
		"HTTP_ADDR":                  ":8080",
		"DATABASE_URL":               "postgres://localhost:5432/db?sslmode=disable",
		"LOG_LEVEL":                  "info",
		"SHUTDOWN_TIMEOUT":           "10s",
		"HTTP_READ_TIMEOUT":          "5s",
		"HTTP_READ_HEADER_TIMEOUT":   "2s",
		"HTTP_WRITE_TIMEOUT":         "10s",
		"HTTP_IDLE_TIMEOUT":          "60s",
		envMaxConns:                  "10",
		envMinConns:                  "2",
		"DATABASE_MAX_CONN_LIFETIME": "30m",
		"DATABASE_CONNECT_TIMEOUT":   "5s",
		"DATABASE_QUERY_TIMEOUT":     "3s",
		"PUSH_HTTP_URL":              "http://localhost:8090",
	}
}

func load(t *testing.T, overrides map[string]string) (*config.Config, error) {
	t.Helper()
	env := validEnv()
	for k, v := range overrides {
		env[k] = v
	}
	for k, v := range env {
		t.Setenv(k, v)
	}
	return config.Load()
}

// Проверяет загрузку с дефолтными и граничными значениями
func TestLoad_Valid(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]string
	}{
		{"defaults", nil},
		{"min conns zero", map[string]string{envMinConns: "0"}},
		{"min equals max", map[string]string{envMinConns: "10", envMaxConns: "10"}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := load(t, tt.overrides)
			require.NoError(t, err)
			require.NotNil(t, cfg)
		})
	}
}

// Проверяет ошибку при нулевых и отрицательных таймаутах
func TestLoad_NonPositiveDurations(t *testing.T) {
	for _, name := range durationVars {
		for _, v := range []string{"0s", "-1s"} {
			t.Run(name+"="+v, func(t *testing.T) {
				_, err := load(t, map[string]string{name: v})
				require.ErrorContains(t, err, name)
			})
		}
	}
}

// Проверяет ошибки размеров пула
func TestLoad_InvalidPoolSizes(t *testing.T) {
	tests := []struct {
		name      string
		overrides map[string]string
		wantVar   string
	}{
		{"max zero", map[string]string{envMaxConns: "0"}, envMaxConns},
		{"max negative", map[string]string{envMaxConns: "-1"}, envMaxConns},
		{"min negative", map[string]string{envMinConns: "-1"}, envMinConns},
		{"min above max", map[string]string{envMinConns: "20", envMaxConns: "5"}, envMinConns},
		{"max overflows int32", map[string]string{envMaxConns: "99999999999"}, envMaxConns},
		{"max not a number", map[string]string{envMaxConns: "abc"}, envMaxConns},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := load(t, tt.overrides)
			require.ErrorContains(t, err, tt.wantVar)
		})
	}
}

// Проверяет дефолты работы 3 и обязательность PUSH_HTTP_URL
func TestLoad_PositionDefaults(t *testing.T) {
	cfg, err := load(t, map[string]string{
		"POSITION_CHECK_INTERVAL": "", "POSITION_STALE_AFTER": "", "PUSH_REQUEST_TIMEOUT": "",
		envWorkers: "", envQueueSize: "", envBatchSize: "",
	})
	require.NoError(t, err)
	require.Equal(t, 30*time.Second, cfg.PositionCheckInterval)
	require.Equal(t, 60*time.Second, cfg.PositionStaleAfter)
	require.Equal(t, time.Second, cfg.PushRequestTimeout)
	require.Equal(t, 4, cfg.PositionWorkers)
	require.Equal(t, 100, cfg.PositionQueueSize)
	require.Equal(t, 100, cfg.PositionBatchSize)

	_, err = load(t, map[string]string{"PUSH_HTTP_URL": ""})
	require.ErrorContains(t, err, "PUSH_HTTP_URL")
}

// Проверяет ошибки размеров пула воркеров и очереди
func TestLoad_InvalidPositionSizes(t *testing.T) {
	tests := []struct{ name, variable, value string }{
		{"workers zero", envWorkers, "0"},
		{"workers negative", envWorkers, "-1"},
		{"workers not a number", envWorkers, "abc"},
		{"queue zero", envQueueSize, "0"},
		{"queue negative", envQueueSize, "-2"},
		{"batch zero", envBatchSize, "0"},
		{"batch negative", envBatchSize, "-1"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := load(t, map[string]string{tt.variable: tt.value})
			require.ErrorContains(t, err, tt.variable)
		})
	}
}
