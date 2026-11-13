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
	c := &Container{shutdown: func(context.Context) error { return nil }, pool: blockingPool{release: release}}

	require.NoError(t, c.Shutdown(context.Background()))
}

// Проверяет, что зависший pool.Close не выводит остановку за дедлайн
func TestContainer_Shutdown_BoundedWhenPoolCloseBlocks(t *testing.T) {
	release := make(chan struct{})
	t.Cleanup(func() { close(release) })
	c := &Container{shutdown: func(context.Context) error { return nil }, pool: blockingPool{release: release}}

	ctx, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := c.Shutdown(ctx)

	require.ErrorContains(t, err, "close pool")
	require.Less(t, time.Since(start), time.Second)
}
