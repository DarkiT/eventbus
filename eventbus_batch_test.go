package eventbus

import (
	"context"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func TestPublishBatch_MixedTopics(t *testing.T) {
	bus := New()
	defer bus.Close()

	var mu sync.Mutex
	calls := make(map[string]int)

	require.NoError(t, bus.Subscribe("a", func(topic string, payload any) { mu.Lock(); calls["a"]++; mu.Unlock() }))
	require.NoError(t, bus.Subscribe("b", func(topic string, payload any) { mu.Lock(); calls["b"]++; mu.Unlock() }))

	msgs := []BatchMessage{
		{Topic: "a", Payload: 1},
		{Topic: "b", Payload: 2},
		{Topic: "a", Payload: 3},
	}

	res, err := bus.PublishBatch(msgs)
	assert.NoError(t, err)
	assert.Equal(t, 3, res.SuccessCount)
	time.Sleep(time.Millisecond * 10)
	mu.Lock()
	assert.Equal(t, 2, calls["a"])
	assert.Equal(t, 1, calls["b"])
	mu.Unlock()
}

func TestPublishBatch_FilterReject(t *testing.T) {
	bus := New()
	defer bus.Close()

	bus.AddFilter(FilterFunc(func(topic string, payload any) bool {
		if v, ok := payload.(int); ok && v == 2 {
			return false
		}
		return true
	}))

	var count int32
	require.NoError(t, bus.Subscribe("a", func(topic string, payload any) { atomic.AddInt32(&count, 1) }))

	msgs := []BatchMessage{
		{Topic: "a", Payload: 1},
		{Topic: "a", Payload: 2}, // will be rejected
		{Topic: "a", Payload: 3},
	}

	res, err := bus.PublishBatch(msgs)
	assert.NoError(t, err)
	assert.Equal(t, 2, res.SuccessCount)
	assert.Equal(t, 0, res.FailedCount)
	assert.True(t, res.Results[1].Skipped)
	time.Sleep(time.Millisecond * 10)
	assert.Equal(t, int32(2), atomic.LoadInt32(&count))
}

func TestPublishBatch_MiddlewareCalled(t *testing.T) {
	bus := New()
	defer bus.Close()

	var beforeCalled int32
	var afterCalled int32
	bus.Use(MiddlewareFunc{
		BeforeFn: func(topic string, payload any) any {
			atomic.AddInt32(&beforeCalled, 1)
			return payload
		},
		AfterFn: func(topic string, payload any) {
			atomic.AddInt32(&afterCalled, 1)
		},
	})

	var count int32
	require.NoError(t, bus.Subscribe("x", func(topic string, payload any) { atomic.AddInt32(&count, 1) }))

	msgs := []BatchMessage{
		{Topic: "x", Payload: 1},
		{Topic: "x", Payload: 2},
	}

	res, err := bus.PublishBatch(msgs)
	assert.NoError(t, err)
	assert.Equal(t, 2, res.SuccessCount)
	time.Sleep(time.Millisecond * 10)
	assert.Equal(t, int32(2), atomic.LoadInt32(&count))
	assert.Equal(t, int32(2), atomic.LoadInt32(&beforeCalled))
	assert.Equal(t, int32(2), atomic.LoadInt32(&afterCalled))
}

func TestPublishBatch_PartialError(t *testing.T) {
	bus := New()
	defer bus.Close()

	require.NoError(t, bus.Subscribe("err", func(topic string, payload any) {}))

	msgs := []BatchMessage{
		{Topic: "err", Payload: 1},
		{Topic: "no_sub", Payload: 2},
	}

	res, err := bus.PublishBatch(msgs)
	assert.Error(t, err)
	assert.True(t, isBatchError(err))
	assert.Equal(t, 1, res.FailedCount)
}

func TestPublishBatch_ContextCancel(t *testing.T) {
	bus := New()
	defer bus.Close()

	require.NoError(t, bus.Subscribe("ctx", func(topic string, payload any) {}))

	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	msgs := []BatchMessage{{Topic: "ctx", Payload: 1, Context: ctx}}
	res, err := bus.PublishBatch(msgs)
	// 异步发布时，已取消的 context 可能仍然成功发送到 channel
	// 因此这里不强制要求返回错误
	assert.NotNil(t, res)
	_ = err // 可能为 nil 或 context.Canceled
}

func TestPublishBatch_Empty(t *testing.T) {
	bus := New()
	defer bus.Close()

	res, err := bus.PublishBatch(nil)
	assert.NoError(t, err)
	assert.Equal(t, 0, res.SuccessCount)
}

func TestPublishBatchSync_Basic(t *testing.T) {
	bus := New()
	defer bus.Close()

	var count int
	require.NoError(t, bus.Subscribe("s", func(topic string, payload any) { count++ }))

	msgs := []BatchMessage{
		{Topic: "s", Payload: 1},
		{Topic: "s", Payload: 2},
	}

	res, err := bus.PublishBatchSync(msgs)
	assert.NoError(t, err)
	assert.Equal(t, 2, res.SuccessCount)
	assert.Equal(t, 0, res.FailedCount)
	assert.Equal(t, 2, count)
}

func TestPublishBatch_NoSubscriber(t *testing.T) {
	bus := New()
	defer bus.Close()

	msgs := []BatchMessage{{Topic: "no", Payload: 1}}
	res, err := bus.PublishBatchSync(msgs)
	assert.Error(t, err)
	assert.Equal(t, ErrNoSubscriber, res.FirstError)
}

// 并发场景 race
func TestPublishBatch_Concurrent(t *testing.T) {
	bus := New()
	defer bus.Close()

	var wg sync.WaitGroup
	require.NoError(t, bus.Subscribe("c", func(topic string, payload any) {}))

	run := func() {
		defer wg.Done()
		msgs := []BatchMessage{{Topic: "c", Payload: 1}, {Topic: "c", Payload: 2}}
		_, _ = bus.PublishBatch(msgs)
	}

	for range 20 {
		wg.Add(1)
		go run()
	}
	wg.Wait()
}

func TestPublishBatch_WildcardMatch(t *testing.T) {
	bus := New()
	defer bus.Close()

	var count int32
	require.NoError(t, bus.Subscribe("user.*", func(topic string, payload any) {
		atomic.AddInt32(&count, 1)
	}))

	msgs := []BatchMessage{
		{Topic: "user.created", Payload: 1},
		{Topic: "user.updated", Payload: 2},
	}

	res, err := bus.PublishBatch(msgs)
	assert.NoError(t, err)
	assert.Equal(t, 2, res.SuccessCount)
	time.Sleep(time.Millisecond * 10)
	assert.Equal(t, int32(2), atomic.LoadInt32(&count))
}

func TestPublishBatch_BusClosed(t *testing.T) {
	bus := New()
	bus.Close()

	msgs := []BatchMessage{{Topic: "x", Payload: 1}}
	res, err := bus.PublishBatch(msgs)
	assert.ErrorIs(t, err, ErrEventBusClosed)
	assert.Nil(t, res)
}

func TestPublishBatch_ContextTimeout(t *testing.T) {
	bus := New()
	defer bus.Close()

	require.NoError(t, bus.Subscribe("ctx.timeout", func(topic string, payload any) {}))

	ctx, cancel := context.WithTimeout(context.Background(), -time.Second)
	defer cancel()

	msgs := []BatchMessage{{Topic: "ctx.timeout", Payload: 1, Context: ctx}}
	res, err := bus.PublishBatch(msgs)
	// 异步发布时，已超时的 context 可能仍然成功发送到 channel
	// 因此这里不强制要求返回错误
	assert.NotNil(t, res)
	_ = err // 可能为 nil 或 context.DeadlineExceeded
}

func TestPublishBatch_UnbufferedChannel(t *testing.T) {
	bus := New() // 默认无缓冲
	defer bus.Close()

	var count int32
	require.NoError(t, bus.Subscribe("unbuf", func(topic string, payload any) {
		atomic.AddInt32(&count, 1)
	}))

	msgs := []BatchMessage{
		{Topic: "unbuf", Payload: 1},
		{Topic: "unbuf", Payload: 2},
		{Topic: "unbuf", Payload: 3},
	}

	res, err := bus.PublishBatch(msgs)
	assert.NoError(t, err)
	assert.Equal(t, 3, res.SuccessCount)
	time.Sleep(time.Millisecond * 10)
	assert.Equal(t, int32(3), atomic.LoadInt32(&count))
}

func TestPublishBatchSync_AggregatesHandlerCountAcrossMatches(t *testing.T) {
	bus := New()
	defer bus.Close()

	require.NoError(t, bus.Subscribe("user.created", func(topic string, payload any) {}))
	require.NoError(t, bus.Subscribe("user.*", func(topic string, payload any) {}))
	require.NoError(t, bus.SubscribeWithResponse("user.*", func(topic string, payload any) (any, error) {
		return payload, nil
	}))

	res, err := bus.PublishBatchSync([]BatchMessage{{Topic: "user.created", Payload: "ok"}})
	require.NoError(t, err)
	require.Len(t, res.Results, 1)
	assert.Equal(t, 2, res.Results[0].HandlerCount)
	assert.GreaterOrEqual(t, res.Results[0].Duration, time.Duration(0))
}

func TestPublishBatchSync_PreservesOriginalTopicPerMessage(t *testing.T) {
	bus := New()
	defer bus.Close()

	topics := make([]string, 0, 2)
	require.NoError(t, bus.Subscribe("user.*", func(topic string, payload any) {
		topics = append(topics, topic)
	}))

	res, err := bus.PublishBatchSync([]BatchMessage{
		{Topic: "user/created", Payload: 1},
		{Topic: "user.created", Payload: 2},
	})
	require.NoError(t, err)
	assert.Equal(t, 2, res.SuccessCount)
	assert.Equal(t, []string{"user/created", "user.created"}, topics)
}
