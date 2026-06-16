package eventbus

import (
	"context"
	"errors"
	"runtime"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"
)

func busHandlerOne(topic string, val int) {
}

func busHandlerTwo(topic string, val int) {
}

func Test_newChannel(t *testing.T) {
	bus := New()
	ch := newChannel("test_topic", -1, bus)
	assert.NotNil(t, ch)
	assert.NotNil(t, ch.channel)
	assert.Equal(t, "test_topic", ch.topic)
	assert.NotNil(t, ch.stopCh)
	assert.NotNil(t, ch.handlers)
	ch.close()

	bufferedCh := newChannel("test_topic", 100, bus)
	assert.NotNil(t, bufferedCh)
	assert.NotNil(t, bufferedCh.channel)
	assert.Equal(t, 100, cap(bufferedCh.channel))
	assert.Equal(t, "test_topic", bufferedCh.topic)
	assert.NotNil(t, bufferedCh.stopCh)
	assert.NotNil(t, bufferedCh.handlers)
	bufferedCh.close()

	bufferedZeroCh := newChannel("test_topic", 0, bus)
	assert.NotNil(t, bufferedZeroCh)
	assert.NotNil(t, bufferedZeroCh.channel)
	assert.Equal(t, "test_topic", bufferedZeroCh.topic)
	assert.NotNil(t, bufferedZeroCh.stopCh)
	assert.NotNil(t, bufferedZeroCh.handlers)
	bufferedZeroCh.close()
}

func Test_channelSubscribe(t *testing.T) {
	bus := New()
	ch := newChannel("test_topic", -1, bus)
	assert.NotNil(t, ch)
	assert.NotNil(t, ch.channel)
	assert.Equal(t, "test_topic", ch.topic)

	err := ch.subscribe(busHandlerOne)
	assert.Nil(t, err)
	ch.close()
	err = ch.subscribe(busHandlerTwo)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrChannelClosed))
}

func Test_channelUnsubscribe(t *testing.T) {
	bus := New()
	ch := newChannel("test_topic", -1, bus)
	assert.NotNil(t, ch)
	assert.NotNil(t, ch.channel)
	assert.Equal(t, "test_topic", ch.topic)

	err := ch.subscribe(busHandlerOne)
	assert.Nil(t, err)
	err = ch.unsubscribe(busHandlerOne)
	assert.Nil(t, err)

	err = ch.subscribe(busHandlerOne)
	assert.Nil(t, err)
	ch.close()
	err = ch.unsubscribe(busHandlerTwo)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrChannelClosed))
}

func Test_channelClose(t *testing.T) {
	bus := New()
	ch := newChannel("test_topic", -1, bus)
	assert.NotNil(t, ch)
	assert.NotNil(t, ch.channel)
	assert.Equal(t, "test_topic", ch.topic)

	err := ch.subscribe(busHandlerOne)
	assert.Nil(t, err)
	ch.close()
	assert.Equal(t, 0, len(ch.handlers))
	ch.close()
}

func Test_channelPublish(t *testing.T) {
	bus := New()
	ch := newChannel("test_topic", -1, bus)
	assert.NotNil(t, ch)
	assert.NotNil(t, ch.channel)
	assert.Equal(t, "test_topic", ch.topic)
	require.NoError(t, ch.subscribe(busHandlerOne))

	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 100 {
			err := ch.publishAsync(context.Background(), "test_topic", i)
			assert.Nil(t, err)
		}
	})
	wg.Wait()

	// 等待消息处理完成
	time.Sleep(time.Millisecond)
	ch.close()
	err := ch.publishAsync(context.Background(), "test_topic", 1)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrChannelClosed))
}

func Test_channelPublishSync(t *testing.T) {
	bus := New()
	ch := newChannel("test_topic", -1, bus)
	assert.NotNil(t, ch)
	assert.NotNil(t, ch.channel)
	assert.Equal(t, "test_topic", ch.topic)
	require.NoError(t, ch.subscribe(busHandlerOne))

	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 100 {
			err := ch.publishSync(context.Background(), "test_topic", i)
			assert.Nil(t, err)
		}
	})
	wg.Wait()

	err := ch.publishSync(context.Background(), "test_topic", nil)
	assert.Nil(t, err)
	time.Sleep(time.Millisecond)
	ch.close()
	err = ch.publishSync(context.Background(), "test_topic", 1)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrChannelClosed))
}

func Test_New(t *testing.T) {
	bus := New()
	assert.NotNil(t, bus)
	assert.Equal(t, -1, bus.bufferSize)
	assert.NotNil(t, bus.channels)
	bus.Close()
}

func Test_NewWithBuffer(t *testing.T) {
	// 测试带缓冲的事件总线
	bus := New(100)
	assert.NotNil(t, bus)
	assert.Equal(t, 100, bus.bufferSize)
	assert.NotNil(t, bus.channels)
	bus.Close()

	// 测试传入0表示无缓冲
	busZero := New(0)
	assert.NotNil(t, busZero)
	assert.Equal(t, -1, busZero.bufferSize)
	assert.NotNil(t, busZero.channels)
	busZero.Close()

	// 测试传入负数使用默认缓冲大小
	busNegative := New(-1)
	assert.NotNil(t, busNegative)
	assert.Equal(t, defaultBufferSize, busNegative.bufferSize)
	assert.NotNil(t, busNegative.channels)
	busNegative.Close()

	// 测试不传参数默认无缓冲
	busDefault := New()
	assert.NotNil(t, busDefault)
	assert.Equal(t, -1, busDefault.bufferSize)
	assert.NotNil(t, busDefault.channels)
	busDefault.Close()
}

