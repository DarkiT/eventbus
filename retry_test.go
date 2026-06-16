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

// TestSubscribeReliableRetriesUntilSuccess 验证失败后自动重试直至成功（异步路径）。
func TestSubscribeReliableRetriesUntilSuccess(t *testing.T) {
	bus := New(8)
	defer bus.Close()

	var attempts atomic.Int32
	var done atomic.Bool
	require.NoError(t, bus.SubscribeReliable(
		"reliable.success",
		func(ctx context.Context, topic string, payload any) error {
			if attempts.Add(1) < 3 {
				return errors.New("transient")
			}
			done.Store(true)
			return nil
		},
		WithMaxAttempts(5),
		WithBackoff(ConstantBackoff(time.Millisecond)),
	))

	require.NoError(t, bus.Publish("reliable.success", 1))
	require.Eventually(t, done.Load, time.Second, time.Millisecond)
	assert.Equal(t, int32(3), attempts.Load(), "前两次失败、第三次成功，共尝试 3 次")
}

// TestSubscribeReliableDeadLetterOnExhaust 验证重试耗尽后触发死信回调。
func TestSubscribeReliableDeadLetterOnExhaust(t *testing.T) {
	bus := New(8)
	defer bus.Close()

	var attempts atomic.Int32
	var mu sync.Mutex
	var dlqTopic string
	var dlqErr error
	require.NoError(t, bus.SubscribeReliable(
		"reliable.fail",
		func(ctx context.Context, topic string, payload any) error {
			attempts.Add(1)
			return errors.New("always fail")
		},
		WithMaxAttempts(3),
		WithBackoff(ConstantBackoff(time.Millisecond)),
		WithDeadLetter(func(topic string, payload any, err error) {
			mu.Lock()
			defer mu.Unlock()
			dlqTopic = topic
			dlqErr = err
		}),
	))

	require.NoError(t, bus.Publish("reliable.fail", "payload"))
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return dlqErr != nil
	}, time.Second, time.Millisecond)

	assert.Equal(t, int32(3), attempts.Load(), "应尝试满 3 次")
	mu.Lock()
	defer mu.Unlock()
	assert.Equal(t, "reliable.fail", dlqTopic)
	assert.ErrorContains(t, dlqErr, "always fail")
}

// TestSubscribeReliableRetryIfSkipsNonRetryable 验证 WithRetryIf 让不可重试错误只执行一次。
func TestSubscribeReliableRetryIfSkipsNonRetryable(t *testing.T) {
	bus := New(8)
	defer bus.Close()

	sentinel := errors.New("nonretryable")
	var attempts atomic.Int32
	var mu sync.Mutex
	var dlqErr error
	require.NoError(t, bus.SubscribeReliable(
		"reliable.if",
		func(ctx context.Context, topic string, payload any) error {
			attempts.Add(1)
			return sentinel
		},
		WithMaxAttempts(5),
		WithBackoff(ConstantBackoff(time.Millisecond)),
		WithRetryIf(func(err error) bool { return !errors.Is(err, sentinel) }),
		WithDeadLetter(func(topic string, payload any, err error) {
			mu.Lock()
			dlqErr = err
			mu.Unlock()
		}),
	))

	require.NoError(t, bus.Publish("reliable.if", 1))
	require.Eventually(t, func() bool {
		mu.Lock()
		defer mu.Unlock()
		return dlqErr != nil
	}, time.Second, time.Millisecond)

	assert.Equal(t, int32(1), attempts.Load(), "不可重试错误应只尝试 1 次")
	mu.Lock()
	defer mu.Unlock()
	assert.ErrorIs(t, dlqErr, sentinel)
}

// TestSubscribeReliablePanicBecomesRetryableError 验证 panic 被转为可重试错误。
func TestSubscribeReliablePanicBecomesRetryableError(t *testing.T) {
	bus := New(8)
	defer bus.Close()

	var attempts atomic.Int32
	var done atomic.Bool
	require.NoError(t, bus.SubscribeReliable(
		"reliable.panic",
		func(ctx context.Context, topic string, payload any) error {
			if attempts.Add(1) == 1 {
				panic("boom")
			}
			done.Store(true)
			return nil
		},
		WithMaxAttempts(3),
		WithBackoff(ConstantBackoff(time.Millisecond)),
	))

	require.NoError(t, bus.Publish("reliable.panic", 1))
	require.Eventually(t, done.Load, time.Second, time.Millisecond)
	assert.Equal(t, int32(2), attempts.Load(), "首次 panic 后第二次成功")
}

