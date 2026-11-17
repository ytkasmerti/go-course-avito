package config

import (
	"fmt"
	"os"
	"strconv"
	"time"
)

type Config struct {
	HTTPAddr                string
	DatabaseURL             string
	LogLevel                string
	ShutdownTimeout         time.Duration
	DatabaseMaxConns        int32
	DatabaseMinConns        int32
	DatabaseMaxConnLifetime time.Duration
	DatabaseConnectTimeout  time.Duration
	DatabaseQueryTimeout    time.Duration
	HTTPReadTimeout         time.Duration
	HTTPReadHeaderTimeout   time.Duration
	HTTPWriteTimeout        time.Duration
	HTTPIdleTimeout         time.Duration
	PositionCheckInterval   time.Duration
	PositionStaleAfter      time.Duration
	PositionWorkers         int
	PositionQueueSize       int
	PushHTTPURL             string
	PushRequestTimeout      time.Duration
	PositionScanBatchSize   int
}

func Load() (*Config, error) {
	httpAddr := os.Getenv("HTTP_ADDR")
	if httpAddr == "" {
		return nil, fmt.Errorf("HTTP_ADDR is required")
	}

	dbURL := os.Getenv("DATABASE_URL")
	if dbURL == "" {
		return nil, fmt.Errorf("DATABASE_URL is required")
	}

	logLevel := os.Getenv("LOG_LEVEL")
	if logLevel == "" {
		return nil, fmt.Errorf("LOG_LEVEL is required")
	}

	pushURL := os.Getenv("PUSH_HTTP_URL")
	if pushURL == "" {
		return nil, fmt.Errorf("PUSH_HTTP_URL is required")
	}

	shutdownTimeout, err := time.ParseDuration(os.Getenv("SHUTDOWN_TIMEOUT"))
	if err != nil {
		return nil, fmt.Errorf("parse SHUTDOWN_TIMEOUT: %w", err)
	}

	maxConns, err := strconv.ParseInt(os.Getenv("DATABASE_MAX_CONNS"), 10, 32)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_MAX_CONNS: %w", err)
	}

	minConns, err := strconv.ParseInt(os.Getenv("DATABASE_MIN_CONNS"), 10, 32)
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_MIN_CONNS: %w", err)
	}

	maxConnLifetime, err := time.ParseDuration(os.Getenv("DATABASE_MAX_CONN_LIFETIME"))
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_MAX_CONN_LIFETIME: %w", err)
	}

	connectTimeout, err := time.ParseDuration(os.Getenv("DATABASE_CONNECT_TIMEOUT"))
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_CONNECT_TIMEOUT: %w", err)
	}

	queryTimeout, err := time.ParseDuration(os.Getenv("DATABASE_QUERY_TIMEOUT"))
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_QUERY_TIMEOUT: %w", err)
	}

	httpReadTimeout, err := time.ParseDuration(os.Getenv("HTTP_READ_TIMEOUT"))
	if err != nil {
		return nil, fmt.Errorf("parse HTTP_READ_TIMEOUT: %w", err)
	}

	httpReadHeaderTimeout, err := time.ParseDuration(os.Getenv("HTTP_READ_HEADER_TIMEOUT"))
	if err != nil {
		return nil, fmt.Errorf("parse HTTP_READ_HEADER_TIMEOUT: %w", err)
	}

	httpWriteTimeout, err := time.ParseDuration(os.Getenv("HTTP_WRITE_TIMEOUT"))
	if err != nil {
		return nil, fmt.Errorf("parse HTTP_WRITE_TIMEOUT: %w", err)
	}

	httpIdleTimeout, err := time.ParseDuration(os.Getenv("HTTP_IDLE_TIMEOUT"))
	if err != nil {
		return nil, fmt.Errorf("parse HTTP_IDLE_TIMEOUT: %w", err)
	}

	checkInterval, err := envDuration("POSITION_CHECK_INTERVAL", 30*time.Second)
	if err != nil {
		return nil, err
	}

	staleAfter, err := envDuration("POSITION_STALE_AFTER", 60*time.Second)
	if err != nil {
		return nil, err
	}

	pushTimeout, err := envDuration("PUSH_REQUEST_TIMEOUT", time.Second)
	if err != nil {
		return nil, err
	}

	workers, err := envInt("POSITION_WORKERS", 4)
	if err != nil {
		return nil, err
	}

	queueSize, err := envInt("POSITION_QUEUE_SIZE", 100)
	if err != nil {
		return nil, err
	}

	scanBatchSize, err := envInt("POSITION_SCAN_BATCH_SIZE", 100)
	if err != nil {
		return nil, err
	}

	cfg := &Config{
		HTTPAddr:                httpAddr,
		DatabaseURL:             dbURL,
		LogLevel:                logLevel,
		ShutdownTimeout:         shutdownTimeout,
		DatabaseMaxConns:        int32(maxConns),
		DatabaseMinConns:        int32(minConns),
		DatabaseMaxConnLifetime: maxConnLifetime,
		DatabaseConnectTimeout:  connectTimeout,
		DatabaseQueryTimeout:    queryTimeout,
		HTTPReadTimeout:         httpReadTimeout,
		HTTPReadHeaderTimeout:   httpReadHeaderTimeout,
		HTTPWriteTimeout:        httpWriteTimeout,
		HTTPIdleTimeout:         httpIdleTimeout,
		PositionCheckInterval:   checkInterval,
		PositionStaleAfter:      staleAfter,
		PositionWorkers:         workers,
		PositionQueueSize:       queueSize,
		PushHTTPURL:             pushURL,
		PushRequestTimeout:      pushTimeout,
		PositionScanBatchSize:   scanBatchSize,
	}

	if err := cfg.validate(); err != nil {
		return nil, err
	}

	return cfg, nil
}