func Test_EventBusSubscribe(t *testing.T) {
	bus := New()
	assert.NotNil(t, bus)
	assert.Equal(t, -1, bus.bufferSize)
	assert.NotNil(t, bus.channels)

	err := bus.Subscribe("testtopic", busHandlerOne)
	assert.Nil(t, err)
	err = bus.Subscribe("testtopic", 1)
	assert.Equal(t, ErrHandlerIsNotFunc, err)

	err = bus.Subscribe("testtopic", func(topic string) error {
		return nil
	})
	assert.Equal(t, ErrHandlerParamNum, err)

	err = bus.Subscribe("testtopic", func(topic int, payload int) error {
		return nil
	})
	assert.Equal(t, ErrHandlerFirstParam, err)
	bus.Close()

	err = bus.Unsubscribe("testtopic", busHandlerTwo)
	assert.Equal(t, ErrEventBusClosed, err)
}

func TestSubscribeHandlerWithContext(t *testing.T) {
	bus := New()
	defer bus.Close()

	received := make(chan context.Context, 1)

	err := bus.Subscribe("ctx.topic", func(ctx context.Context, topic string, payload any) {
		received <- ctx
		<-ctx.Done()
	})
	require.NoError(t, err)

	ctx, cancel := context.WithCancel(context.Background())
	go func() {
		time.Sleep(20 * time.Millisecond)
		cancel()
	}()

	err = bus.PublishWithContext(ctx, "ctx.topic", "payload")
	require.NoError(t, err)

	select {
	case receivedCtx := <-received:
		assert.Equal(t, ctx, receivedCtx)
	case <-time.After(time.Second):
		t.Fatal("未接收到上下文")
	}

	ctxSync, cancelSync := context.WithCancel(context.Background())
	go func() {
		<-time.After(20 * time.Millisecond)
		cancelSync()
	}()

	err = bus.PublishSyncWithContext(ctxSync, "ctx.topic", "payload")
	require.Error(t, err)
	assert.ErrorIs(t, err, context.Canceled)
}

func TestPublishSyncAnyCancelsSlowerHandlers(t *testing.T) {
	bus := New()
	defer bus.Close()

	slowCanceled := make(chan struct{}, 1)

	require.NoError(t, bus.SubscribeWithResponseContext("resp.topic", func(ctx context.Context, topic string, payload any) (any, error) {
		<-ctx.Done()
		slowCanceled <- struct{}{}
		return nil, ctx.Err()
	}))
	require.NoError(t, bus.SubscribeWithResponseContext("resp.topic", func(ctx context.Context, topic string, payload any) (any, error) {
		return "ok", nil
	}))

	result, err := bus.PublishSyncAny("resp.topic", "payload")
	assert.NoError(t, err)
	assert.True(t, result.Success)
	assert.Equal(t, 2, result.HandlerCount)
	assert.GreaterOrEqual(t, result.SuccessCount, 1)

	select {
	case <-slowCanceled:
	case <-time.After(time.Second):
		t.Fatal("慢处理器未被取消")
	}
}

func TestPublishSyncAnyWithContextPropagatesValues(t *testing.T) {
	bus := New()
	defer bus.Close()

	type ctxKey string
	const traceKey ctxKey = "trace-id"

	received := make(chan string, 1)

	require.NoError(t, bus.SubscribeWithResponseContext("resp.ctx", func(ctx context.Context, topic string, payload any) (any, error) {
		val, _ := ctx.Value(traceKey).(string)
		received <- val
		return payload, nil
	}))

	ctx := context.WithValue(context.Background(), traceKey, "TRACE-001")
	result, err := bus.PublishSyncAnyWithContext(ctx, "resp.ctx", "payload")
	require.NoError(t, err)
	require.True(t, result.Success)

	select {
	case got := <-received:
		assert.Equal(t, "TRACE-001", got)
	case <-time.After(time.Second):
		t.Fatal("未接收到上下文中的 trace 信息")
	}
}

