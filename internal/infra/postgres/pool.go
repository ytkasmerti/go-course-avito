package postgres

import (
	"context"
	"errors"
	"fmt"
	"regexp"
	"strings"
	"time"

	"github.com/exaring/otelpgx"
	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/codes"
	"go.opentelemetry.io/otel/metric"
	"go.opentelemetry.io/otel/trace"

	"github.com/ytkasmerti/go-course-avito/internal/config"
)

func NewPool(ctx context.Context, cfg config.Config) (*pgxpool.Pool, error) {
	poolCfg, err := pgxpool.ParseConfig(cfg.DatabaseURL)
	if err != nil {
		return nil, fmt.Errorf("parse db config: %w", err)
	}

	poolCfg.MaxConns = cfg.DatabaseMaxConns
	poolCfg.MinConns = cfg.DatabaseMinConns
	poolCfg.MaxConnLifetime = cfg.DatabaseMaxConnLifetime
	tracer, err := newQueryTracer(otelpgx.NewTracer())
	if err != nil {
		return nil, fmt.Errorf("init db tracer: %w", err)
	}
	poolCfg.ConnConfig.Tracer = tracer
	// Ограничивает время выполнения SQL на уровне сессии
	poolCfg.ConnConfig.RuntimeParams["statement_timeout"] = fmt.Sprintf("%d", cfg.DatabaseQueryTimeout.Milliseconds())

	pool, err := pgxpool.NewWithConfig(ctx, poolCfg)
	if err != nil {
		return nil, fmt.Errorf("create pool: %w", err)
	}

	pingCtx, cancel := context.WithTimeout(ctx, cfg.DatabaseConnectTimeout)
	defer cancel()
	if err := pool.Ping(pingCtx); err != nil {
		pool.Close()
		return nil, fmt.Errorf("ping db: %w", err)
	}

	return pool, nil
}

// Добавляет к спанам otelpgx атрибуты операции и метрику длительности
type queryTracer struct {
	*otelpgx.Tracer
	duration metric.Float64Histogram
}

type queryStartKey struct{}

type queryStart struct {
	at        time.Time
	operation string
	table     string
}

var tableRe = regexp.MustCompile(`(?i)\b(?:from|into|update)\s+([a-z_][a-z0-9_]*)`)

func newQueryTracer(inner *otelpgx.Tracer) (*queryTracer, error) {
	duration, err := otel.Meter("trip-service/db").Float64Histogram(
		"db.client.operation.duration",
		metric.WithUnit("s"),
		metric.WithDescription("Duration of database client operations"),
	)
	if err != nil {
		return nil, err
	}
	return &queryTracer{Tracer: inner, duration: duration}, nil
}

func (t *queryTracer) TraceQueryStart(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryStartData) context.Context {
	ctx = t.Tracer.TraceQueryStart(ctx, conn, data)

	var op, table string
	if fields := strings.Fields(data.SQL); len(fields) > 0 {
		op = strings.ToUpper(fields[0])
	}
	if m := tableRe.FindStringSubmatch(data.SQL); m != nil {
		table = m[1]
	}
	trace.SpanFromContext(ctx).SetAttributes(
		attribute.String("db.system.name", "postgresql"),
		attribute.String("db.operation.name", op),
		attribute.String("db.collection.name", table),
	)
	return context.WithValue(ctx, queryStartKey{}, queryStart{at: time.Now(), operation: op, table: table})
}

func (t *queryTracer) TraceQueryEnd(ctx context.Context, conn *pgx.Conn, data pgx.TraceQueryEndData) {
	if s, ok := ctx.Value(queryStartKey{}).(queryStart); ok {
		attrs := []attribute.KeyValue{
			attribute.String("db.system.name", "postgresql"),
			attribute.String("db.operation.name", s.operation),
			attribute.String("db.collection.name", s.table),
		}
		if data.Err != nil {
			errType := errorType(data.Err)
			attrs = append(attrs, attribute.String("error.type", errType))
			// до inner.TraceQueryEnd: он завершает спан
			span := trace.SpanFromContext(ctx)
			span.SetAttributes(attribute.String("error.type", errType))
			span.SetStatus(codes.Error, errType)
		}
		t.duration.Record(ctx, time.Since(s.at).Seconds(), metric.WithAttributes(attrs...))
	}
	t.Tracer.TraceQueryEnd(ctx, conn, data)
}

// дает значение SQLSTATE для ошибок Postgres, иначе класс
func errorType(err error) string {
	var pgErr *pgconn.PgError
	switch {
	case errors.As(err, &pgErr):
		return pgErr.Code
	case errors.Is(err, context.DeadlineExceeded):
		return "timeout"
	case errors.Is(err, context.Canceled):
		return "canceled"
	default:
		return "_OTHER"
	}
}
