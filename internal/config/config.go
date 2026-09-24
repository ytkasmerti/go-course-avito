package config

import (
	"fmt"
	"os"
	"time"
	"strconv"
)

type Config struct {
	HTTPAddr        string
	DatabaseURL     string
	LogLevel        string
	ShutdownTimeout time.Duration
	DatabaseMaxConns   int32
	DatabaseMinConns   int32
	DatabaseMaxConnLifetime time.Duration
	DatabaseConnectTimeout  time.Duration
	DatabaseQueryTimeout    time.Duration
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
	
	maxConns, err := strconv.Atoi(os.Getenv("DATABASE_MAX_CONNS"))
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_MAX_CONNS: %w", err)
	}

	minConns, err := strconv.Atoi(os.Getenv("DATABASE_MIN_CONNS"))
	if err != nil {
		return nil, fmt.Errorf("parse DATABASE_MIN_CONNS: %w", err)
	}

	maxConnLifetime, err := time.ParseDuration(os.Getenv("DATABASE_MAX_CONN_LIFETIME"))
	if err != nil {
		return nil, fmt.Errorf("Parse DATABASE_MAX_CONN_LIFETIME: %w", err)
	}

	connectTimeout, err := time.ParseDuration(os.Getenv("DATABASE_CONNECT_TIMEOUT"))
	if err != nil {
		return nil, fmt.Errorf("Parse DATABASE_CONNECT_TIMEOUT: %w", err)
	}

	queryTimeout, err := time.ParseDuration(os.Getenv("DATABASE_QUERY_TIMEOUT"))
	if err != nil {
		return nil, fmt.Errorf("Parse DATABASE_QUERY_TIMEOUT: %w", err)
	}
	
	return &Config{
		HTTPAddr: httpAddr,
		DatabaseURL: dbURL,
		LogLevel: logLevel,
		ShutdownTimeout: shutdownTimeout,
		DatabaseMaxConns: int32(maxConns),
		DatabaseMinConns: int32(minConns),
		DatabaseMaxConnLifetime: maxConnLifetime,
		DatabaseConnectTimeout: connectTimeout,
		DatabaseQueryTimeout: queryTimeout,
	}, nil
}