func TestTopicGroupAdvancedMethods(t *testing.T) {
	bus := New()
	defer bus.Close()

	group := bus.NewGroup("order")

	var order []int
	var mu sync.Mutex
	require.NoError(t, group.SubscribeWithPriority("created", func(topic string, payload any) {
		mu.Lock()
		order = append(order, 1)
		mu.Unlock()
	}, 5))
	require.NoError(t, group.SubscribeWithPriority("created", func(topic string, payload any) {
		mu.Lock()
		order = append(order, 2)
		mu.Unlock()
	}, 1))

	type ctxKey string
	const userKey ctxKey = "user"

	received := make(chan string, 1)
	require.NoError(t, group.SubscribeWithResponseContext("created", func(ctx context.Context, topic string, payload any) (any, error) {
		val, _ := ctx.Value(userKey).(string)
		received <- val
		return payload, nil
	}))

	ctx := context.WithValue(context.Background(), userKey, "tester")
	result, err := group.PublishSyncAnyWithContext(ctx, "created", "ok")
	require.NoError(t, err)
	require.True(t, result.Success)

	timeout := time.NewTimer(time.Second)
	select {
	case got := <-received:
		assert.Equal(t, "tester", got)
	case <-timeout.C:
		t.Fatal("未接收到上下文中的 user 信息")
	}

	require.NoError(t, group.PublishSync("created", "payload"))

	// 验证优先级顺序
	mu.Lock()
	defer mu.Unlock()
	assert.Len(t, order, 2)
	assert.Equal(t, []int{1, 2}, order)

	require.NoError(t, group.UnsubscribeAll("created"))
}

func TestSingletonAdvancedWrappers(t *testing.T) {
	ResetSingleton()
	defer Close()

	received := make(chan struct{}, 1)
	require.NoError(t, SubscribeWithResponse("singleton.topic", func(topic string, payload any) (any, error) {
		received <- struct{}{}
		return payload, nil
	}))

	result, err := PublishSyncAny("singleton.topic", "ok")
	require.NoError(t, err)
	require.True(t, result.Success)

	select {
	case <-received:
		// ok
	case <-time.After(time.Second):
		t.Fatal("单例响应未触发")
	}

	stats := GetStats()
	var channelCount int
	switch v := stats["channel_count"].(type) {
	case int:
		channelCount = v
	case int32:
		channelCount = int(v)
	case int64:
		channelCount = int(v)
	case uint32:
		channelCount = int(v)
	case uint64:
		channelCount = int(v)
	default:
		t.Fatalf("未知的 channel_count 类型: %T", v)
	}
	assert.GreaterOrEqual(t, channelCount, 1)

	group := NewGroup("singleton")
	require.NotNil(t, group)
	handler := func(topic string, payload any) {}
	require.NoError(t, group.Subscribe("sub", handler))
	require.NoError(t, group.Unsubscribe("sub", handler))
}

func TestSmartFilter(t *testing.T) {
	filter := NewSmartFilter()
	assert.True(t, filter.Filter("user.login", nil))
	filter.BlockTopic("user.test")
	assert.False(t, filter.Filter("user.test", nil))
	assert.False(t, filter.Filter("user.test.detail", nil))

	filter.SetLimit("user.login", 1)
	assert.True(t, filter.Filter("user.login", nil))
	assert.False(t, filter.Filter("user.login", nil))

	filter.UnblockTopic("user.test")
	assert.True(t, filter.Filter("user.test", nil))
}

func TestMiddleware(t *testing.T) {
	mw := NewMiddleware()
	mw.SetTransformer(func(topic string, payload any) any {
		if s, ok := payload.(string); ok {
			return strings.ToUpper(s)
		}
		return payload
	})

	converted := mw.Before("order.created", "ok")
	assert.Equal(t, "OK", converted)

	// 模拟处理耗时
	time.Sleep(time.Millisecond)
	mw.After("order.created", converted)

	stats := mw.GetStats()
	stat, ok := stats["order.created"]
	assert.True(t, ok)
	assert.Equal(t, 1, stat.Count)
	assert.Greater(t, stat.TotalTime, time.Duration(0))

	mw.Reset()
	assert.Empty(t, mw.GetStats())
}

func TestUnsubscribeAllClearsResponseHandlers(t *testing.T) {
	bus := New()
	defer bus.Close()

	require.NoError(t, bus.SubscribeWithResponse("clean.topic", func(topic string, payload any) (any, error) {
		return payload, nil
	}))

	require.NoError(t, bus.UnsubscribeAll("clean.topic"))

	normalized, _ := normalizeTopic("clean.topic")
	ch, ok := bus.channels.Load(normalized)
	assert.False(t, ok)
	assert.Nil(t, ch)
}

func TestUnsubscribeResponseHandler(t *testing.T) {
	bus := New()
	defer bus.Close()

	responseHandler := func(topic string, payload any) (any, error) {
		return payload, nil
	}

	require.NoError(t, bus.SubscribeWithResponse("resp.cleanup", responseHandler))
	require.NoError(t, bus.Unsubscribe("resp.cleanup", responseHandler))

	respNorm, _ := normalizeTopic("resp.cleanup")
	ch, ok := bus.channels.Load(respNorm)
	assert.False(t, ok)
	assert.Nil(t, ch)
}

func Test_EventBusUnsubscribe(t *testing.T) {
	bus := New()
	assert.NotNil(t, bus)
	assert.Equal(t, -1, bus.bufferSize)
	assert.NotNil(t, bus.channels)

	err := bus.Unsubscribe("testtopic", busHandlerOne)
	assert.Equal(t, ErrNoSubscriber, err)

	err = bus.Subscribe("testtopic", busHandlerOne)
	assert.Nil(t, err)

	err = bus.Unsubscribe("testtopic", busHandlerOne)
	assert.Nil(t, err)
	assert.Equal(t, 0, bus.channels.Len())
	bus.Close()

	err = bus.Unsubscribe("testtopic", busHandlerTwo)
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrEventBusClosed))
}

