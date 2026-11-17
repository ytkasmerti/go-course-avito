package container

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
)

type blockingPool struct{ release chan struct{} }

func (p blockingPool) Close() { <-p.release }

// Проверяет, что остановка закрывает пул
func TestContainer_Shutdown_ClosesPool(t *testing.T) {
	release := make(chan struct{})
	close(release)
	c := &Container{shutdown: func(context.Context) error { return nil }, pool: blockingPool{release: release}, workers: nopWorkers{}, push: nopPush{}}

	require.NoError(t, c.Shutdown(context.Background()))
}

// Проверяет, что зависший pool.Close не выводит остановку за дедлайн
func TestContainer_Shutdown_BoundedWhenPoolCloseBlocks(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	c := &Container{shutdown: func(context.Context) error { return nil }, pool: blockingPool{release: release}, workers: nopWorkers{}, push: nopPush{}}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := c.Shutdown(ctx)

	require.ErrorContains(t, err, "close pool")
	require.Less(t, time.Since(start), time.Second)
}

type nopWorkers struct{}

func (nopWorkers) Shutdown(context.Context) error { return nil }

type nopPush struct{}

func (nopPush) Close() {}

type orderWorkers struct{ order *[]string }

func (w orderWorkers) Shutdown(context.Context) error {
	*w.order = append(*w.order, "workers")
	return nil
}

type orderPush struct{ order *[]string }

func (p orderPush) Close() { *p.order = append(*p.order, "push") }

// Проверяет порядок остановки: воркеры - клиент пушей - пул - телеметрия
func TestContainer_Shutdown_Order(t *testing.T) {
	var order []string
	release := make(chan struct{})
	close(release)
	c := &Container{
		workers: orderWorkers{&order},
		push:    orderPush{&order},
		pool:    blockingPool{release: release},
		shutdown: func(context.Context) error {
			order = append(order, "telemetry")
			return nil
		},
	}
	require.NoError(t, c.Shutdown(context.Background()))
	require.Equal(t, []string{"workers", "push", "telemetry"}, order)
}
