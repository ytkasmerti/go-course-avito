package config_test

import (
	"testing"

	"github.com/stretchr/testify/require"

	"github.com/ytkasmerti/go-course-avito/internal/config"
)

const (
	envMaxConns = "DATABASE_MAX_CONNS"
	envMinConns = "DATABASE_MIN_CONNS"
)

var durationVars = []string{
	"SHUTDOWN_TIMEOUT", "DATABASE_MAX_CONN_LIFETIME", "DATABASE_CONNECT_TIMEOUT", "DATABASE_QUERY_TIMEOUT",
	"HTTP_READ_TIMEOUT", "HTTP_READ_HEADER_TIMEOUT", "HTTP_WRITE_TIMEOUT", "HTTP_IDLE_TIMEOUT",
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