func Test_EventBusUnsubscribe_PrunesChannelAndAllowsResubscribe(t *testing.T) {
	bus := New()
	defer bus.Close()

	var count atomic.Int32
	handler := func(topic string, payload any) {
		count.Add(1)
	}

	require.NoError(t, bus.Subscribe("re.topic", handler))
	require.NoError(t, bus.Unsubscribe("re.topic", handler))
	assert.Equal(t, 0, bus.channels.Len())

	require.NoError(t, bus.Subscribe("re.topic", handler))
	require.NoError(t, bus.PublishSync("re.topic", "payload"))
	assert.Equal(t, int32(1), count.Load())
}

func Test_EventBusPublish(t *testing.T) {
	bus := New()
	assert.NotNil(t, bus)
	assert.Equal(t, -1, bus.bufferSize)
	assert.NotNil(t, bus.channels)

	err := bus.Publish("testtopic", 1)
	assert.Nil(t, err)

	err = bus.Subscribe("testtopic", busHandlerOne)
	assert.Nil(t, err)

	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 100 {
			err := bus.Publish("testtopic", i)
			assert.Nil(t, err)
		}
	})
	wg.Wait()
	bus.Close()
}

func Test_SubscribeConcurrentSingleChannel_NoDuplicateCreation(t *testing.T) {
	bus := New()
	defer bus.Close()

	topic := "dup.topic"
	startG := runtime.NumGoroutine()

	handler := func(topic string, payload any) {}

	const concurrency = 100
	var wg sync.WaitGroup
	wg.Add(concurrency)
	for range concurrency {
		go func() {
			defer wg.Done()
			require.NoError(t, bus.SubscribeWithPriority(topic, handler, 0))
		}()
	}
	wg.Wait()

	// 其他订阅入口也应复用同一通道
	require.NoError(t, bus.SubscribeWithResponse(topic, func(topic string, payload any) (any, error) {
		return payload, nil
	}))
	require.NoError(t, bus.SubscribeWithResponseContext(topic, func(ctx context.Context, topic string, payload any) (any, error) {
		return payload, nil
	}))

	normalized, _ := normalizeTopic(topic)
	ch, ok := bus.channels.Load(normalized)
	require.True(t, ok, "通道应已创建")
	assert.Equal(t, 1, bus.channels.Len(), "并发订阅应只创建一个通道")

	// 功能向后兼容：发布后处理器被调用
	var delivered int32
	require.NoError(t, bus.Subscribe(topic, func(t string, payload any) {
		atomic.AddInt32(&delivered, 1)
	}))
	require.NoError(t, bus.PublishSync(topic, "msg"))
	time.Sleep(10 * time.Millisecond)
	assert.Equal(t, int32(1), atomic.LoadInt32(&delivered))

	// 关闭后 goroutine 不应明显泄漏
	bus.Close()
	time.Sleep(50 * time.Millisecond)
	endG := runtime.NumGoroutine()
	assert.LessOrEqual(t, endG, startG+5, "不应出现明显 goroutine 泄漏")

	// 防止编译器优化未使用变量
	_ = ch
	_ = ok
}

func Test_EventBusPublishSync(t *testing.T) {
	bus := New()
	assert.NotNil(t, bus)
	assert.Equal(t, -1, bus.bufferSize)
	assert.NotNil(t, bus.channels)

	err := bus.PublishSync("testtopic", 1)
	assert.Nil(t, err)

	err = bus.Subscribe("testtopic", busHandlerOne)
	assert.Nil(t, err)

	var wg sync.WaitGroup
	wg.Go(func() {
		for i := range 100 {
			err := bus.PublishSync("testtopic", i)
			assert.Nil(t, err)
		}
	})
	wg.Wait()
	bus.Close()
}

func Test_EventBusPrioritySubscription(t *testing.T) {
	bus := New()
	defer bus.Close()

	var order []int
	var mu sync.Mutex

	// 添加不同优先级的处理器
	require.NoError(t, bus.SubscribeWithPriority("test", func(topic string, payload any) {
		mu.Lock()
		order = append(order, 1)
		mu.Unlock()
	}, 1))

	require.NoError(t, bus.SubscribeWithPriority("test", func(topic string, payload any) {
		mu.Lock()
		order = append(order, 10)
		mu.Unlock()
	}, 10))

	require.NoError(t, bus.SubscribeWithPriority("test", func(topic string, payload any) {
		mu.Lock()
		order = append(order, 5)
		mu.Unlock()
	}, 5))

	// 同步发布以确保顺序
	err := bus.PublishSync("test", "message")
	require.NoError(t, err)

	// 验证执行顺序（高优先级先执行）
	mu.Lock()
	expected := []int{10, 5, 1}
	assert.Equal(t, expected, order)
	mu.Unlock()
}

