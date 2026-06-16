package eventbus

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestEventBusSetTracerCanSwitchAndClear(t *testing.T) {
	bus := New()
	defer bus.Close()

	require.NotPanics(t, func() {
		bus.SetTracer(NewMetricsTracer())
		bus.SetTracer(&testTracer{})
		bus.SetTracer(nil)
		bus.SetTracer(NewMetricsTracer())
	})
}

func TestEventBusUseNilMiddlewareIsIgnored(t *testing.T) {
	bus := New()
	defer bus.Close()

	bus.Use(nil)

	var called atomic.Int32
	require.NoError(t, bus.Subscribe("nil.middleware", func(topic string, payload any) {
		called.Add(1)
	}))

	require.NoError(t, bus.PublishSync("nil.middleware", "payload"))
	assert.Equal(t, int32(1), called.Load())
}

func TestEventBusMiddlewarePanicIsReturnedAsError(t *testing.T) {
	t.Run("before", func(t *testing.T) {
		bus := New()
		defer bus.Close()

		bus.Use(MiddlewareFunc{
			BeforeFn: func(topic string, payload any) any {
				panic("before boom")
			},
		})
		require.NoError(t, bus.Subscribe("middleware.before.panic", func(topic string, payload any) {}))

		err := bus.PublishSync("middleware.before.panic", "payload")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "middleware before panic")
	})

	t.Run("after", func(t *testing.T) {
		bus := New()
		defer bus.Close()

		bus.Use(MiddlewareFunc{
			AfterFn: func(topic string, payload any) {
				panic("after boom")
			},
		})
		require.NoError(t, bus.Subscribe("middleware.after.panic", func(topic string, payload any) {}))

		err := bus.PublishSync("middleware.after.panic", "payload")
		require.Error(t, err)
		assert.Contains(t, err.Error(), "middleware after panic")
	})
}

func TestPublishBatchRunsMiddlewareAfterWhenNoSubscriber(t *testing.T) {
	bus := New()
	defer bus.Close()

	var beforeCalled atomic.Int32
	var afterCalled atomic.Int32
	bus.Use(MiddlewareFunc{
		BeforeFn: func(topic string, payload any) any {
			beforeCalled.Add(1)
			return payload
		},
		AfterFn: func(topic string, payload any) {
			afterCalled.Add(1)
		},
	})

	_, err := bus.PublishBatch([]BatchMessage{{Topic: "missing.subscriber", Payload: "payload"}})
	require.Error(t, err)
	var batchErr BatchError
	require.ErrorAs(t, err, &batchErr)
	require.NotNil(t, batchErr.Result)
	assert.ErrorIs(t, batchErr.Result.FirstError, ErrNoSubscriber)
	assert.Equal(t, int32(1), beforeCalled.Load())
	assert.Equal(t, int32(1), afterCalled.Load())
}

func TestPublishSyncAnyReturnsAfterTimeoutWhenSlowHandlerIgnoresCancellation(t *testing.T) {
	bus := New()
	defer bus.Close()
	bus.SetTimeout(20 * time.Millisecond)

	slowDone := make(chan struct{})
	require.NoError(t, bus.SubscribeWithResponse("any.timeout.success", func(topic string, payload any) (any, error) {
		return "fast", nil
	}))
	require.NoError(t, bus.SubscribeWithResponseContext("any.timeout.success", func(ctx context.Context, topic string, payload any) (any, error) {
		time.Sleep(80 * time.Millisecond)
		close(slowDone)
		return "slow", nil
	}))

	start := time.Now()
	result, err := bus.PublishSyncAny("any.timeout.success", "payload")
	elapsed := time.Since(start)

	require.NoError(t, err)
	require.NotNil(t, result)
	assert.True(t, result.Success)
	assert.Equal(t, 1, result.SuccessCount)
	assert.Equal(t, 1, result.FailureCount)
	assert.Less(t, elapsed, 70*time.Millisecond)

	select {
	case <-slowDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("slow handler did not finish")
	}
}

func TestPipePublishWithContextNilContext(t *testing.T) {
	pipe := NewPipe[int]()
	defer pipe.Close()

	var got atomic.Int32
	require.NoError(t, pipe.Subscribe(func(payload int) {
		got.Store(int32(payload))
	}))

	require.NoError(t, pipe.PublishWithContext(nil, 7))
	require.Eventually(t, func() bool {
		return got.Load() == 7
	}, time.Second, time.Millisecond)
}

func TestPipePublishSyncAnyReturnsAfterTimeoutWhenSlowHandlerIgnoresCancellation(t *testing.T) {
	pipe := NewPipeWithTimeout[int](20 * time.Millisecond)
	defer pipe.Close()

	slowDone := make(chan struct{})
	_, err := pipe.SubscribeWithResponse(func(payload int) (any, error) {
		return payload + 1, nil
	})
	require.NoError(t, err)
	require.NoError(t, pipe.SubscribeWithResponseContext(func(ctx context.Context, payload int) (any, error) {
		time.Sleep(80 * time.Millisecond)
		close(slowDone)
		return nil, errors.New("slow")
	}))

	start := time.Now()
	value, err := pipe.PublishSyncAnyWithContext(context.Background(), 10)
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, 11, value)
	assert.Less(t, elapsed, 70*time.Millisecond)

	select {
	case <-slowDone:
	case <-time.After(200 * time.Millisecond):
		t.Fatal("slow handler did not finish")
	}
}

func TestChannelCloseIfEmptyKeepsNonEmptyChannelOpen(t *testing.T) {
	bus := New()
	defer bus.Close()

	ch := newChannel("close.if.nonempty", 1, bus)
	defer ch.close()
	require.NoError(t, ch.subscribe(func(topic string, payload any) {}))

	assert.False(t, ch.closeIfEmpty())
	assert.False(t, ch.closed.Load())
}