func (c *Config) validate() error {
	durations := []struct {
		name string
		v    time.Duration
	}{
		{"SHUTDOWN_TIMEOUT", c.ShutdownTimeout},
		{"DATABASE_MAX_CONN_LIFETIME", c.DatabaseMaxConnLifetime},
		{"DATABASE_CONNECT_TIMEOUT", c.DatabaseConnectTimeout},
		{"DATABASE_QUERY_TIMEOUT", c.DatabaseQueryTimeout},
		{"HTTP_READ_TIMEOUT", c.HTTPReadTimeout},
		{"HTTP_READ_HEADER_TIMEOUT", c.HTTPReadHeaderTimeout},
		{"HTTP_WRITE_TIMEOUT", c.HTTPWriteTimeout},
		{"HTTP_IDLE_TIMEOUT", c.HTTPIdleTimeout},
		{"POSITION_CHECK_INTERVAL", c.PositionCheckInterval},
		{"POSITION_STALE_AFTER", c.PositionStaleAfter},
		{"PUSH_REQUEST_TIMEOUT", c.PushRequestTimeout},
	}

	for _, d := range durations {
		if d.v <= 0 {
			return fmt.Errorf("%s must be positive, got %s", d.name, d.v)
		}
	}

	if c.DatabaseMaxConns <= 0 {
		return fmt.Errorf("DATABASE_MAX_CONNS must be > 0, got %d", c.DatabaseMaxConns)
	}

	if c.DatabaseMinConns < 0 {
		return fmt.Errorf("DATABASE_MIN_CONNS must be >= 0, got %d", c.DatabaseMinConns)
	}

	if c.DatabaseMinConns > c.DatabaseMaxConns {
		return fmt.Errorf("DATABASE_MIN_CONNS (%d) must not exceed DATABASE_MAX_CONNS (%d)",
			c.DatabaseMinConns, c.DatabaseMaxConns)
	}

	if c.PositionWorkers <= 0 {
		return fmt.Errorf("POSITION_WORKERS must be > 0, got %d", c.PositionWorkers)
	}

	if c.PositionQueueSize <= 0 {
		return fmt.Errorf("POSITION_QUEUE_SIZE must be > 0, got %d", c.PositionQueueSize)
	}

	if c.PositionScanBatchSize <= 0 {
		return fmt.Errorf("POSITION_SCAN_BATCH_SIZE must be > 0, got %d", c.PositionScanBatchSize)
	}

	return nil
}

func envDuration(name string, def time.Duration) (time.Duration, error) {
	v := os.Getenv(name)
	if v == "" {
		return def, nil
	}
	d, err := time.ParseDuration(v)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return d, nil
}

func envInt(name string, def int) (int, error) {
	v := os.Getenv(name)
	if v == "" {
		return def, nil
	}
	n, err := strconv.Atoi(v)
	if err != nil {
		return 0, fmt.Errorf("parse %s: %w", name, err)
	}
	return n, nil
}