func Test_EventBusGlobalPriorityOrderingWithWildcard(t *testing.T) {
	bus := New()
	defer bus.Close()

	var order []string
	var mu sync.Mutex

	require.NoError(t, bus.SubscribeWithPriority("a/b", func(topic string, payload any) {
		mu.Lock()
		order = append(order, "specific")
		mu.Unlock()
	}, 1))

	require.NoError(t, bus.SubscribeWithPriority("a/#", func(topic string, payload any) {
		mu.Lock()
		order = append(order, "wildcard")
		mu.Unlock()
	}, 10))

	require.NoError(t, bus.PublishSync("a/b", "payload"))

	mu.Lock()
	assert.Equal(t, []string{"wildcard", "specific"}, order)
	mu.Unlock()
}

func Test_EventBusPublishSync_DoesNotDedupDifferentSubscriptions(t *testing.T) {
	bus := New()
	defer bus.Close()

	var count atomic.Int32
	handler := func(topic string, payload any) {
		count.Add(1)
	}

	require.NoError(t, bus.Subscribe("dup.topic", handler))
	require.NoError(t, bus.Subscribe("dup.#", handler))

	require.NoError(t, bus.PublishSync("dup.topic", "payload"))
	assert.Equal(t, int32(2), count.Load())
}

func Test_EventBusPublishSync_KeepsDuplicateSubscriptionsConsistent(t *testing.T) {
	bus := New()
	defer bus.Close()

	var count atomic.Int32
	handler := func(topic string, payload any) {
		count.Add(1)
	}

	require.NoError(t, bus.Subscribe("dup.same", handler))
	require.NoError(t, bus.Subscribe("dup.same", handler))

	require.NoError(t, bus.PublishSync("dup.same", "payload"))
	assert.Equal(t, int32(2), count.Load())
}

func Test_EventBusRejectsInvalidWildcardPattern(t *testing.T) {
	bus := New()
	defer bus.Close()

	handler := func(topic string, payload any) {}

	require.ErrorIs(t, bus.Subscribe("invalid.#.topic", handler), ErrInvalidTopic)
	require.ErrorIs(t, bus.Subscribe("invalid.segment*", handler), ErrInvalidTopic)
	require.ErrorIs(t, bus.PublishSync("invalid.segment*", "payload"), ErrInvalidTopic)
}

func Test_EventBusSubscribeOnce_Concurrent(t *testing.T) {
	bus := New()
	defer bus.Close()

	var count atomic.Int32
	require.NoError(t, bus.SubscribeOnce("once.topic", func(topic string, payload any) {
		count.Add(1)
	}))

	var wg sync.WaitGroup
	const concurrency = 200
	wg.Add(concurrency)
	for range concurrency {
		go func() {
			defer wg.Done()
			_ = bus.Publish("once.topic", "payload")
		}()
	}
	wg.Wait()
	time.Sleep(50 * time.Millisecond)

	assert.Equal(t, int32(1), count.Load())

	// 再次发布也不应触发
	_ = bus.Publish("once.topic", "again")
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, int32(1), count.Load())
}

func Test_EventBusSubscribeOnce_WithContext(t *testing.T) {
	bus := New()
	defer bus.Close()

	var count atomic.Int32
	require.NoError(t, bus.SubscribeOnce("ctx.once", func(ctx context.Context, topic string, payload any) {
		if ctx != nil {
			count.Add(1)
		}
	}))

	_ = bus.PublishWithContext(context.Background(), "ctx.once", "payload")
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, int32(1), count.Load())

	// 再次发布确认不会重复
	_ = bus.Publish("ctx.once", "again")
	time.Sleep(20 * time.Millisecond)
	assert.Equal(t, int32(1), count.Load())
}

func Test_EventBusSubscribeOnce_PanicSafeUnsubscribe(t *testing.T) {
	bus := New()
	defer bus.Close()

	require.NoError(t, bus.SubscribeOnce("panic.once", func(topic string, payload any) {
		panic("boom")
	}))

	require.NoError(t, bus.PublishSync("panic.once", "payload"))

	// 再次发布不应再触发 panic，且返回应为成功
	require.NoError(t, bus.PublishSync("panic.once", "second"))
}

func Test_EventBusSubscribeOnce_Wildcard(t *testing.T) {
	bus := New()
	defer bus.Close()

	var count atomic.Int32
	require.NoError(t, bus.SubscribeOnce("a/#", func(topic string, payload any) {
		count.Add(1)
	}))

	require.NoError(t, bus.PublishSync("a/b/c", "payload"))
	require.NoError(t, bus.PublishSync("a/x", "again"))
	assert.Equal(t, int32(1), count.Load())
}

func Test_EventBusContextPublish(t *testing.T) {
	bus := New()
	defer bus.Close()

	received := make(chan bool, 1)
	require.NoError(t, bus.Subscribe("test", func(topic string, payload any) {
		time.Sleep(2 * time.Second) // 模拟慢处理
		received <- true
	}))

	// 异步发布不会因为慢处理阻塞
	ctxAsync, cancelAsync := context.WithTimeout(context.Background(), time.Second)
	defer cancelAsync()

	err := bus.PublishWithContext(ctxAsync, "test", "message")
	assert.NoError(t, err)

	select {
	case <-received:
	case <-time.After(3 * time.Second):
		t.Fatal("异步发布未触发处理器")
	}

	// 同步发布将受到上下文超时影响
	ctxSync, cancelSync := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancelSync()

	err = bus.PublishSyncWithContext(ctxSync, "test", "message")
	assert.Error(t, err)
	assert.Equal(t, context.DeadlineExceeded, err)
}