// TestSubscribeReliableSyncContextCancelInterruptsRetry 验证同步路径下调用方 ctx 取消可中断重试。
func TestSubscribeReliableSyncContextCancelInterruptsRetry(t *testing.T) {
	bus := New(8)
	defer bus.Close()

	var attempts atomic.Int32
	require.NoError(t, bus.SubscribeReliable(
		"reliable.cancel",
		func(ctx context.Context, topic string, payload any) error {
			attempts.Add(1)
			return errors.New("fail")
		},
		WithMaxAttempts(20),
		WithBackoff(ConstantBackoff(50*time.Millisecond)),
	))

	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Millisecond)
	defer cancel()
	_ = bus.PublishSyncWithContext(ctx, "reliable.cancel", 1)

	// 调用方 ctx 在退避等待中被取消，attempts 不应达到上限
	assert.Less(t, attempts.Load(), int32(20), "ctx 取消应中断重试")
	assert.GreaterOrEqual(t, attempts.Load(), int32(1))
}

// TestSubscribeReliableDefaultConfig 验证零配置即获得安全默认。
func TestSubscribeReliableDefaultConfig(t *testing.T) {
	cfg := applyRetryOptions(nil)
	assert.Equal(t, 3, cfg.maxAttempts)
	require.NotNil(t, cfg.backoff)
	require.NotNil(t, cfg.retryIf)
	assert.True(t, cfg.retryIf(errors.New("any error")))
	assert.Nil(t, cfg.deadLetter, "默认不配置死信")
}

// TestBackoffStrategies 验证各退避构造器的输出特征。
func TestBackoffStrategies(t *testing.T) {
	t.Run("constant", func(t *testing.T) {
		cb := ConstantBackoff(10 * time.Millisecond)
		assert.Equal(t, 10*time.Millisecond, cb(1))
		assert.Equal(t, 10*time.Millisecond, cb(9))
	})

	t.Run("linear", func(t *testing.T) {
		lb := LinearBackoff(10 * time.Millisecond)
		assert.Equal(t, 10*time.Millisecond, lb(1))
		assert.Equal(t, 50*time.Millisecond, lb(5))
	})

	t.Run("exponential with jitter and cap", func(t *testing.T) {
		eb := ExponentialBackoff(10*time.Millisecond, 100*time.Millisecond)
		// attempt=1：base 10ms，±25% jitter → [7.5ms, 12.5ms]
		d1 := eb(1)
		assert.GreaterOrEqual(t, d1, 7*time.Millisecond)
		assert.LessOrEqual(t, d1, 13*time.Millisecond)
		// attempt=2：base 20ms，±25% → [15ms, 25ms]
		d2 := eb(2)
		assert.GreaterOrEqual(t, d2, 15*time.Millisecond)
		assert.LessOrEqual(t, d2, 25*time.Millisecond)
		// attempt 很大：封顶 100ms，jitter → [75ms, 100ms]
		dbig := eb(20)
		assert.GreaterOrEqual(t, dbig, 75*time.Millisecond)
		assert.LessOrEqual(t, dbig, 100*time.Millisecond)
	})

	t.Run("non-positive degrades to zero", func(t *testing.T) {
		zero := ExponentialBackoff(0, 100*time.Millisecond)
		assert.Equal(t, time.Duration(0), zero(1))
	})
}

// TestSubscribeReliableNilHandlerRejected 验证空 handler 被拒绝。
func TestSubscribeReliableNilHandlerRejected(t *testing.T) {
	bus := New(8)
	defer bus.Close()
	err := bus.SubscribeReliable("reliable.nil", nil)
	assert.ErrorIs(t, err, ErrHandlerIsNotFunc)
}
