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

	return nil
}