func Test_EventBusAsyncPublish(t *testing.T) {
	bus := New(10)
	defer bus.Close()

	var received int32
	require.NoError(t, bus.Subscribe("test", func(topic string, payload any) {
		atomic.AddInt32(&received, 1)
	}))

	// 异步发布多条消息
	for i := range 5 {
		err := bus.Publish("test", i)
		assert.NoError(t, err)
	}

	// 等待处理完成
	time.Sleep(100 * time.Millisecond)
	assert.Equal(t, int32(5), atomic.LoadInt32(&received))
}

func BenchmarkEventBusPublish(b *testing.B) {
	bus := New()
	if err := bus.Subscribe("testtopic", busHandlerOne); err != nil {
		b.Fatalf("订阅失败: %v", err)
	}

	b.ResetTimer()
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 0; i < b.N; i++ {
			if err := bus.Publish("testtopic", i); err != nil {
				panic(err)
			}
		}
	})
	wg.Wait()
	bus.Close()
}

func BenchmarkEventBusPublishSync(b *testing.B) {
	bus := New()
	if err := bus.Subscribe("testtopic", busHandlerOne); err != nil {
		b.Fatalf("订阅失败: %v", err)
	}

	b.ResetTimer()
	var wg sync.WaitGroup
	wg.Go(func() {
		for i := 0; i < b.N; i++ {
			if err := bus.PublishSync("testtopic", i); err != nil {
				panic(err)
			}
		}
	})
	wg.Wait()
	bus.Close()
}

func BenchmarkHighConcurrency(b *testing.B) {
	bus := New()
	topics := []string{"topic1", "topic2", "topic3"}

	for _, topic := range topics {
		if err := bus.Subscribe(topic, func(t string, p any) {}); err != nil {
			panic(err)
		}
	}

	b.RunParallel(func(pb *testing.PB) {
		i := 0
		for pb.Next() {
			topic := topics[i%len(topics)]
			i++
			if err := bus.Publish(topic, "test"); err != nil {
				panic(err)
			}
		}
	})
}

func TestEventBus_Filters(t *testing.T) {
	bus := New()
	filtered := false

	// 创建过滤器
	filter := &testFilter{
		filterFunc: func(topic string, payload any) bool {
			filtered = true
			return false // 返回 false 表示消息被过滤掉
		},
	}

	bus.AddFilter(filter)
	err := bus.Publish("test", 1)
	assert.NoError(t, err)
	assert.True(t, filtered)
}

func TestEventBus_Middleware(t *testing.T) {
	bus := New()
	count := 0

	// 创建中间件
	middleware := &testMiddleware{
		beforeFunc: func(topic string, payload any) any {
			count++
			return payload
		},
		afterFunc: func(topic string, payload any) {
			count++
		},
	}

	bus.Use(middleware)
	require.NoError(t, bus.Subscribe("test", func(topic string, payload int) {}))
	require.NoError(t, bus.Publish("test", 1))

	time.Sleep(50 * time.Millisecond) // 等待异步处理
	assert.Equal(t, 2, count)
}

func TestEventBusFilterPanic(t *testing.T) {
	bus := New()
	defer bus.Close()

	bus.AddFilter(&testFilter{filterFunc: func(topic string, payload any) bool {
		panic("boom")
	}})

	err := bus.Publish("panic.topic", 1)
	require.Error(t, err)
	assert.Contains(t, err.Error(), "filter panic")
}

// 测试辅助类型
type testFilter struct {
	filterFunc func(topic string, payload any) bool
}

func (f *testFilter) Filter(topic string, payload any) bool {
	return f.filterFunc(topic, payload)
}

type testMiddleware struct {
	beforeFunc func(topic string, payload any) any
	afterFunc  func(topic string, payload any)
}

func (m *testMiddleware) Before(topic string, payload any) any {
	return m.beforeFunc(topic, payload)
}

func (m *testMiddleware) After(topic string, payload any) {
	m.afterFunc(topic, payload)
}

func Test_EventBusTracer(t *testing.T) {
	bus := New()

	// 创建一个测试追踪器
	tracer := &mockTracer{}
	bus.SetTracer(tracer)

	// 定义一个处理器变量以便重用
	handler := func(topic string, msg any) {}

	// 测试订阅
	err := bus.Subscribe("test", handler)
	assert.Nil(t, err)

	// 测试发布
	err = bus.Publish("test", "message")
	assert.Nil(t, err)

	// 测试取消订阅 - 使用相同的处理器变量
	err = bus.Unsubscribe("test", handler)
	assert.Nil(t, err)
}

