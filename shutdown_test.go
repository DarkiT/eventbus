package eventbus

import (
	"context"
	"errors"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

// TestShutdownDrainsBufferedQueue 验证 Shutdown 在超时范围内排空有缓冲队列，
// 已入队消息全部到达处理器（快乐路径）。
func TestShutdownDrainsBufferedQueue(t *testing.T) {
	bus := New(256)
	var got atomic.Int32
	require.NoError(t, bus.Subscribe("drain.topic", func(topic string, payload any) {
		time.Sleep(time.Millisecond) // 轻微耗时，制造队列积压
		got.Add(1)
	}))

	const n = 100
	for i := range n {
		require.NoError(t, bus.Publish("drain.topic", i))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	require.NoError(t, bus.Shutdown(ctx))

	assert.Equal(t, int32(n), got.Load(), "Shutdown 应排空队列，所有已入队消息都到达处理器")
}

// TestShutdownTimeoutClosesAnyway 验证 ctx 超时后仍执行关闭，并返回 DeadlineExceeded。
// handler 使用有界 sleep（不响应取消但有界），Close 等待当前消息处理完后回收 loop。
func TestShutdownTimeoutClosesAnyway(t *testing.T) {
	bus := New(8)
	require.NoError(t, bus.Subscribe("drain.slow", func(topic string, payload any) {
		time.Sleep(20 * time.Millisecond)
	}))
	for i := range 5 {
		require.NoError(t, bus.Publish("drain.slow", i))
	}

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	start := time.Now()
	err := bus.Shutdown(ctx)
	elapsed := time.Since(start)

	require.Error(t, err)
	assert.ErrorIs(t, err, context.DeadlineExceeded)
	assert.True(t, bus.closed.Load(), "超时后总线应已关闭")
	// 防止 hang：Close 等待 loop 退出不应远超一个 handler 周期 + 积压量
	assert.Less(t, elapsed, time.Second)
}

// TestShutdownRejectsNewPublish 验证 draining 标记后异步/同步发布都被拒绝。
func TestShutdownRejectsNewPublish(t *testing.T) {
	bus := New(8)
	require.NoError(t, bus.Subscribe("drain.reject", func(topic string, payload any) {}))
	require.NoError(t, bus.Publish("drain.reject", 1))

	// 模拟 Shutdown 中段：已置 draining 但尚未 Close
	bus.draining.Store(true)

	assert.ErrorIs(t, bus.Publish("drain.reject", 2), ErrShuttingDown)
	assert.ErrorIs(t, bus.PublishSync("drain.reject", 3), ErrShuttingDown)
	_, err := bus.PublishBatch([]BatchMessage{{Topic: "drain.reject", Payload: 4}})
	require.Error(t, err)
	assert.NotErrorIs(t, err, ErrEventBusClosed) // 应是 shutting down 而非 closed

	// 还原后正常关闭，避免 goroutine 泄漏
	bus.draining.Store(false)
	bus.Close()
}

// TestShutdownThenCloseIdempotent 验证 Shutdown 后 Close 幂等、再发布返回 closed。
func TestShutdownThenCloseIdempotent(t *testing.T) {
	bus := New(8)
	require.NoError(t, bus.Subscribe("drain.idem", func(topic string, payload any) {}))
	require.NoError(t, bus.Publish("drain.idem", 1))

	require.NoError(t, bus.Shutdown(context.Background()))
	assert.NotPanics(t, func() { bus.Close() }, "Shutdown 后 Close 应幂等")
	assert.ErrorIs(t, bus.Publish("drain.idem", 2), ErrEventBusClosed)
}

// TestShutdownConcurrentPublishAndDrain 验证并发发布与 Shutdown 不 panic、最终一致关闭。
func TestShutdownConcurrentPublishAndDrain(t *testing.T) {
	bus := New(64)
	var got atomic.Int32
	require.NoError(t, bus.Subscribe("drain.concurrent", func(topic string, payload any) {
		got.Add(1)
	}))

	var wg sync.WaitGroup
	stop := make(chan struct{})
	for range 4 {
		wg.Go(func() {
			for {
				select {
				case <-stop:
					return
				default:
				}
				if err := bus.Publish("drain.concurrent", 1); err != nil {
					return // Shutdown 后预期被拒
				}
			}
		})
	}

	// 让生产者跑一会儿形成积压
	time.Sleep(20 * time.Millisecond)

	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	require.NoError(t, bus.Shutdown(ctx))
	close(stop)
	wg.Wait()

	assert.NotPanics(t, func() { bus.Close() })
	// 已到达处理器的消息数应等于实际投递成功数（不为零即可，并发下发布部分被拒）
	assert.Greater(t, got.Load(), int32(0))
}

// TestShutdownWaitsReliableRetryBeforeClose 验证 Shutdown 不只看队列 len，
// 还会等待当前已取出的可靠处理器完成重试与死信，避免队列刚清空就 Close 取消重试。
func TestShutdownWaitsReliableRetryBeforeClose(t *testing.T) {
	bus := New(8)

	var attempts atomic.Int32
	deadLetter := make(chan error, 1)
	require.NoError(t, bus.SubscribeReliable(
		"drain.reliable",
		func(ctx context.Context, topic string, payload any) error {
			attempts.Add(1)
			return errors.New("retry me")
		},
		WithMaxAttempts(3),
		WithBackoff(ConstantBackoff(10*time.Millisecond)),
		WithDeadLetter(func(topic string, payload any, err error) {
			deadLetter <- err
		}),
	))

	require.NoError(t, bus.Publish("drain.reliable", 1))

	ctx, cancel := context.WithTimeout(context.Background(), time.Second)
	defer cancel()
	require.NoError(t, bus.Shutdown(ctx))

	assert.Equal(t, int32(3), attempts.Load(), "Shutdown 应等待当前可靠处理器完成全部重试")
	select {
	case err := <-deadLetter:
		assert.ErrorContains(t, err, "retry me")
	default:
		t.Fatal("Shutdown 返回前应已触发死信回调")
	}
}
