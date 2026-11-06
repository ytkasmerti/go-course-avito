package txmanager

import (
	"context"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
)

type TxManager interface {
	Do(ctx context.Context, fn func(ctx context.Context) error) error
}

type Manager struct {
	pool    *pgxpool.Pool
	timeout time.Duration
}

type ctxKey struct{}

func New(pool *pgxpool.Pool, timeout time.Duration) TxManager {
	return &Manager{pool: pool, timeout: timeout}
}

func (m *Manager) Do(ctx context.Context, fn func(ctx context.Context) error) error {
	//Проверка, есть ли транзакция в контексте
	if _, ok := TxFromContext(ctx); ok {
		return fn(ctx)
	}

	//Открытие транзакции
	beginCtx, cancelBegin := context.WithTimeout(ctx, m.timeout)
	tx, err := m.pool.BeginTx(beginCtx, pgx.TxOptions{
		IsoLevel: pgx.ReadCommitted,
	})
	cancelBegin()
	if err != nil {
		return fmt.Errorf("Begin tx: %w", err)
	}

	//Откат при панике
	defer func() {
		if p := recover(); p != nil {
			_ = tx.Rollback(ctx)
			panic(p)
		}
	}()

	//Кладем в контекст
	ctxWithTx := context.WithValue(ctx, ctxKey{}, tx)

	//Откат при ошибке
	if err := fn(ctxWithTx); err != nil {
		_ = tx.Rollback(ctx)
		return err
	}

	//Коммит
	commitCtx, cancelCommit := context.WithTimeout(ctx, m.timeout)
	defer cancelCommit()
	if err := tx.Commit(commitCtx); err != nil {
		return fmt.Errorf("Commit tx: %w", err)
	}
	return nil
}

func TxFromContext(ctx context.Context) (pgx.Tx, bool) {
	tx, ok := ctx.Value(ctxKey{}).(pgx.Tx)
	return tx, ok
}