func TestEventBusOnComplete(t *testing.T) {
	bus := New()
	defer bus.Close()

	tracer := newRecordingTracer()
	bus.SetTracer(tracer)

	require.NoError(t, bus.Subscribe("complete.topic", func(topic string, payload any) {}))
	require.NoError(t, bus.PublishSync("complete.topic", "payload"))

	select {
	case meta := <-tracer.completeCh:
		assert.Equal(t, 1, meta.HandlerCount)
		assert.True(t, meta.Success)
	case <-time.After(time.Second):
		t.Fatal("未捕获到同步处理完成事件")
	}

	require.NoError(t, bus.SubscribeWithResponse("complete.response", func(topic string, payload any) (any, error) {
		return payload, nil
	}))
	result, err := bus.PublishSyncAll("complete.response", "payload")
	require.NoError(t, err)
	assert.True(t, result.Success)

	select {
	case meta := <-tracer.completeCh:
		assert.GreaterOrEqual(t, meta.HandlerCount, 1)
		assert.True(t, meta.Success)
	case <-time.After(time.Second):
		t.Fatal("未捕获到响应式处理完成事件")
	}
}

func TestMetricsTracerErrorCounting(t *testing.T) {
	bus := New()
	defer bus.Close()

	tracer := NewMetricsTracer()
	bus.SetTracer(tracer)

	// 发布到有 panic 的处理器
	require.NoError(t, bus.Subscribe("panic.topic", func(topic string, payload any) {
		panic("boom")
	}))
	_ = bus.PublishSync("panic.topic", "payload")

	metrics := tracer.GetMetrics()
	assert.EqualValues(t, 1, metrics["error_count"])
	assert.EqualValues(t, 1, metrics["message_count"])
}

func Test_EventBusClose(t *testing.T) {
	bus := New()

	// 订阅一些处理器
	err := bus.Subscribe("test", func(topic string, msg any) {})
	assert.Nil(t, err)

	// 发布一些消息
	err = bus.Publish("test", "message")
	assert.Nil(t, err)

	// 关闭事件总线
	bus.Close()

	// 测试关闭后的操作
	err = bus.Publish("test", "message")
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrEventBusClosed))

	err = bus.Subscribe("test", func(topic string, msg any) {})
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrEventBusClosed))
}

func Test_EventBusHealthCheck(t *testing.T) {
	bus := New()

	// 测试正常状态
	err := bus.HealthCheck()
	assert.Nil(t, err)

	// 关闭事件总线
	bus.Close()

	// 测试关闭后的状态
	err = bus.HealthCheck()
	assert.Error(t, err)
	assert.True(t, errors.Is(err, ErrEventBusClosed))
}

// Mock 实现
type mockTracer struct{}

func (m *mockTracer) OnSubscribe(topic string, handler any)                     {}
func (m *mockTracer) OnUnsubscribe(topic string, handler any)                   {}
func (m *mockTracer) OnPublish(topic string, msg any, metadata PublishMetadata) {}
func (m *mockTracer) OnError(topic string, err error)                           {}
func (m *mockTracer) OnQueueFull(topic string, queueSize int)                   {}
func (m *mockTracer) OnSlowConsumer(topic string, latency time.Duration)        {}
func (m *mockTracer) OnComplete(topic string, metadata CompleteMetadata)        {}

type recordingTracer struct {
	completeCh chan CompleteMetadata
}

func newRecordingTracer() *recordingTracer {
	return &recordingTracer{
		completeCh: make(chan CompleteMetadata, 8),
	}
}

func (r *recordingTracer) OnPublish(topic string, payload any, metadata PublishMetadata) {}
func (r *recordingTracer) OnSubscribe(topic string, handler any)                         {}
func (r *recordingTracer) OnUnsubscribe(topic string, handler any)                       {}
func (r *recordingTracer) OnError(topic string, err error)                               {}
func (r *recordingTracer) OnQueueFull(topic string, size int)                            {}
func (r *recordingTracer) OnSlowConsumer(topic string, latency time.Duration)            {}
func (r *recordingTracer) OnComplete(topic string, metadata CompleteMetadata) {
	select {
	case r.completeCh <- metadata:
	default:
	}
}

func TestUnsubscribeAllRemovesTopicFromQueries(t *testing.T) {
	bus := New()
	defer bus.Close()

	handler := func(topic string, payload any) {}
	require.NoError(t, bus.Subscribe("cleanup.topic", handler))
	require.True(t, bus.HasSubscribers("cleanup.topic"))
	assert.Contains(t, bus.GetTopics(), "cleanup.topic")

	require.NoError(t, bus.UnsubscribeAll("cleanup.topic"))

	count, err := bus.GetSubscriberCount("cleanup.topic")
	require.NoError(t, err)
	assert.Equal(t, 0, count)
	assert.False(t, bus.HasSubscribers("cleanup.topic"))
	assert.NotContains(t, bus.GetTopics(), "cleanup.topic")
	assert.Equal(t, 0, bus.channels.Len())
}

func TestUnsubscribeAll_AllowsResubscribeWildcardTopic(t *testing.T) {
	bus := New()
	defer bus.Close()

	var count int32
	handler := func(topic string, payload any) {
		atomic.AddInt32(&count, 1)
	}

	require.NoError(t, bus.Subscribe("user.*", handler))
	require.NoError(t, bus.UnsubscribeAll("user.*"))
	require.NoError(t, bus.Subscribe("user.*", handler))
	require.NoError(t, bus.PublishSync("user.created", "payload"))
	assert.Equal(t, int32(1), atomic.LoadInt32(&count))
}

func TestSubscribeWithFilter_CanUnsubscribeOriginalHandler(t *testing.T) {
	bus := New()
	defer bus.Close()

	var count int32
	handler := func(topic string, payload any) {
		atomic.AddInt32(&count, 1)
	}
	filter := &testFilter{filterFunc: func(topic string, payload any) bool { return true }}

	require.NoError(t, bus.SubscribeWithFilter("filter.topic", handler, filter))
	require.NoError(t, bus.Unsubscribe("filter.topic", handler))
	require.NoError(t, bus.PublishSync("filter.topic", "payload"))
	assert.Equal(t, int32(0), atomic.LoadInt32(&count))
}

func TestSubscribeWithFilter_ContextHandlerCanUnsubscribeOriginalHandler(t *testing.T) {
	bus := New()
	defer bus.Close()

	var count int32
	handler := func(ctx context.Context, topic string, payload any) {
		atomic.AddInt32(&count, 1)
	}
	filter := &testFilter{filterFunc: func(topic string, payload any) bool { return true }}

	require.NoError(t, bus.SubscribeWithFilter("filter.ctx", handler, filter))
	require.NoError(t, bus.Unsubscribe("filter.ctx", handler))
	require.NoError(t, bus.PublishSyncWithContext(context.Background(), "filter.ctx", "payload"))
	assert.Equal(t, int32(0), atomic.LoadInt32(&count))
}

func TestSubscribeWithFilter_FilterPanicIsReportedAndHandlerSkipped(t *testing.T) {
	bus := New()
	defer bus.Close()

	var count int32
	handler := func(topic string, payload any) {
		atomic.AddInt32(&count, 1)
	}
	filter := &testFilter{filterFunc: func(topic string, payload any) bool {
		panic("boom")
	}}

	require.NoError(t, bus.SubscribeWithFilter("filter.panic", handler, filter))
	require.NoError(t, bus.PublishSync("filter.panic", "payload"))
	assert.Equal(t, int32(0), atomic.LoadInt32(&count))
}

func TestPublishSyncAnyValue_ReturnsFirstSuccessResult(t *testing.T) {
	bus := New()
	defer bus.Close()

	slowCanceled := make(chan struct{}, 1)
	require.NoError(t, bus.SubscribeWithResponseContext("value.topic", func(ctx context.Context, topic string, payload any) (any, error) {
		<-ctx.Done()
		slowCanceled <- struct{}{}
		return nil, ctx.Err()
	}))
	require.NoError(t, bus.SubscribeWithResponse("value.topic", func(topic string, payload any) (any, error) {
		return "fast-value", nil
	}))

	start := time.Now()
	value, err := bus.PublishSyncAnyValue("value.topic", "payload")
	elapsed := time.Since(start)

	require.NoError(t, err)
	assert.Equal(t, "fast-value", value)
	assert.Less(t, elapsed, 500*time.Millisecond)

	select {
	case <-slowCanceled:
	case <-time.After(time.Second):
		t.Fatal("慢处理器未被取消")
	}
}

func TestTopicGroupPublishSyncAnyValueWithContext(t *testing.T) {
	bus := New()
	defer bus.Close()

	group := bus.NewGroup("notify")
	require.NoError(t, group.SubscribeWithResponseContext("email", func(ctx context.Context, topic string, payload any) (any, error) {
		return ctx.Value("channel"), nil
	}))

	ctx := context.WithValue(context.Background(), "channel", "email")
	value, err := group.PublishSyncAnyValueWithContext(ctx, "email", "payload")
	require.NoError(t, err)
	assert.Equal(t, "email", value)
}

func TestSingletonPublishSyncAnyValue(t *testing.T) {
	ResetSingleton()
	defer Close()

	require.NoError(t, SubscribeWithResponse("singleton.value", func(topic string, payload any) (any, error) {
		return "singleton-ok", nil
	}))

	value, err := PublishSyncAnyValue("singleton.value", "payload")
	require.NoError(t, err)
	assert.Equal(t, "singleton-ok", value)
}

func TestSubscribeWithFilter_PanicIsTracedAndHandlerSkipped(t *testing.T) {
	bus := New()
	defer bus.Close()

	var called int32
	tracer := &testTracer{}
	var traced atomic.Bool
	tracer.onError = func(topic string, err error) {
		if strings.Contains(err.Error(), "filter panic") {
			traced.Store(true)
		}
	}
	bus.SetTracer(tracer)

	require.NoError(t, bus.SubscribeWithFilter("filter.panic", func(topic string, payload any) {
		atomic.AddInt32(&called, 1)
	}, &testFilter{filterFunc: func(topic string, payload any) bool {
		panic("boom")
	}}))

	require.NoError(t, bus.PublishSync("filter.panic", "payload"))
	assert.Equal(t, int32(0), atomic.LoadInt32(&called))
	assert.True(t, traced.Load())
}
